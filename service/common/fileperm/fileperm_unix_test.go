//go:build unix

package fileperm

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSecureUmaskCreatesOwnerOnlyFiles(t *testing.T) {
	old := syscall.Umask(0022)
	defer syscall.Umask(old)

	SecureUmask()
	path := filepath.Join(t.TempDir(), "created")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("file created with mode %o, want 600", got)
	}
}
