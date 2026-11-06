package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/server/service"
)

func withSetupDatabase(t *testing.T) string {
	t.Helper()
	previous := *conf.GetEnvironmentConfig()
	config := previous
	config.Config = t.TempDir()
	conf.SetConfig(config)
	_ = db.Close()
	_ = configure.ResetAccounts()
	t.Cleanup(func() {
		_ = configure.ResetAccounts()
		_ = db.Close()
		conf.SetConfig(previous)
	})
	return config.Config
}

func postAccount(t *testing.T, setupToken string) map[string]interface{} {
	t.Helper()
	body := `{"username":"admin","password":"test-password"`
	if setupToken != "" {
		body += `,"setupToken":"` + setupToken + `"`
	}
	body += `}`
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/account", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	PostAccount(ctx)

	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return response
}

func TestPostAccountRequiresTheSetupToken(t *testing.T) {
	configDir := withSetupDatabase(t)
	if err := service.RotateSetupToken(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(configDir, "setup-token"))
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(raw))

	response := postAccount(t, "")
	if got := response["errorCode"]; got != "SETUP_TOKEN_REQUIRED" {
		t.Fatalf("without a token errorCode = %v, want SETUP_TOKEN_REQUIRED", got)
	}
	response = postAccount(t, token+"0")
	if got := response["errorCode"]; got != "SETUP_TOKEN_INVALID" {
		t.Fatalf("with a wrong token errorCode = %v, want SETUP_TOKEN_INVALID", got)
	}
	response = postAccount(t, token)
	if got := response["code"]; got != "SUCCESS" {
		t.Fatalf("with the token response = %v, want SUCCESS", response)
	}
	if _, ok := response["data"].(map[string]interface{})["token"]; !ok {
		t.Fatalf("registration returned no token: %v", response)
	}
	response = postAccount(t, token)
	if got := response["errorCode"]; got != "ACCOUNT_EXISTS" {
		t.Fatalf("second registration errorCode = %v, want ACCOUNT_EXISTS", got)
	}
}
