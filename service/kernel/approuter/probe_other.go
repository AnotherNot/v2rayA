//go:build !linux

package approuter

// Probe is empty where there are no cgroup eBPF hooks.
type Probe struct{}

func StartProbe(string) (*Probe, error) {
	return nil, ErrUnsupported
}

func (*Probe) Snapshot() (Snapshot, error) { return Snapshot{}, ErrUnsupported }

func (*Probe) Close() error { return nil }
