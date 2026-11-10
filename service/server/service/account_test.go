package service

import (
	"crypto/md5"
	"crypto/sha512"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/db"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/server/jwt"
)

func TestLoginTokenSuppliesIdentity(t *testing.T) {
	token, _, err := Register("claim-test", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Delete("accounts", "claim-test")
	r := gin.New()
	r.Use(jwt.JWTAuth(false))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, c.GetString("Name")) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, req)
	if response.Code != http.StatusOK || response.Body.String() != "claim-test" {
		t.Fatalf("identity response: %d %q", response.Code, response.Body.String())
	}
}

func TestLoginUpgradesALegacyPasswordHash(t *testing.T) {
	const username = "legacy-hash-test"
	const password = "legacy-password-123"
	sum := sha512.Sum512_256([]byte(password))
	legacy := fmt.Sprintf("%x", md5.Sum(sum[:]))
	if err := configure.SetAccount(username, legacy); err != nil {
		t.Fatal(err)
	}
	defer db.Delete("accounts", username)

	if _, _, err := Login(username, password); err != nil {
		t.Fatalf("login with the legacy hash: %v", err)
	}
	upgraded, err := configure.GetPasswordOfAccount(username)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(upgraded, "$2") {
		t.Fatalf("password hash was not upgraded: %q", upgraded)
	}
	if _, _, err := Login(username, password); err != nil {
		t.Fatalf("login after the upgrade: %v", err)
	}
}

func TestNewPasswordsNeedTwelveCharacters(t *testing.T) {
	if ok, _ := ValidPasswordLength("elevenchars"); ok {
		t.Fatal("an 11-character password was accepted")
	}
	if ok, _ := ValidPasswordLength("twelve-chars"); !ok {
		t.Fatal("a 12-character password was refused")
	}
	if ok, _ := ValidPasswordLength(strings.Repeat("a", 73)); ok {
		t.Fatal("a password over bcrypt's limit was accepted")
	}
}
