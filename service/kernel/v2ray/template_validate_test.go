package v2ray

import (
	"os"
	"runtime"
	"testing"
)

func TestProcessExecutableMatchesUsesProc(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the check reads /proc")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !processExecutableMatches(os.Getpid(), exe) {
		t.Fatalf("this process was not matched to %s", exe)
	}
	if processExecutableMatches(os.Getpid(), "/bin/sh") {
		t.Fatal("a different executable was accepted")
	}
}
