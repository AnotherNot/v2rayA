// Package approuter carries per-application routing: a cgroup eBPF program
// decides where each flow goes, and a userspace relay hands a proxied flow to
// a local inbound of the core. The probe is the first stage — it attaches
// where the engine will attach, reads the identity the engine will read and
// counts, without changing any traffic.
package approuter

import (
	"errors"
	"fmt"
	"sort"
)

// ErrUnsupported reports a platform without the cgroup eBPF hooks.
var ErrUnsupported = errors.New("the app router requires Linux cgroup BPF")

// Snapshot is what the probe has counted since it was attached.
type Snapshot struct {
	Connects4   uint64
	Connects6   uint64
	Executables []Executable
}

// Executable is one executable the probe saw connect, named by its file
// rather than by its name: two binaries called the same are told apart, and a
// replaced binary is a different executable.
type Executable struct {
	Device uint32
	Inode  uint64
	Comm   string
	Flows  uint64
}

func (s Snapshot) String() string {
	return fmt.Sprintf("%d IPv4 and %d IPv6 connects from %d executables",
		s.Connects4, s.Connects6, len(s.Executables))
}

// sortExecutables orders by traffic, then by name, so two snapshots of the
// same state read the same.
func (s *Snapshot) sortExecutables() {
	sort.Slice(s.Executables, func(i, j int) bool {
		if s.Executables[i].Flows != s.Executables[j].Flows {
			return s.Executables[i].Flows > s.Executables[j].Flows
		}
		return s.Executables[i].Comm < s.Executables[j].Comm
	})
}
