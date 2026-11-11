package service

import (
	"crypto/md5"
	"crypto/sha512"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
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

func TestLoginRefusesALegacyPasswordHash(t *testing.T) {
	const username = "legacy-hash-test"
	const password = "legacy-password-123"
	// md5(sha512_256(password)), the scheme older releases stored
	sum := sha512.Sum512_256([]byte(password))
	legacy := fmt.Sprintf("%x", md5.Sum(sum[:]))
	if err := configure.SetAccount(username, legacy); err != nil {
		t.Fatal(err)
	}
	defer db.Delete("accounts", username)

	// The account still has to count as existing: reporting it as absent would
	// reopen the first-run registration on an upgraded installation.
	if !configure.HasAnyAccounts() {
		t.Fatal("a legacy account stopped counting as existing")
	}
	// GetAccounts feeds the VMess UUID derived from the accounts, so a legacy
	// account has to keep appearing there or its clients would lose the UUID.
	accounts, listErr := configure.GetAccounts()
	if listErr != nil {
		t.Fatal(listErr)
	}
	listed := false
	for _, account := range accounts {
		if account[0] == username {
			listed = true
		}
	}
	if !listed {
		t.Fatal("the legacy account disappeared from the account list")
	}
	_, _, err := Login(username, password)
	var coded *common.CodedError
	if !errors.As(err, &coded) || coded.Code != "LEGACY_PASSWORD_HASH" {
		t.Fatalf("login error = %v, want LEGACY_PASSWORD_HASH", err)
	}
	stored, err := configure.GetPasswordOfAccount(username)
	if err != nil {
		t.Fatal(err)
	}
	if stored != legacy {
		t.Fatalf("the stored hash changed to %q", stored)
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
