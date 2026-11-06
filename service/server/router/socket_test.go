package router

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPrepareUnixSocketRefusesNonSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2raya.sock")
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareUnixSocket(path); err == nil {
		t.Fatal("a regular file was accepted as a socket path")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the regular file was removed: %v", err)
	}
}

func TestPrepareUnixSocketReplacesOwnedSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2raya.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if err := prepareUnixSocket(path); err != nil {
		t.Fatalf("prepareUnixSocket: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("the old socket survived: %v", err)
	}
}

func TestPrepareUnixSocketRefusesWorldWritableDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := prepareUnixSocket(filepath.Join(dir, "v2raya.sock")); err == nil {
		t.Fatal("a world-writable directory was accepted")
	}
}

func TestPrepareUnixSocketAllowsStickyWorldWritableDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := prepareUnixSocket(filepath.Join(dir, "v2raya.sock")); err != nil {
		t.Fatalf("a sticky world-writable directory was refused: %v", err)
	}
}

func TestUnixSocketMode(t *testing.T) {
	mode, gid, err := unixSocketMode("")
	if err != nil || mode != 0600 || gid != -1 {
		t.Fatalf("owner-only mode = %v %v %v", mode, gid, err)
	}
	if _, _, err := unixSocketMode("this-group-does-not-exist"); err == nil {
		t.Fatal("an unknown group was accepted")
	}
}

func TestSecureUnixSocketSetsOwnerOnlyMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v2raya.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := secureUnixSocket(path, ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("socket mode = %o, want 600", got)
	}
}

func TestPeerCredGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	guard := peerCredGuard()
	serve := func(ctx context.Context) *httptest.ResponseRecorder {
		r := gin.New()
		r.Use(guard)
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, req)
		return recorder
	}

	ownerCtx := context.WithValue(context.Background(), peerCredKey{}, peerCred{uid: uint32(os.Geteuid())})
	if got := serve(ownerCtx); got.Code != http.StatusOK {
		t.Fatalf("owner status = %d, want 200", got.Code)
	}
	otherCtx := context.WithValue(context.Background(), peerCredKey{}, peerCred{uid: uint32(os.Geteuid()) + 1})
	if got := serve(otherCtx); got.Code != http.StatusForbidden {
		t.Fatalf("other user status = %d, want 403", got.Code)
	}
	if got := serve(context.Background()); got.Code != http.StatusOK {
		t.Fatalf("TCP request status = %d, want 200", got.Code)
	}
}
