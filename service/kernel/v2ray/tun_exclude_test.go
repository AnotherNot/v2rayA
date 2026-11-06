package v2ray

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLookPathForShellUsesSystemDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("system-directory lookup is Unix-only")
	}
	path, err := lookPathForShell("bash")
	if err != nil {
		t.Skipf("bash is not installed: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("resolved shell %q is not absolute", path)
	}
	if strings.Contains(path, "/home/") {
		t.Fatalf("resolved shell %q from a user directory", path)
	}
}

func TestSanitizedShellEnvDropsStartupHooks(t *testing.T) {
	t.Setenv("BASH_ENV", "/tmp/evil-bash-env")
	t.Setenv("ENV", "/tmp/evil-env")
	env := sanitizedShellEnv()
	var sawPath bool
	for _, kv := range env {
		if strings.HasPrefix(kv, "BASH_ENV=") || strings.HasPrefix(kv, "ENV=") {
			t.Fatalf("startup hook survived: %q", kv)
		}
		if strings.HasPrefix(kv, "PATH=") {
			sawPath = true
			if runtime.GOOS != "windows" && !strings.Contains(kv, "/usr/bin") {
				t.Fatalf("PATH was not replaced with the system path: %q", kv)
			}
		}
	}
	if !sawPath {
		t.Fatal("sanitized environment has no PATH")
	}
}
