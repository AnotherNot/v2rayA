//go:build linux

package approuter

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"github.com/v2rayA/v2rayA/pkg/util/log"
	"golang.org/x/sys/unix"
)

// The object is generated from probe.bpf.c and committed, so the build needs
// no clang. See the comment at the top of that file for the command.
//
//go:embed bpf/probe.bpf.o
var probeObject []byte

// reportInterval is how often an attached probe writes its counters to the
// log. Long enough not to bury the log of a probe left running by accident.
const reportInterval = 30 * time.Second

// Probe counts connects per executable and lets every one of them through.
type Probe struct {
	collection  *ebpf.Collection
	connects    *ebpf.Map
	executables *ebpf.Map
	links       []io.Closer
	stop        chan struct{}
	done        chan struct{}
	mu          sync.Mutex
	closed      bool
}

// probeExeKey mirrors struct probe_exe_key. It is a separate type from exeKey
// only because the probe's map is its own; the layout is the same.
type probeExeKey struct {
	Dev uint32
	Pad uint32
	Ino uint64
}

// Mirrors struct probe_exe_stat. No padding on either side, so the sizes agree.
type exeStat struct {
	Comm  [16]byte
	Flows uint64
}

// StartProbe attaches the probe to the supplied cgroup v2 subtree. Real BPF
// links are used and nothing is pinned: closing the descriptors must also
// detach the programs after a crash.
func StartProbe(cgroup string) (*Probe, error) {
	dir, err := os.Open(cgroup)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	var stat unix.Statfs_t
	if err := unix.Fstatfs(int(dir.Fd()), &stat); err != nil {
		return nil, err
	}
	if stat.Type != unix.CGROUP2_SUPER_MAGIC {
		return nil, fmt.Errorf("%s is not on a cgroup v2 mount", cgroup)
	}
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(probeObject))
	if err != nil {
		return nil, fmt.Errorf("read the embedded app-router probe: %w", err)
	}
	collection, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("load the app-router probe: %w", err)
	}
	p := &Probe{
		collection: collection,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	cleanup := func() {
		for _, l := range p.links {
			_ = l.Close()
		}
		collection.Close()
	}
	for _, name := range []string{"connects", "executables"} {
		if collection.Maps[name] == nil {
			cleanup()
			return nil, fmt.Errorf("the embedded app-router probe has no %s map", name)
		}
	}
	p.connects = collection.Maps["connects"]
	p.executables = collection.Maps["executables"]
	for _, hook := range []struct {
		program string
		kind    ebpf.AttachType
	}{
		{"probe_connect4", ebpf.AttachCGroupInet4Connect},
		{"probe_connect6", ebpf.AttachCGroupInet6Connect},
	} {
		program, ok := collection.Programs[hook.program]
		if !ok {
			cleanup()
			return nil, fmt.Errorf("the embedded app-router probe has no %s program", hook.program)
		}
		attached, err := link.AttachRawLink(link.RawLinkOptions{
			Target: int(dir.Fd()), Program: program, Attach: hook.kind,
		})
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("attach the app-router probe (%s): %w", hook.program, err)
		}
		p.links = append(p.links, attached)
	}
	go p.report()
	log.Info("app-router probe: attached to %s; counters every %v", cgroup, reportInterval)
	return p, nil
}

// Snapshot reads the maps the probe fills. It refuses to read a closed probe
// rather than return zeroes that look like an idle machine.
func (p *Probe) Snapshot() (Snapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return Snapshot{}, errors.New("the app-router probe is closed")
	}
	var snapshot Snapshot
	var counter uint64
	if err := p.connects.Lookup(uint32(0), &counter); err != nil {
		return Snapshot{}, err
	}
	snapshot.Connects4 = counter
	if err := p.connects.Lookup(uint32(1), &counter); err != nil {
		return Snapshot{}, err
	}
	snapshot.Connects6 = counter
	iter := p.executables.Iterate()
	var key probeExeKey
	var stat exeStat
	for iter.Next(&key, &stat) {
		snapshot.Executables = append(snapshot.Executables, Executable{
			Device: key.Dev,
			Inode:  key.Ino,
			Comm:   strings.TrimRight(string(stat.Comm[:]), "\x00"),
			Flows:  stat.Flows,
		})
	}
	if err := iter.Err(); err != nil {
		return Snapshot{}, err
	}
	snapshot.sortExecutables()
	return snapshot, nil
}

func (p *Probe) report() {
	defer close(p.done)
	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			snapshot, err := p.Snapshot()
			if err != nil {
				log.Warn("app-router probe: read counters: %v", err)
				continue
			}
			log.Info("app-router probe: %v", snapshot)
		}
	}
}

func (p *Probe) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.stop)
	p.mu.Unlock()
	// The reporter can hold the lock in Snapshot; stop it before closing the
	// maps it reads.
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	var errs []error
	for i := len(p.links) - 1; i >= 0; i-- {
		errs = append(errs, p.links[i].Close())
	}
	p.links = nil
	p.collection.Close()
	p.collection = nil
	p.connects = nil
	p.executables = nil
	return errors.Join(errs...)
}