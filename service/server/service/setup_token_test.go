package service

import (
	"os"
	"strings"
	"testing"

	"github.com/v2rayA/v2rayA/db"
)

func TestSetupTokenLifecycle(t *testing.T) {
	_ = db.Delete(setupTokenBucket, setupTokenKey)
	_ = os.Remove(SetupTokenPath())
	t.Cleanup(func() {
		_ = db.Delete(setupTokenBucket, setupTokenKey)
		_ = os.Remove(SetupTokenPath())
	})

	if err := RotateSetupToken(); err != nil {
		t.Fatalf("RotateSetupToken: %v", err)
	}
	info, err := os.Stat(SetupTokenPath())
	if err != nil {
		t.Fatalf("stat setup token: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("setup token mode = %o, want 600", got)
	}
	raw, err := os.ReadFile(SetupTokenPath())
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(raw))
	if len(token) != 64 {
		t.Fatalf("setup token length = %d, want 64 hex chars", len(token))
	}
	if !VerifySetupToken(token) {
		t.Fatal("the generated token did not verify")
	}
	if VerifySetupToken(token + "0") {
		t.Fatal("a changed token verified")
	}
	if VerifySetupToken("") {
		t.Fatal("an empty token verified")
	}

	ConsumeSetupToken()
	if VerifySetupToken(token) {
		t.Fatal("the consumed token still verified")
	}
	if _, err := os.Stat(SetupTokenPath()); !os.IsNotExist(err) {
		t.Fatalf("setup token file survived consumption: %v", err)
	}
}
