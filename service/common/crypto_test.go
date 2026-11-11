package common

import (
	"strings"
	"testing"
)

func TestCryptoPwdUsesBcrypt(t *testing.T) {
	hash, err := CryptoPwd("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !isBcryptHash(hash) {
		t.Fatalf("hash %q is not bcrypt", hash)
	}
	if !CheckPassword("correct horse battery staple", hash) {
		t.Fatal("the right password was refused")
	}
	if CheckPassword("wrong password", hash) {
		t.Fatal("a wrong password was accepted")
	}
}

func TestCryptoPwdRejectsOverlongPassword(t *testing.T) {
	if _, err := CryptoPwd(strings.Repeat("a", 73)); err == nil {
		t.Fatal("a password longer than bcrypt's 72-byte limit was hashed")
	}
}

// The unsalted MD5-derived format older releases stored is not verified: it is
// fast enough to search offline, and the account is reset instead.
func TestCheckPasswordRefusesTheLegacyFormat(t *testing.T) {
	const legacy = "5f4dcc3b5aa765d61d8327deb882cf99"
	if CheckPassword("password", legacy) {
		t.Fatal("a legacy hash was accepted")
	}
	if CheckPassword("password", "not-a-hash") {
		t.Fatal("a malformed hash was accepted")
	}
}
