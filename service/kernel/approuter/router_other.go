//go:build !linux

package approuter

// Router is empty where there are no cgroup BPF hooks.
type Router struct{}

func Start(Options) (*Router, error) {
	return nil, ErrUnsupported
}

func (*Router) Snapshot() (Stats, error) { return Stats{}, ErrUnsupported }

func (*Router) Close() error { return nil }
