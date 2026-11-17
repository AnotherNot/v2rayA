//go:build linux

package approuter

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/v2rayA/v2rayA/pkg/util/log"
	"golang.org/x/sys/unix"
)

// The object is generated from bpf/router.bpf.c and committed, so the build
// needs no clang. See the comment at the top of that file for the command.
//
//go:embed bpf/router.bpf.o
var routerObject []byte

// Mirrors struct router_cfg.
type routerCfg struct {
	Enabled   uint32
	RelayPort uint32
	SelfTgid  uint32
	Pad       uint32
	Netns     uint64
}

// Mirrors struct router_comm_key: sixteen bytes of bpf_get_current_comm, read
// as two words. The bytes are what both sides key on, so the order the words
// are interpreted in does not matter as long as it is the same on both.
type routerCommKey struct {
	W0 uint64
	W1 uint64
}

// Mirrors struct router_orig.
type routerOrig struct {
	IP4  uint32
	Port uint16
	Pad  uint16
	Pid  uint32
	Pad2 uint32
}

// Mirrors struct router_port_key.
type routerPortKey struct {
	IP   uint32
	Port uint32
}

// singleClass is the class the engine serves. One class is what the first
// stage needs: a flow is either routed through the proxy group or left alone.
const singleClass = 1

// Router is the attached engine: the cgroup programs and the relay that
// receives what they rewrite.
type Router struct {
	collection *ebpf.Collection
	byPort     *ebpf.Map
	stats      *ebpf.Map
	links      []link.Link
	relay      *relay
	netns      uint64
	stop       chan struct{}
	done       chan struct{}
	mu         sync.Mutex
	closed     bool
}

// Start attaches the engine to the supplied cgroup v2 subtree and starts its
// relay. Real BPF links are used and nothing is pinned: closing the descriptors
// must also detach the programs after a crash.
func Start(opts Options) (*Router, error) {
	if len(opts.Apps) == 0 {
		return nil, errors.New("the app router needs at least one application")
	}
	if _, ok := opts.ClassPorts[singleClass]; !ok {
		return nil, fmt.Errorf("the app router needs a local inbound for class %d", singleClass)
	}
	cookie, err := netnsCookie()
	if errors.Is(err, unix.ENOPROTOOPT) {
		return nil, errors.New("SO_NETNS_COOKIE is not supported: the kernel must be 5.14 or later")
	}
	if err != nil {
		return nil, fmt.Errorf("read network namespace cookie: %w", err)
	}
	dir, err := os.Open(opts.Cgroup)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(dir.Fd()), &stat); err != nil {
		return nil, err
	}
	if stat.Type != unix.CGROUP2_SUPER_MAGIC {
		return nil, fmt.Errorf("%s is not on a cgroup v2 mount", opts.Cgroup)
	}

	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(routerObject))
	if err != nil {
		return nil, fmt.Errorf("read the embedded app router: %w", err)
	}
	collection, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("load the app router: %w", err)
	}
	r := &Router{collection: collection, netns: cookie, stop: make(chan struct{}), done: make(chan struct{})}
	cleanup := func() {
		for _, l := range r.links {
			_ = l.Close()
		}
		collection.Close()
	}
	if err := r.fillMaps(collection, opts); err != nil {
		cleanup()
		return nil, err
	}
	for _, hook := range []struct {
		program string
		kind    ebpf.AttachType
	}{
		{"router_connect4", ebpf.AttachCGroupInet4Connect},
		{"router_sockops", ebpf.AttachCGroupSockOps},
		{"router_getpeername4", ebpf.AttachCgroupInet4GetPeername},
	} {
		program, ok := collection.Programs[hook.program]
		if !ok {
			cleanup()
			return nil, fmt.Errorf("the embedded app router has no %s program", hook.program)
		}
		attached, err := link.AttachCgroup(link.CgroupOptions{
			Path: opts.Cgroup, Attach: hook.kind, Program: program,
		})
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("attach the app router (%s): %w", hook.program, err)
		}
		r.links = append(r.links, attached)
	}
	r.byPort = collection.Maps["by_port"]
	r.stats = collection.Maps["stats"]
	r.relay = newRelay(mapStore{byPort: r.byPort}, opts.ClassPorts, singleClass)
	if err := r.relay.start(); err != nil {
		cleanup()
		return nil, err
	}
	// Enabled last: the programs are attached by now, so no connect is served
	// by a table that is still being filled.
	if err := r.putConfig(true); err != nil {
		_ = r.relay.close()
		cleanup()
		return nil, err
	}
	log.Info("app router: attached to %s, relaying on port %d, applications %s",
		opts.Cgroup, RelayPort, strings.Join(opts.Apps, ", "))
	go r.report()
	return r, nil
}

// report writes the counters to the log while the engine runs: a mode that
// silently routes nothing is indistinguishable from one that is not attached,
// and the counters are what tells them apart.
func (r *Router) report() {
	defer close(r.done)
	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-ticker.C:
			snapshot, err := r.Snapshot()
			if err != nil {
				log.Warn("app router: read counters: %v", err)
				continue
			}
			log.Info("app router: %d connects seen, %d redirected, %d relayed, %d refused, %d not selected",
				snapshot.Connects, snapshot.Redirected, snapshot.Relayed, snapshot.Refused, snapshot.NotSelected)
		}
	}
}

func (r *Router) fillMaps(collection *ebpf.Collection, opts Options) error {
	apps, ok := collection.Maps["app_names"]
	if !ok {
		return errors.New("the embedded app router has no app_names map")
	}
	value := uint8(1)
	for _, name := range opts.Apps {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if len(name) > 15 {
			// bpf_get_current_comm keeps fifteen bytes and a terminator, so a
			// longer name can never match.
			return fmt.Errorf("%q is longer than the 15 bytes the kernel reports for a process name", name)
		}
		var comm [16]byte
		copy(comm[:], name)
		key := routerCommKey{
			W0: binary.NativeEndian.Uint64(comm[0:8]),
			W1: binary.NativeEndian.Uint64(comm[8:16]),
		}
		if err := apps.Put(key, value); err != nil {
			return fmt.Errorf("add %q to the application list: %w", name, err)
		}
	}
	return r.putConfig(false)
}

func (r *Router) putConfig(enabled bool) error {
	cfg, ok := r.collection.Maps["cfg"]
	if !ok {
		return errors.New("the embedded app router has no cfg map")
	}
	config := routerCfg{
		RelayPort: uint32(RelayPort),
		SelfTgid:  uint32(os.Getpid()),
		Netns:     r.netns,
	}
	if enabled {
		config.Enabled = 1
	}
	return cfg.Put(uint32(0), config)
}

// Snapshot reads what the kernel and the relay have counted.
func (r *Router) Snapshot() (Stats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Stats{}, errors.New("the app router is closed")
	}
	var counters [8]uint64
	for i := range counters {
		if err := r.stats.Lookup(uint32(i), &counters[i]); err != nil {
			return Stats{}, err
		}
	}
	return Stats{
		Connects:       counters[0],
		ImplicitDirect: counters[1],
		NotSelected:    counters[2],
		Self:           counters[3],
		ForeignNetns:   counters[4],
		Redirected:     counters[5],
		PortWritten:    counters[6],
		Getpeername:    counters[7],
		Relayed:        r.relay.relayed.Load(),
		Refused:        r.relay.refused.Load(),
	}, nil
}

func (r *Router) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	close(r.stop)
	r.mu.Unlock()
	// The reporter reads the maps through Snapshot, which takes the same lock;
	// stop it before the maps go away.
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	relayErr := r.relay.close()
	var errs []error
	for i := len(r.links) - 1; i >= 0; i-- {
		errs = append(errs, r.links[i].Close())
	}
	r.links = nil
	r.collection.Close()
	return errors.Join(append([]error{relayErr}, errs...)...)
}

// netnsCookie reads the network namespace cookie of this process. Programs
// compare it against the cookie of the socket, so a container's cgroup under
// the root one is left alone.
func netnsCookie() (uint64, error) {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	return unix.GetsockoptUint64(fd, unix.SOL_SOCKET, unix.SO_NETNS_COOKIE)
}

// mapStore adapts the by_port map to what the relay asks for.
type mapStore struct {
	byPort *ebpf.Map
}

// take reads the entry and removes it: one connection, one destination. A stale
// entry must not be handed to a later connection that reuses the port.
func (s mapStore) take(key portKey) (orig, bool, error) {
	bpfKey := routerPortKey{IP: key.IP, Port: key.Port}
	var value routerOrig
	err := s.byPort.Lookup(bpfKey, &value)
	if errors.Is(err, ebpf.ErrKeyNotExist) {
		return orig{}, false, nil
	}
	if err != nil {
		return orig{}, false, err
	}
	if err := s.byPort.Delete(bpfKey); err != nil && !errors.Is(err, ebpf.ErrKeyNotExist) {
		return orig{}, false, err
	}
	o := orig{Port: int(value.Port), Pid: value.Pid}
	binary.NativeEndian.PutUint32(o.IP[:], value.IP4)
	return o, true, nil
}