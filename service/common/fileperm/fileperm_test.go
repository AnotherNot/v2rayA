package fileperm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePrivateDirCreatesAndForcesMode(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "config")
	if err := EnsurePrivateDir(dir, 0750); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0750 {
		t.Fatalf("created dir mode = %o, want 750", got)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateDir(dir, 0750); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0750 {
		t.Fatalf("existing dir mode = %o, want 750", got)
	}
}

func TestEnsurePrivateDirRefusesSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateDir(link, 0750); err == nil {
		t.Fatal("a symlinked directory was accepted")
	}
}

func TestEnsurePrivateFileForcesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2raya.db")
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateFile(path, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("file mode = %o, want 600", got)
	}
	if err := EnsurePrivateFile(filepath.Join(t.TempDir(), "missing"), 0600); err != nil {
		t.Fatalf("missing file returned an error: %v", err)
	}
}

func TestEnsurePrivateFileRefusesSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.WriteFile(target, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePrivateFile(link, 0600); err == nil {
		t.Fatal("a symlinked file was accepted")
	}
}
