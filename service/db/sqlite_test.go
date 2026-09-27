package db

import (
	"path/filepath"
	"sync"
	"testing"

	"github.com/v2rayA/v2rayA/conf"
)

// Close hands the pool back so a later GetDB opens afresh. The reopen runs
// initDB again, so the handle has to stay reachable through the same guard
// that Close used, and a reader racing the close must not see a nil handle.
func TestCloseThenGetDBReopens(t *testing.T) {
	previous := *conf.GetEnvironmentConfig()
	config := previous
	config.Config = t.TempDir()
	conf.SetConfig(config)
	t.Cleanup(func() { conf.SetConfig(previous) })

	first := GetDB()
	if first == nil {
		t.Fatal("first GetDB returned nil")
	}
	path := DBPath()
	if want := filepath.Join(conf.GetEnvironmentConfig().Config, "v2raya.db"); path != want {
		t.Fatalf("db path %q, want %q", path, want)
	}
	if err := Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// a close of an already-closed pool is not an error
	if err := Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}

	second := GetDB()
	if second == nil {
		t.Fatal("GetDB after Close returned nil")
	}
	if second == first {
		t.Fatal("GetDB after Close handed back the closed handle")
	}
	if got := DBPath(); got != path {
		t.Fatalf("db path after reopen %q, want %q", got, path)
	}
	if err := second.Ping(); err != nil {
		t.Fatalf("reopened database is not usable: %v", err)
	}
	t.Cleanup(func() { _ = Close() })
}

func TestConcurrentGetDBAndClose(t *testing.T) {
	previous := *conf.GetEnvironmentConfig()
	config := previous
	config.Config = t.TempDir()
	conf.SetConfig(config)
	t.Cleanup(func() {
		conf.SetConfig(previous)
		_ = Close()
	})

	// Run under -race: the handle and the reopen decision are shared state,
	// and a reader racing the close must never observe a nil handle. It may
	// observe a closed one — a handle taken before Close is not pinned, which
	// is why shutdown drains the HTTP server before it reaches db.Close.
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 16 {
				if i%2 == 0 {
					if err := Close(); err != nil {
						t.Errorf("close: %v", err)
						return
					}
					continue
				}
				if GetDB() == nil {
					t.Error("GetDB returned nil")
					return
				}
			}
		}()
	}
	wg.Wait()
}
