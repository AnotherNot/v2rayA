package where

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// stubCore puts a fake v2raya_core on PATH and clears the 10 s version cache,
// so each case probes its own binary instead of the previous one's answer.
func stubCore(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fixture is a shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, ServiceNameList[0])
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	v2rayVersion.mu.Lock()
	v2rayVersion.version, v2rayVersion.binPath, v2rayVersion.lastUpdate = "", "", time.Time{}
	v2rayVersion.mu.Unlock()
	t.Cleanup(func() {
		v2rayVersion.mu.Lock()
		v2rayVersion.lastUpdate = time.Time{}
		v2rayVersion.mu.Unlock()
	})
}

func TestGetV2rayServiceVersionReadsOutput(t *testing.T) {
	stubCore(t, "#!/bin/sh\necho 'v2raya_core 1.2.3'\n")

	_, ver, err := GetV2rayServiceVersion()
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if ver != "1.2.3" {
		t.Fatalf("version %q, want %q", ver, "1.2.3")
	}
}

// A core that never prints a version must not wedge the caller: the probe
// gives up on its own deadline rather than blocking on Wait for a process
// that has to be killed, and it says the core was slow.
func TestGetV2rayServiceVersionTimesOutOnSilentCore(t *testing.T) {
	stubCore(t, "#!/bin/sh\nsleep 60\n")

	done := make(chan error, 1)
	go func() {
		_, _, err := GetV2rayServiceVersion()
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a silent core reported a version")
		}
		if want := "within 5 s"; !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention the deadline", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the version probe did not give up on its deadline")
	}
}

// A core that fails outright is reported as a failure, not as unparseable
// output: the distinction tells a user whether to replace the binary.
func TestGetV2rayServiceVersionReportsRunFailure(t *testing.T) {
	stubCore(t, "#!/bin/sh\necho boom >&2\nexit 3\n")

	if _, _, err := GetV2rayServiceVersion(); err == nil {
		t.Fatal("a failing core reported a version")
	}
}
