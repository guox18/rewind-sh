package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type WatchDiffBackend struct {
	mu           sync.Mutex
	monitorPaths []string
	initialized  bool
	stateDir     string
	roots        []string
	current      map[string]entryRecord
	watcher      dirtyWatcher
	info         ScopeInfo
}

type entryRecord struct {
	Root       string `json:"root"`
	Rel        string `json:"rel"`
	Type       string `json:"type"`
	Mode       uint32 `json:"mode"`
	Size       int64  `json:"size"`
	ModTime    int64  `json:"mod_time"`
	Hash       string `json:"hash,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

type snapshotRecord struct {
	ID      string        `json:"id"`
	Roots   []string      `json:"roots"`
	Entries []entryRecord `json:"entries"`
}

func NewWatchDiffBackend(monitorPaths []string) *WatchDiffBackend {
	items := make([]string, 0, len(monitorPaths))
	for _, item := range monitorPaths {
		if strings.TrimSpace(item) == "" {
			continue
		}
		items = append(items, item)
	}
	return &WatchDiffBackend{
		monitorPaths: items,
	}
}

func (b *WatchDiffBackend) Name() string {
	return "watch-diff"
}

func (b *WatchDiffBackend) Available() (bool, string) {
	if runtime.GOOS != "linux" {
		return false, "watch-diff 仅支持 Linux"
	}
	return true, ""
}

func (b *WatchDiffBackend) ScopeInfo() ScopeInfo {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.info
	out.Roots = append([]string{}, b.info.Roots...)
	return out
}

func (b *WatchDiffBackend) Initialize(workdir, stateDir string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.init(workdir, stateDir)
}

func (b *WatchDiffBackend) ExtendRoots(paths []string) (ScopeInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.initialized {
		return ScopeInfo{}, errors.New("后端尚未初始化")
	}
	incoming := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		clean := filepath.Clean(path)
		if !filepath.IsAbs(clean) {
			abs, err := filepath.Abs(clean)
			if err != nil {
				return ScopeInfo{}, err
			}
			clean = abs
		}
		incoming = append(incoming, clean)
	}
	if len(incoming) == 0 {
		out := b.info
		out.Roots = append([]string{}, b.info.Roots...)
		return out, nil
	}
	candidate := compactRoots(append(append([]string{}, b.roots...), incoming...))
	limit := b.info.WatchLimit
	if limit <= 0 {
		limit = inotifyWatchLimit()
	}
	used := countWatchTargets(candidate)
	if limit > 0 && used > limit {
		return ScopeInfo{}, fmt.Errorf("监控范围目录过多，watch数量=%d 超过上限=%d，请缩小监控范围", used, limit)
	}
	added := rootsNeedAdd(b.roots, candidate)
	if len(added) > 0 && b.watcher != nil {
		if _, err := b.watcher.AddRoots(added); err != nil {
			return ScopeInfo{}, err
		}
	}
	b.roots = candidate
	if b.stateDir != "" && b.info.LogFile != "" {
		if err := writeWatchLog(b.info.LogFile, b.roots); err != nil {
			return ScopeInfo{}, err
		}
	}
	b.info = ScopeInfo{
		Roots:      append([]string{}, b.roots...),
		WatchLimit: limit,
		WatchUsed:  used,
		LogFile:    b.info.LogFile,
	}
	out := b.info
	out.Roots = append([]string{}, b.info.Roots...)
	return out, nil
}

func (b *WatchDiffBackend) Create(workdir, stateDir, snapshotID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.init(workdir, stateDir); err != nil {
		return err
	}
	if err := b.syncCurrent(stateDir); err != nil {
		return err
	}
	return saveManifest(stateDir, snapshotID, b.roots, b.current)
}

func (b *WatchDiffBackend) Restore(workdir, stateDir, snapshotID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.init(workdir, stateDir); err != nil {
		return err
	}
	target, err := loadManifest(stateDir, snapshotID)
	if err != nil {
		return err
	}
	for _, root := range target.Roots {
		if err = restoreRoot(stateDir, root, target.Entries); err != nil {
			return err
		}
	}
	b.current = entriesToMap(target.Entries)
	if b.watcher != nil {
		_, _, _ = b.watcher.Drain()
	}
	return nil
}

func (b *WatchDiffBackend) Delete(stateDir, snapshotID string) error {
	return os.Remove(snapshotPath(stateDir, snapshotID))
}

func (b *WatchDiffBackend) init(workdir, stateDir string) error {
	if b.initialized {
		return nil
	}
	roots, limit, used, err := resolveMonitorRoots(workdir, b.monitorPaths)
	if err != nil {
		return err
	}
	if err = ensureStateDirs(stateDir); err != nil {
		return err
	}
	logPath := filepath.Join(stateDir, "watch_scope.log")
	if err = writeWatchLog(logPath, roots); err != nil {
		return err
	}
	watcher := newDirtyWatcher()
	if watcher != nil {
		watchCount, watchErr := watcher.Start(roots)
		if watchErr == nil {
			used = watchCount
		}
	}
	b.roots = roots
	b.stateDir = stateDir
	b.info = ScopeInfo{
		Roots:      append([]string{}, roots...),
		WatchLimit: limit,
		WatchUsed:  used,
		LogFile:    logPath,
	}
	b.watcher = watcher
	b.current = map[string]entryRecord{}
	b.initialized = true
	return nil
}

func rootsNeedAdd(existing []string, candidate []string) []string {
	added := make([]string, 0, len(candidate))
	for _, root := range candidate {
		covered := false
		for _, old := range existing {
			if within(old, root) {
				covered = true
				break
			}
		}
		if !covered {
			added = append(added, root)
		}
	}
	return added
}

func (b *WatchDiffBackend) syncCurrent(stateDir string) error {
	paths := []string{}
	fullScan := false
	if b.watcher == nil {
		fullScan = true
	} else {
		dirty, forceFull, err := b.watcher.Drain()
		if err != nil {
			fullScan = true
		}
		paths = dirty
		if forceFull {
			fullScan = true
		}
	}
	if len(b.current) == 0 {
		fullScan = true
	}
	if fullScan {
		next := map[string]entryRecord{}
		for _, root := range b.roots {
			if err := scanTree(root, root, next); err != nil {
				return err
			}
		}
		if err := persistObjects(stateDir, next); err != nil {
			return err
		}
		b.current = next
		return nil
	}
	compact := compactDirtyPaths(paths)
	for _, path := range compact {
		root := matchRoot(b.roots, path)
		if root == "" {
			continue
		}
		if err := reconcilePath(stateDir, root, path, b.current); err != nil {
			return err
		}
	}
	return nil
}

func ensureStateDirs(stateDir string) error {
	for _, path := range []string{
		filepath.Join(stateDir, "snapshots"),
		filepath.Join(stateDir, "objects"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func writeWatchLog(path string, roots []string) error {
	lines := make([]string, 0, 1024)
	for _, root := range roots {
		lines = append(lines, "root="+root)
		err := filepath.WalkDir(root, func(cur string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, cur)
			if err != nil {
				return err
			}
			if rel != "." && isRewindMetaPath(rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				lines = append(lines, cur)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	content := strings.Join(lines, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func scanTree(root, path string, out map[string]entryRecord) error {
	return filepath.WalkDir(path, func(cur string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, cur)
		if err != nil {
			return err
		}
		if rel != "." && isRewindMetaPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rec := entryRecord{
			Root:    root,
			Rel:     filepath.ToSlash(rel),
			Mode:    uint32(info.Mode().Perm()),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			rec.Type = "symlink"
			link, e := os.Readlink(cur)
			if e != nil {
				return e
			}
			rec.LinkTarget = link
		case d.IsDir():
			rec.Type = "dir"
		default:
			rec.Type = "file"
			hash, e := fileHash(cur)
			if e != nil {
				return e
			}
			rec.Hash = hash
		}
		out[entryKey(root, rec.Rel)] = rec
		return nil
	})
}

func persistObjects(stateDir string, entries map[string]entryRecord) error {
	for _, rec := range entries {
		if rec.Type != "file" || rec.Hash == "" {
			continue
		}
		path := filepath.Join(rec.Root, filepath.FromSlash(rec.Rel))
		if err := ensureObject(stateDir, rec.Hash, path, fs.FileMode(rec.Mode)); err != nil {
			return err
		}
	}
	return nil
}

func reconcilePath(stateDir, root, path string, current map[string]entryRecord) error {
	cleanPath := filepath.Clean(path)
	if !within(root, cleanPath) {
		return nil
	}
	rel, err := filepath.Rel(root, cleanPath)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		for key, rec := range current {
			if rec.Root == root {
				delete(current, key)
			}
		}
		return scanAndPersistRoot(stateDir, root, current)
	}
	removePrefix(current, root, rel)
	info, err := os.Lstat(cleanPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if info.IsDir() {
		return scanAndPersistPath(stateDir, root, cleanPath, current)
	}
	rec, err := buildSingleRecord(root, cleanPath, info)
	if err != nil {
		return err
	}
	if rec.Type == "file" {
		if err = ensureObject(stateDir, rec.Hash, cleanPath, fs.FileMode(rec.Mode)); err != nil {
			return err
		}
	}
	current[entryKey(root, rec.Rel)] = rec
	return nil
}

func scanAndPersistRoot(stateDir, root string, current map[string]entryRecord) error {
	tmp := map[string]entryRecord{}
	if err := scanTree(root, root, tmp); err != nil {
		return err
	}
	if err := persistObjects(stateDir, tmp); err != nil {
		return err
	}
	for key, rec := range tmp {
		current[key] = rec
	}
	return nil
}

func scanAndPersistPath(stateDir, root, path string, current map[string]entryRecord) error {
	tmp := map[string]entryRecord{}
	if err := scanTree(root, path, tmp); err != nil {
		return err
	}
	for _, rec := range tmp {
		if rec.Type != "file" {
			continue
		}
		abs := filepath.Join(root, filepath.FromSlash(rec.Rel))
		if err := ensureObject(stateDir, rec.Hash, abs, fs.FileMode(rec.Mode)); err != nil {
			return err
		}
	}
	for key, rec := range tmp {
		current[key] = rec
	}
	return nil
}

func buildSingleRecord(root, absPath string, info fs.FileInfo) (entryRecord, error) {
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return entryRecord{}, err
	}
	rel = filepath.ToSlash(rel)
	rec := entryRecord{
		Root:    root,
		Rel:     rel,
		Mode:    uint32(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime().UnixNano(),
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		rec.Type = "symlink"
		link, e := os.Readlink(absPath)
		if e != nil {
			return entryRecord{}, e
		}
		rec.LinkTarget = link
	case info.IsDir():
		rec.Type = "dir"
	default:
		rec.Type = "file"
		hash, e := fileHash(absPath)
		if e != nil {
			return entryRecord{}, e
		}
		rec.Hash = hash
	}
	return rec, nil
}

func removePrefix(current map[string]entryRecord, root, rel string) {
	prefix := rel + "/"
	for key, rec := range current {
		if rec.Root != root {
			continue
		}
		if rec.Rel == rel || strings.HasPrefix(rec.Rel, prefix) {
			delete(current, key)
		}
	}
}

func fileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ensureObject(stateDir, hash, src string, mode fs.FileMode) error {
	dst := objectPath(stateDir, hash)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	return copyFile(src, dst, mode)
}

func objectPath(stateDir, hash string) string {
	prefix := "xx"
	if len(hash) >= 2 {
		prefix = hash[:2]
	}
	return filepath.Join(stateDir, "objects", prefix, hash)
}

func saveManifest(stateDir, snapshotID string, roots []string, entries map[string]entryRecord) error {
	list := make([]entryRecord, 0, len(entries))
	for _, rec := range entries {
		list = append(list, rec)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Root != list[j].Root {
			return list[i].Root < list[j].Root
		}
		return list[i].Rel < list[j].Rel
	})
	record := snapshotRecord{
		ID:      snapshotID,
		Roots:   append([]string{}, roots...),
		Entries: list,
	}
	b, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return os.WriteFile(snapshotPath(stateDir, snapshotID), b, 0o644)
}

func loadManifest(stateDir, snapshotID string) (snapshotRecord, error) {
	b, err := os.ReadFile(snapshotPath(stateDir, snapshotID))
	if err != nil {
		return snapshotRecord{}, err
	}
	var record snapshotRecord
	if err = json.Unmarshal(b, &record); err != nil {
		return snapshotRecord{}, err
	}
	return record, nil
}

func snapshotPath(stateDir, snapshotID string) string {
	return filepath.Join(stateDir, "snapshots", snapshotID+".json")
}

func restoreRoot(stateDir, root string, entries []entryRecord) error {
	target := map[string]entryRecord{}
	for _, rec := range entries {
		if rec.Root != root {
			continue
		}
		target[rec.Rel] = rec
	}
	protected := protectedRelPaths(root)
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err = os.MkdirAll(root, 0o755); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	if err := filepath.WalkDir(root, func(cur string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, cur)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if isRewindMetaPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if _, keep := protected[rel]; keep {
			return nil
		}
		if _, ok := target[rel]; ok {
			return nil
		}
		if err := os.RemoveAll(cur); err != nil {
			return err
		}
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}); err != nil {
		return err
	}
	dirs := make([]entryRecord, 0, len(target))
	others := make([]entryRecord, 0, len(target))
	for _, rec := range target {
		if rec.Type == "dir" {
			dirs = append(dirs, rec)
		} else {
			others = append(others, rec)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i].Rel) < len(dirs[j].Rel) })
	for _, rec := range dirs {
		if _, keep := protected[rec.Rel]; keep {
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(rec.Rel))
		if err := os.MkdirAll(dst, fs.FileMode(rec.Mode)); err != nil {
			return err
		}
	}
	for _, rec := range others {
		if _, keep := protected[rec.Rel]; keep {
			continue
		}
		dst := filepath.Join(root, filepath.FromSlash(rec.Rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		switch rec.Type {
		case "symlink":
			_ = os.RemoveAll(dst)
			if err := os.Symlink(rec.LinkTarget, dst); err != nil {
				return err
			}
		case "file":
			src := objectPath(stateDir, rec.Hash)
			if _, err := os.Stat(src); err != nil {
				return err
			}
			if err := copyFile(src, dst, fs.FileMode(rec.Mode)); err != nil {
				return err
			}
		}
	}
	return nil
}

func protectedRelPaths(root string) map[string]struct{} {
	protected := map[string]struct{}{}
	exe, err := os.Executable()
	if err != nil {
		return protected
	}
	addProtectedPath(root, exe, protected)
	if resolved, e := filepath.EvalSymlinks(exe); e == nil {
		addProtectedPath(root, resolved, protected)
	}
	return protected
}

func addProtectedPath(root, absPath string, out map[string]struct{}) {
	clean := filepath.Clean(absPath)
	if !within(root, clean) {
		return
	}
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == "." {
		return
	}
	out[filepath.ToSlash(rel)] = struct{}{}
}

func entriesToMap(entries []entryRecord) map[string]entryRecord {
	out := make(map[string]entryRecord, len(entries))
	for _, rec := range entries {
		out[entryKey(rec.Root, rec.Rel)] = rec
	}
	return out
}

func entryKey(root, rel string) string {
	return root + "|" + rel
}

func within(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == path {
		return true
	}
	prefix := root + string(filepath.Separator)
	return strings.HasPrefix(path, prefix)
}

func compactDirtyPaths(paths []string) []string {
	set := map[string]struct{}{}
	for _, path := range paths {
		clean := filepath.Clean(path)
		set[clean] = struct{}{}
	}
	list := make([]string, 0, len(set))
	for path := range set {
		list = append(list, path)
	}
	sort.Slice(list, func(i, j int) bool { return len(list[i]) < len(list[j]) })
	out := make([]string, 0, len(list))
	for _, path := range list {
		skip := false
		for _, keep := range out {
			if within(keep, path) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, path)
		}
	}
	return out
}

func matchRoot(roots []string, path string) string {
	best := ""
	bestLen := -1
	for _, root := range roots {
		if within(root, path) && len(root) > bestLen {
			best = root
			bestLen = len(root)
		}
	}
	return best
}
