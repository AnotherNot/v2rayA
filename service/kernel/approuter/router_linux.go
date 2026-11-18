//go:build linux

package approuter

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strconv"
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
	CoreTgid  uint32
	Netns     uint64
}

// Mirrors struct router_orig.
type routerOrig struct {
	Addr      [16]uint8
	Port      uint16
	Pad0      uint16
	Pid       uint32
	ExeDev    uint32
	Pad1      uint32
	ExeIno    uint64
	Cookie    uint64
	Action    uint8
	Family    uint8
	Proto     uint8
	InboundID uint8
	Pad3      [4]uint8
}

// Mirrors struct router_port_key.
type routerPortKey struct {
	IP   uint32
	Port uint32
}

// Mirrors struct router_exec_rules: the ordered rule indexes of one
// executable, plus its length.
type routerExecRules struct {
	N    uint32
	Rule [rulesPerExe]uint32
}

// Router is the attached engine: the cgroup programs and the relay that
// receives what they rewrite.
type Router struct {
	collection *ebpf.Collection
	byPort     *ebpf.Map
	stats      *ebpf.Map
	links      []link.Link
	relay      *relay
	udpRelay   *udpRelay
	planner    *planner
	netns      uint64
	coreTgid   uint32
	stop       chan struct{}
	done       chan struct{}
	mu         sync.Mutex
	closed     bool
}

// Start attaches the engine to the supplied cgroup v2 subtree and starts its
// relay. Real BPF links are used and nothing is pinned: closing the descriptors
// must also detach the programs after a crash.
func Start(opts Options) (*Router, error) {
	if len(opts.Rules) == 0 {
		return nil, errors.New("the app router needs at least one rule")
	}
	compiled, err := compileRules(opts.Rules)
	if err != nil {
		return nil, err
	}
	used := map[int]bool{}
	for _, rule := range opts.Rules {
		if rule.Action != ActionProxy {
			continue
		}
		if _, ok := opts.ClassPorts[rule.Class]; !ok {
			return nil, fmt.Errorf("a rule sends traffic to class %d, and no inbound serves it", rule.Class)
		}
		used[rule.Class] = true
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
	r := &Router{
		collection: collection,
		netns:      cookie,
		coreTgid:   initNamespacePidOf(opts.CorePid),
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	cleanup := func() {
		for _, l := range r.links {
			_ = l.Close()
		}
		collection.Close()
	}
	if err := r.fillMaps(collection, compiled); err != nil {
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
		{"router_sendmsg4", ebpf.AttachCGroupUDP4Sendmsg},
		{"router_recvmsg4", ebpf.AttachCGroupUDP4Recvmsg},
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
	cache := &mapCache{
		exe2rules: collection.Maps["exe2rules"],
		execRules: collection.Maps["exec_rules"],
	}
	r.planner = newPlanner(compiled, cache, opts.UnknownPolicy)
	r.relay = newRelay(mapStore{byPort: r.byPort}, r.planner, opts.ClassPorts)
	if err := r.relay.start(); err != nil {
		cleanup()
		return nil, err
	}
	// UDP uses the same port number as TCP: the two are separate namespaces,
	// and one port is easier to reason about than two.
	r.udpRelay = newUDPRelay(mapFlowStore{flows: collection.Maps["flows"]}, r.planner, opts.ClassPorts)
	if err := r.udpRelay.start(); err != nil {
		_ = r.relay.close()
		cleanup()
		return nil, err
	}
	// Enabled last: the programs are attached by now, so no connect is served
	// by a table that is still being filled.
	if err := r.putConfig(true); err != nil {
		_ = r.udpRelay.close()
		_ = r.relay.close()
		cleanup()
		return nil, err
	}
	log.Info("app router: attached to %s, %d rules, %d classes, relaying on port %d",
		opts.Cgroup, len(opts.Rules), len(used), RelayPort)
	go r.report()
	return r, nil
}

func (r *Router) fillMaps(collection *ebpf.Collection, compiled *compiled) error {
	rules, ok := collection.Maps["brules"]
	if !ok {
		return errors.New("the embedded app router has no brules map")
	}
	for i := range compiled.kernel {
		if err := rules.Put(uint32(i), compiled.kernel[i]); err != nil {
			return fmt.Errorf("write rule %d: %w", i+1, err)
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
		SelfTgid:  initNamespacePid(),
		CoreTgid:  r.coreTgid,
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
	var counters [32]uint64
	for i := range counters {
		if err := r.stats.Lookup(uint32(i), &counters[i]); err != nil {
			return Stats{}, err
		}
	}
	return Stats{
		Connects:       counters[0],
		ImplicitDirect: counters[1],
		Self:           counters[2],
		ForeignNetns:   counters[3],
		Decide:         counters[4],
		NoRule:         counters[5],
		RuleDirect:     counters[6],
		RuleProxy:      counters[7],
		RuleBlock:      counters[8],
		Redirected:     counters[9],
		PortWritten:    counters[10],
		Getpeername:    counters[11],
		Exempt:         counters[12],
		RulesFull:      counters[13],
		UDPDatagrams:   counters[14],
		UDPRedirected:  counters[15],
		UDPNoFlow:      counters[16],
		UDPRewritten:   counters[17],
		UDPAnswers:     counters[18],
		Relayed:        r.relay.relayed.Load(),
		RelayDirect:    r.relay.direct.Load(),
		Refused:        r.relay.refused.Load(),
		UDPRelayed:     r.udpRelay.forwarded.Load(),
		UDPDirect:      r.udpRelay.direct.Load(),
		UDPRefused:     r.udpRelay.refused.Load(),
	}, nil
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
			log.Info("app router: %d connects, %d proxied, %d direct, %d blocked, %d decided late, %d relayed, %d dialled directly, %d refused",
				snapshot.Connects, snapshot.RuleProxy, snapshot.RuleDirect, snapshot.RuleBlock,
				snapshot.Decide, snapshot.Relayed, snapshot.RelayDirect, snapshot.Refused)
			log.Info("app router: %d UDP datagrams, %d redirected, %d relayed, %d direct, %d refused, %d answers restored",
				snapshot.UDPDatagrams, snapshot.UDPRedirected, snapshot.UDPRelayed,
				snapshot.UDPDirect, snapshot.UDPRefused, snapshot.UDPRewritten)
		}
	}
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
	relayErr := errors.Join(r.relay.close(), r.udpRelay.close())
	var errs []error
	for i := len(r.links) - 1; i >= 0; i-- {
		errs = append(errs, r.links[i].Close())
	}
	r.links = nil
	r.collection.Close()
	return errors.Join(append([]error{relayErr}, errs...)...)
}

// initNamespacePid is this process's pid in the namespace the kernel reports
// pids in. Inside a container the two differ, and comparing the wrong one would
// leave the daemon's own sockets unexempt: the relay's dial would be redirected
// back to the relay, and the flow would loop until it timed out.
func initNamespacePid() uint32 { return initNamespacePidOf(os.Getpid()) }

// initNamespacePidOf is the same answer for another process of this namespace,
// which is how the core's pid is named the way the kernel names it.
func initNamespacePidOf(pid int) uint32 {
	if pid <= 0 {
		return 0
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.HasPrefix(line, "NSpid:") {
				continue
			}
			fields := strings.Fields(strings.TrimPrefix(line, "NSpid:"))
			if len(fields) > 0 {
				if pid, err := strconv.ParseUint(fields[0], 10, 32); err == nil {
					return uint32(pid)
				}
			}
			break
		}
	}
	if pid > 0 {
		return uint32(pid)
	}
	return 0
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
	return origOf(value), true, nil
}

// origOf turns what the kernel recorded into the shape the relay uses.
func origOf(value routerOrig) orig {
	o := orig{
		Port:      int(value.Port),
		Pid:       value.Pid,
		ExeDev:    value.ExeDev,
		ExeIno:    value.ExeIno,
		Action:    Action(value.Action),
		InboundID: int(value.InboundID),
		Network:   networkOfProto(value.Proto),
	}
	copy(o.IP[:], value.Addr[12:16])
	return o
}

// mapFlowStore adapts the flows map to what the UDP relay asks for. The entry
// is not consumed: every datagram of a flow names the same address.
type mapFlowStore struct {
	flows *ebpf.Map
}

func (s mapFlowStore) flow(addr uint32) (orig, bool, error) {
	var value routerOrig
	err := s.flows.Lookup(addr, &value)
	if errors.Is(err, ebpf.ErrKeyNotExist) {
		return orig{}, false, nil
	}
	if err != nil {
		return orig{}, false, err
	}
	return origOf(value), true, nil
}

// mapCache writes what the slow path learned into the kernel's maps.
type mapCache struct {
	exe2rules *ebpf.Map
	execRules *ebpf.Map
}

func (c *mapCache) putExe(key exeKey, slot uint32) error {
	return c.exe2rules.Put(key, slot)
}

func (c *mapCache) putExecRules(slot uint32, ruleIndexes []uint32) error {
	var value routerExecRules
	value.N = uint32(len(ruleIndexes))
	copy(value.Rule[:], ruleIndexes)
	return c.execRules.Put(slot, value)
}