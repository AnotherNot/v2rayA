package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

const (
	setupTokenBucket = "system"
	setupTokenKey    = "setupTokenHash"
	setupTokenFile   = "setup-token"
)

// SetupTokenPath is where the first-run setup token is written for the
// operator who cannot read the service log. The file is removed as soon as
// the first account is registered.
func SetupTokenPath() string {
	return filepath.Join(conf.GetEnvironmentConfig().Config, setupTokenFile)
}

func hashSetupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// EnsureSetupToken prints the first-run token when the instance has no
// account yet. It is a no-op once an account exists, and an existing token
// is not replaced: a restart must not invalidate the token an operator has
// already copied.
func EnsureSetupToken() error {
	if configure.HasAnyAccounts() {
		ConsumeSetupToken()
		return nil
	}
	var stored string
	err := db.Get(setupTokenBucket, setupTokenKey, &stored)
	if err == nil && len(stored) == sha256.Size*2 {
		return nil
	}
	if err != nil && !strings.Contains(err.Error(), "key is not found") {
		// The row may exist and be unreadable right now. Replacing it would
		// invalidate a token the operator already copied, so leave it alone.
		return fmt.Errorf("read setup token: %w", err)
	}
	return generateSetupToken()
}

// RotateSetupToken discards the previous token and prints a new one. It is
// called after --reset-password, where the old token must not survive.
func RotateSetupToken() error {
	ConsumeSetupToken()
	return generateSetupToken()
}

// VerifySetupToken compares a submitted token with the stored hash in
// constant time. A missing or unreadable hash never matches.
func VerifySetupToken(token string) bool {
	if token == "" {
		return false
	}
	var stored string
	if err := db.Get(setupTokenBucket, setupTokenKey, &stored); err != nil {
		return false
	}
	want := hashSetupToken(token)
	return subtle.ConstantTimeCompare([]byte(want), []byte(stored)) == 1
}

// ConsumeSetupToken removes the token from the database and from disk. It
// is safe to call when neither exists.
func ConsumeSetupToken() {
	_ = db.Delete(setupTokenBucket, setupTokenKey)
	_ = os.Remove(SetupTokenPath())
}

func generateSetupToken() error {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("generate setup token: %w", err)
	}
	token := hex.EncodeToString(buf)
	if err := db.Set(setupTokenBucket, setupTokenKey, hashSetupToken(token)); err != nil {
		return fmt.Errorf("store setup token: %w", err)
	}
	if err := writeSetupTokenFile(token); err != nil {
		// The log is the primary channel; the file is a convenience for
		// operators who start the service detached from a terminal.
		log.Warn("cannot write the setup token file %v: %v", SetupTokenPath(), err)
	}
	log.Alert("first-run setup token: %s", token)
	log.Alert("use this token to create the first account; it is also at %v", SetupTokenPath())
	return nil
}

func writeSetupTokenFile(token string) error {
	path := SetupTokenPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.WriteString(strings.TrimSpace(token) + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
