//go:build linux

package approuter

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/v2rayA/v2rayA/kernel/bounddevice"
	"golang.org/x/sys/unix"
)

// A stale committed object fails here, before anyone runs the daemon as root:
// the loader looks these up by name.
func TestProbeObjectHasTheExpectedProgramsAndMaps(t *testing.T) {
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(probeObject))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"probe_connect4", "probe_connect6"} {
		if spec.Programs[name] == nil {
			t.Errorf("the object has no %s program", name)
		}
	}
	for _, name := range []string{"connects", "executables"} {
		if spec.Maps[name] == nil {
			t.Errorf("the object has no %s map", name)
		}
	}
}

func TestStartProbeRejectsANonCgroupMount(t *testing.T) {
	if _, err := StartProbe(t.TempDir()); err == nil {
		t.Fatal("a regular directory must not be accepted as a cgroup mount")
	}
}

// Opt-in privileged test. The probe is attached ONLY to a new, disposable
// cgroup, never to the host root cgroup; the child connects to loopback only.
func TestNativeAppRouterProbe(t *testing.T) {
	if os.Getenv("V2RAYA_BPF_TEST") != "1" {
		t.Skip("set V2RAYA_BPF_TEST=1 and run as root to verify with the kernel")
	}
	if os.Geteuid() != 0 {
		t.Fatal("native BPF verification needs root")
	}
	cgroup := filepath.Join("/sys/fs/cgroup", fmt.Sprintf("v2raya-approuter-test-%d", os.Getpid()))
	if err := os.Mkdir(cgroup, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Remove(cgroup); err != nil {
			t.Errorf("remove test cgroup: %v", err)
		}
	})

	first, err := StartProbe(cgroup)
	if err != nil {
		t.Fatalf("BPF load/attach failed: %+v", err)
	}
	t.Cleanup(func() { first.Close() })
	runProbeChild(t, cgroup)
	snapshot, err := first.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Connects4 == 0 || snapshot.Connects6 == 0 {
		t.Fatalf("the probe counted %v; want an IPv4 and an IPv6 connect", snapshot)
	}
	if len(snapshot.Executables) == 0 {
		t.Fatal("no executable identity was read from the socket")
	}
	if snapshot.Executables[0].Comm == "" {
		t.Fatalf("the executable has no name: %+v", snapshot.Executables[0])
	}

	// The REDIRECT guard is a cgroup/sock_addr program too. Both have to sit
	// on the same cgroup at once, or the router can never run beside it.
	guard, err := bounddevice.Start(cgroup)
	if err != nil {
		t.Fatalf("bound-device guard on the same cgroup: %v", err)
	}
	defer guard.Close()
	second, err := StartProbe(cgroup)
	if err != nil {
		t.Fatalf("second probe on the same cgroup: %v", err)
	}
	defer second.Close()
	before, err := second.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	runProbeChild(t, cgroup)
	after, err := second.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.Connects4 <= before.Connects4 {
		t.Fatalf("the second probe did not count beside the first: %v -> %v", before, after)
	}
	// Closing one link must not detach the other.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	before = after
	runProbeChild(t, cgroup)
	after, err = second.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.Connects4 <= before.Connects4 {
		t.Fatalf("closing the first probe detached the second: %v -> %v", before, after)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Snapshot(); err == nil {
		t.Fatal("a closed probe returned a snapshot")
	}
}

func runProbeChild(t *testing.T, cgroup string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestAppRouterProbeChild$")
	cmd.Env = append(os.Environ(), "V2RAYA_BPF_CHILD=1", "V2RAYA_BPF_CHILD_CGROUP="+cgroup)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("socket child: %v\n%s", err, output)
	}
}

func TestAppRouterProbeChild(t *testing.T) {
	if os.Getenv("V2RAYA_BPF_CHILD") != "1" {
		t.Skip("run by the isolated cgroup test")
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("V2RAYA_BPF_CHILD_CGROUP"), "cgroup.procs"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
		fd, err := unix.Socket(family, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, unix.IPPROTO_TCP)
		if err != nil {
			t.Fatal(err)
		}
		var target unix.Sockaddr = &unix.SockaddrInet4{Port: 1, Addr: [4]byte{127, 0, 0, 1}}
		if family == unix.AF_INET6 {
			target = &unix.SockaddrInet6{Port: 1, Addr: [16]byte{15: 1}}
		}
		// Nothing listens on port 1; the connect hook runs before the refusal,
		// which is what the probe counts.
		if err := unix.Connect(fd, target); err != nil && err != unix.ECONNREFUSED && err != unix.EINPROGRESS {
			t.Fatalf("connect: %v", err)
		}
		unix.Close(fd)
	}
}