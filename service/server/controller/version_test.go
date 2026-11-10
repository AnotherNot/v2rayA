package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/pkg/server/jwt"
)

func TestGetSettingReportsGFWListAndGeoSiteVersionsSeparately(t *testing.T) {
	assetDir := t.TempDir()
	t.Setenv("XRAY_LOCATION_ASSET", assetDir)
	date := time.Date(2026, time.September, 15, 12, 0, 0, 0, time.Local)
	for _, name := range []string{"LoyalsoldierSite.dat", "geosite.dat"} {
		path := filepath.Join(assetDir, name)
		if err := os.WriteFile(path, []byte("dat"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, date, date); err != nil {
			t.Fatal(err)
		}
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/setting", nil)
	GetSetting(ctx)
	var response struct {
		Data struct {
			LocalGFWListVersion string `json:"localGFWListVersion"`
			LocalGeositeVersion string `json:"localGeositeVersion"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.LocalGFWListVersion != "2026-09-15" {
		t.Errorf("localGFWListVersion = %q, want 2026-09-15", response.Data.LocalGFWListVersion)
	}
	if response.Data.LocalGeositeVersion != "2026-09-15" {
		t.Errorf("localGeositeVersion = %q, want 2026-09-15", response.Data.LocalGeositeVersion)
	}
}

func TestGetVersionHidesHostFactsWithoutASession(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/version", nil)
	GetVersion(ctx)
	var response struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"isRoot", "tunSupported", "docker", "coreVersion"} {
		if _, ok := response.Data[key]; ok {
			t.Errorf("%s was returned without a session", key)
		}
	}
	for _, key := range []string{"version", "lite", "hasAccounts"} {
		if _, ok := response.Data[key]; !ok {
			t.Errorf("%s is missing from the minimal answer", key)
		}
	}
}

func TestGetVersionIncludesDockerFlagWithASession(t *testing.T) {
	sessionID, _, err := jwt.CreateSession("version-test")
	if err != nil {
		t.Fatal(err)
	}
	defer jwt.RevokeSession(sessionID)
	ttl := jwt.AccessTTL
	token, err := jwt.MakeJWT(map[string]string{"uname": "version-test", "jti": sessionID}, &ttl)
	if err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/version", nil)
	ctx.Request.Header.Set("Authorization", "Bearer "+token)
	GetVersion(ctx)
	var response struct {
		Data struct {
			Docker *bool `json:"docker"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Docker == nil || *response.Data.Docker != common.IsDocker() {
		t.Fatalf("docker = %v, want %v", response.Data.Docker, common.IsDocker())
	}
}

type versionTransport func(*http.Request) (*http.Response, error)

func (f versionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRemoteVersionRequestHasDeadline(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	called := false
	http.DefaultTransport = versionTransport(func(r *http.Request) (*http.Response, error) {
		called = true
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("remote version request has no deadline")
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader(`[]`)), Header: make(http.Header)}, nil
	})
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	GetRemoteGFWListVersion(ctx)
	if !called {
		t.Fatal("remote version was not queried")
	}
}
