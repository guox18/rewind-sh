package snapshot

type dirtyWatcher interface {
	Start(roots []string) (int, error)
	AddRoots(roots []string) (int, error)
	Drain() ([]string, bool, error)
}
