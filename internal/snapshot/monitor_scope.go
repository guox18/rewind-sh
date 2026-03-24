package snapshot

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

func resolveMonitorRoots(workdir string, monitorPaths []string) ([]string, int, int, error) {
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return nil, 0, 0, err
	}
	limit := inotifyWatchLimit()
	return resolveMonitorRootsWithLimit(absWorkdir, monitorPaths, limit)
}

func resolveMonitorRootsWithLimit(absWorkdir string, monitorPaths []string, limit int) ([]string, int, int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = absWorkdir
	}
	home = filepath.Clean(home)
	roots := []string{filepath.Clean(absWorkdir)}
	for _, item := range monitorPaths {
		resolved := item
		if strings.HasPrefix(resolved, "~") {
			resolved = filepath.Join(home, strings.TrimPrefix(resolved, "~"))
		}
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(absWorkdir, resolved)
		}
		resolved = filepath.Clean(resolved)
		roots = append(roots, resolved)
	}
	roots = compactRoots(roots)
	used := countWatchTargets(roots)
	if limit > 0 && used > limit {
		return nil, limit, used, fmt.Errorf("监控范围目录过多，watch数量=%d 超过上限=%d，请缩小监控范围", used, limit)
	}
	return roots, limit, used, nil
}

func compactRoots(roots []string) []string {
	items := make([]string, 0, len(roots))
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		items = append(items, filepath.Clean(root))
	}
	sort.Slice(items, func(i, j int) bool { return len(items[i]) < len(items[j]) })
	out := make([]string, 0, len(items))
	for _, root := range items {
		skip := false
		for _, keep := range out {
			if within(keep, root) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, root)
		}
	}
	return out
}

func countWatchTargets(roots []string) int {
	total := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			if rel != "." && isRewindMetaPath(rel) {
				return filepath.SkipDir
			}
			total++
			return nil
		})
		if err != nil {
			continue
		}
	}
	return total
}

func inotifyWatchLimit() int {
	if runtime.GOOS != "linux" {
		return 0
	}
	file, err := os.Open("/proc/sys/fs/inotify/max_user_watches")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return 0
	}
	value := strings.TrimSpace(scanner.Text())
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return n
}

