package db

import (
	"errors"
	"os"
	"testing"

	"github.com/v2rayA/v2rayA/conf"
)

func TestDatabaseFilesAreOwnerOnly(t *testing.T) {
	previous := *conf.GetEnvironmentConfig()
	config := previous
	config.Config = t.TempDir()
	conf.SetConfig(config)
	_ = Close()
	t.Cleanup(func() {
		_ = Close()
		conf.SetConfig(previous)
	})

	if err := Open(); err != nil {
		t.Fatal(err)
	}
	_ = GetDB()

	path := DBPath()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		info, err := os.Stat(path + suffix)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("stat %v: %v", path+suffix, err)
		}
		if got := info.Mode().Perm(); got != 0600 {
			t.Fatalf("%v mode = %o, want 600", path+suffix, got)
		}
	}

	// An older installation has 0644 files; reopening must repair them.
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := Close(); err != nil {
		t.Fatal(err)
	}
	if err := Open(); err != nil {
		t.Fatal(err)
	}
	_ = GetDB()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("database mode after reopen = %o, want 600", got)
	}
}
