package common

import (
	"crypto/md5"
	"crypto/sha512"
	"fmt"
	"strings"
	"testing"
)

func legacyHash(password string) string {
	sum := sha512.Sum512_256([]byte(password))
	hash := md5.Sum(sum[:])
	return fmt.Sprintf("%x", hash)
}

func TestCryptoPwdUsesBcrypt(t *testing.T) {
	hash, err := CryptoPwd("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !isBcryptHash(hash) {
		t.Fatalf("hash %q is not bcrypt", hash)
	}
	ok, rehash := CheckPasswordAndRehash("correct horse battery staple", hash)
	if !ok || rehash {
		t.Fatalf("bcrypt check = %v, rehash = %v", ok, rehash)
	}
	if ok, _ := CheckPasswordAndRehash("wrong password", hash); ok {
		t.Fatal("a wrong password was accepted")
	}
}

func TestCryptoPwdRejectsOverlongPassword(t *testing.T) {
	if _, err := CryptoPwd(strings.Repeat("a", 73)); err == nil {
		t.Fatal("a password longer than bcrypt's 72-byte limit was hashed")
	}
}

func TestLegacyHashIsAcceptedAndMarkedForRehash(t *testing.T) {
	hash := legacyHash("old-password")
	ok, rehash := CheckPasswordAndRehash("old-password", hash)
	if !ok || !rehash {
		t.Fatalf("legacy check = %v, rehash = %v", ok, rehash)
	}
	if ok, _ := CheckPasswordAndRehash("wrong", hash); ok {
		t.Fatal("a wrong password was accepted against a legacy hash")
	}
	if ok, rehash := CheckPasswordAndRehash("x", "not-a-hash"); ok || rehash {
		t.Fatal("a malformed hash was accepted")
	}
}
