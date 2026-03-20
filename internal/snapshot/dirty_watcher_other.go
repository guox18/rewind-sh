//go:build !linux

package snapshot

type noopDirtyWatcher struct{}

func newDirtyWatcher() dirtyWatcher {
	return &noopDirtyWatcher{}
}

func (w *noopDirtyWatcher) Start(roots []string) (int, error) {
	return 0, nil
}

func (w *noopDirtyWatcher) AddRoots(roots []string) (int, error) {
	return 0, nil
}

func (w *noopDirtyWatcher) Drain() ([]string, bool, error) {
	return nil, true, nil
}
