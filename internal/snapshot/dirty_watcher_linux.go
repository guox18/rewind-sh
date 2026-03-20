//go:build linux

package snapshot

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

type linuxDirtyWatcher struct {
	mu          sync.Mutex
	fd          int
	wdToPath    map[int]string
	pathToWd    map[string]int
	dirty       map[string]struct{}
	forceFull   bool
	initialized bool
}

func newDirtyWatcher() dirtyWatcher {
	return &linuxDirtyWatcher{
		wdToPath: map[int]string{},
		pathToWd: map[string]int{},
		dirty:    map[string]struct{}{},
	}
}

func (w *linuxDirtyWatcher) Start(roots []string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.initialized {
		return len(w.wdToPath), nil
	}
	fd, err := syscall.InotifyInit()
	if err != nil {
		return 0, err
	}
	w.fd = fd
	mask := uint32(syscall.IN_CREATE | syscall.IN_CLOSE_WRITE | syscall.IN_DELETE | syscall.IN_DELETE_SELF | syscall.IN_MODIFY | syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_ATTRIB)
	for _, root := range roots {
		if err = w.addRootLocked(root, mask); err != nil {
			return 0, err
		}
	}
	w.initialized = true
	go w.loop()
	return len(w.wdToPath), nil
}

func (w *linuxDirtyWatcher) AddRoots(roots []string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.initialized {
		return len(w.wdToPath), nil
	}
	mask := uint32(syscall.IN_CREATE | syscall.IN_CLOSE_WRITE | syscall.IN_DELETE | syscall.IN_DELETE_SELF | syscall.IN_MODIFY | syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_ATTRIB)
	for _, root := range roots {
		if err := w.addRootLocked(root, mask); err != nil {
			return len(w.wdToPath), err
		}
	}
	return len(w.wdToPath), nil
}

func (w *linuxDirtyWatcher) addRootLocked(root string, mask uint32) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
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
		if _, exists := w.pathToWd[path]; exists {
			return nil
		}
		wd, addErr := syscall.InotifyAddWatch(w.fd, path, mask)
		if addErr != nil {
			return addErr
		}
		w.wdToPath[wd] = path
		w.pathToWd[path] = wd
		return nil
	})
}

func (w *linuxDirtyWatcher) loop() {
	buf := make([]byte, syscall.SizeofInotifyEvent*256)
	for {
		n, err := syscall.Read(w.fd, buf)
		if err != nil || n <= 0 {
			w.mu.Lock()
			w.forceFull = true
			w.mu.Unlock()
			return
		}
		offset := 0
		for offset+syscall.SizeofInotifyEvent <= n {
			event := (*syscall.InotifyEvent)(unsafe.Pointer(&buf[offset]))
			offset += syscall.SizeofInotifyEvent
			name := ""
			if event.Len > 0 && offset+int(event.Len) <= n {
				raw := buf[offset : offset+int(event.Len)]
				if i := indexZero(raw); i >= 0 {
					raw = raw[:i]
				}
				name = string(raw)
				offset += int(event.Len)
			}
			w.handleEvent(event, name)
		}
	}
}

func (w *linuxDirtyWatcher) handleEvent(event *syscall.InotifyEvent, name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if event.Mask&syscall.IN_Q_OVERFLOW != 0 {
		w.forceFull = true
		return
	}
	base, ok := w.wdToPath[int(event.Wd)]
	if !ok {
		w.forceFull = true
		return
	}
	path := base
	if name != "" {
		path = filepath.Join(base, name)
	}
	w.dirty[path] = struct{}{}
	if event.Mask&syscall.IN_ISDIR != 0 && event.Mask&syscall.IN_CREATE != 0 {
		mask := uint32(syscall.IN_CREATE | syscall.IN_CLOSE_WRITE | syscall.IN_DELETE | syscall.IN_DELETE_SELF | syscall.IN_MODIFY | syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_ATTRIB)
		_ = filepath.WalkDir(path, func(cur string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			wd, addErr := syscall.InotifyAddWatch(w.fd, cur, mask)
			if addErr == nil {
				w.wdToPath[wd] = cur
				w.pathToWd[cur] = wd
			}
			return nil
		})
	}
	if event.Mask&syscall.IN_IGNORED != 0 {
		if p, ok := w.wdToPath[int(event.Wd)]; ok {
			delete(w.pathToWd, p)
		}
		delete(w.wdToPath, int(event.Wd))
	}
}

func (w *linuxDirtyWatcher) Drain() ([]string, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	items := make([]string, 0, len(w.dirty))
	for path := range w.dirty {
		items = append(items, path)
	}
	forceFull := w.forceFull
	w.dirty = map[string]struct{}{}
	w.forceFull = false
	return items, forceFull, nil
}

func indexZero(b []byte) int {
	for i, v := range b {
		if v == 0 {
			return i
		}
	}
	return -1
}

var _ = binary.LittleEndian
