package jwt

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db"
)

func withTempDatabase(t *testing.T) {
	t.Helper()
	previous := *conf.GetEnvironmentConfig()
	config := previous
	config.Config = t.TempDir()
	conf.SetConfig(config)
	_ = db.Close()
	t.Cleanup(func() {
		_ = db.Close()
		conf.SetConfig(previous)
	})
	if err := db.Open(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	withTempDatabase(t)

	id, refresh, err := CreateSession("alice")
	if err != nil {
		t.Fatal(err)
	}
	if !SessionValid(id) {
		t.Fatal("a fresh session is not valid")
	}

	newID, username, newRefresh, err := RefreshSession(refresh)
	if err != nil {
		t.Fatal(err)
	}
	if newID != id || username != "alice" {
		t.Fatalf("refresh returned %q/%q, want %q/alice", newID, username, id)
	}
	if newRefresh == refresh {
		t.Fatal("refresh token was not rotated")
	}
	if _, _, _, err := RefreshSession(refresh); err == nil {
		t.Fatal("the old refresh token still worked")
	}
	if _, _, _, err := RefreshSession(newRefresh); err != nil {
		t.Fatalf("the rotated refresh token failed: %v", err)
	}

	if err := RevokeSession(id); err != nil {
		t.Fatal(err)
	}
	if SessionValid(id) {
		t.Fatal("a revoked session is still valid")
	}
}

func TestRotateSecretInvalidatesAccessTokens(t *testing.T) {
	withTempDatabase(t)

	id, _, err := CreateSession("bob")
	if err != nil {
		t.Fatal(err)
	}
	duration := time.Minute
	token, err := MakeJWT(map[string]string{"uname": "bob", "jti": id}, &duration)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidateToken(token) {
		t.Fatal("a fresh access token did not validate")
	}
	if err := RotateSecret(); err != nil {
		t.Fatal(err)
	}
	if ValidateToken(token) {
		t.Fatal("an access token survived secret rotation")
	}
}

func TestJWTAuthRejectsRevokedSessionWith401(t *testing.T) {
	withTempDatabase(t)
	gin.SetMode(gin.TestMode)

	id, _, err := CreateSession("carol")
	if err != nil {
		t.Fatal(err)
	}
	duration := time.Minute
	token, err := MakeJWT(map[string]string{"uname": "carol", "jti": id}, &duration)
	if err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	r.Use(JWTAuth(false))
	r.GET("/", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
	request := func() int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, req)
		return recorder.Code
	}
	if got := request(); got != http.StatusOK {
		t.Fatalf("before revoke status = %d, want 200", got)
	}
	if err := RevokeSession(id); err != nil {
		t.Fatal(err)
	}
	if got := request(); got != http.StatusUnauthorized {
		t.Fatalf("after revoke status = %d, want 401", got)
	}
}
