package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testSecurity() securityConfig {
	return securityConfig{
		allowedOrigins: map[string]struct{}{
			"http://admin.example.com": {},
		},
		allowedHosts: map[string]struct{}{
			"admin.example.com": {},
		},
		localHosts: map[string]struct{}{
			"localhost": {},
			"127.0.0.1": {},
			"::1":       {},
		},
	}
}

func serveSecurity(t *testing.T, middleware gin.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware)
	r.Any("/*path", func(ctx *gin.Context) { ctx.Status(http.StatusOK) })
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)
	return recorder
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		ErrorCode string `json:"errorCode"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
	return body.ErrorCode
}

func TestOriginGuardRejectsCrossSiteMutations(t *testing.T) {
	guard := testSecurity().csrfGuard()
	cases := []struct {
		name       string
		method     string
		origin     string
		fetchSite  string
		wantStatus int
		wantCode   string
	}{
		{name: "cross-origin post", method: http.MethodPost, origin: "http://evil.example", wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_FORBIDDEN"},
		{name: "same host", method: http.MethodPost, origin: "http://localhost:2017", wantStatus: http.StatusOK},
		{name: "allowed origin", method: http.MethodPost, origin: "http://admin.example.com", wantStatus: http.StatusOK},
		{name: "no origin cli", method: http.MethodPost, wantStatus: http.StatusOK},
		{name: "cross-site without origin", method: http.MethodPost, fetchSite: "cross-site", wantStatus: http.StatusForbidden, wantCode: "CROSS_ORIGIN_FORBIDDEN"},
		{name: "safe get", method: http.MethodGet, origin: "http://evil.example", wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/account", nil)
			req.Host = "localhost:2017"
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			recorder := serveSecurity(t, guard, req)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if tc.wantCode != "" && errorCode(t, recorder) != tc.wantCode {
				t.Fatalf("errorCode = %q, want %q", errorCode(t, recorder), tc.wantCode)
			}
		})
	}
}

func TestHostGuardRejectsUnknownNames(t *testing.T) {
	guard := testSecurity().hostGuard()
	cases := []struct {
		host       string
		wantStatus int
	}{
		{"evil.example", http.StatusMisdirectedRequest},
		{"localhost:2017", http.StatusOK},
		{"127.0.0.1:2017", http.StatusOK},
		{"[::1]:2017", http.StatusOK},
		{"admin.example.com", http.StatusOK},
		{"203.0.113.10:2017", http.StatusOK},
		{"0.0.0.0:2017", http.StatusMisdirectedRequest},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tc.host
			recorder := serveSecurity(t, guard, req)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.wantStatus)
			}
		})
	}
}

func TestJSONBodyGuardRequiresJSON(t *testing.T) {
	guard := jsonBodyGuard()
	cases := []struct {
		name        string
		method      string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "text plain", method: http.MethodPost, contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType},
		{name: "json", method: http.MethodPost, contentType: "application/json", body: `{}`, wantStatus: http.StatusOK},
		{name: "problem json", method: http.MethodPost, contentType: "application/problem+json", body: `{}`, wantStatus: http.StatusOK},
		{name: "delete without body", method: http.MethodDelete, wantStatus: http.StatusOK},
		{name: "get is not a body request", method: http.MethodGet, contentType: "text/plain", body: `x`, wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/test", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			recorder := serveSecurity(t, guard, req)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestHostAndOriginNormalization(t *testing.T) {
	if got := hostOnly("[::1]:2017"); got != "::1" {
		t.Errorf("hostOnly IPv6 = %q", got)
	}
	if got := hostOnly("localhost."); got != "localhost" {
		t.Errorf("hostOnly trailing dot = %q", got)
	}
	if got := normalizeOrigin("HTTP://Admin.Example.COM/"); got != "http://admin.example.com" {
		t.Errorf("normalizeOrigin = %q", got)
	}
	if got := normalizeOrigin("file:///etc/passwd"); got != "" {
		t.Errorf("non-http origin = %q, want empty", got)
	}
	if !hasWildcardOrigin("http://ok.example, *") {
		t.Error("wildcard origin was not detected")
	}
	if hasWildcardOrigin("http://ok.example") {
		t.Error("an explicit origin was treated as a wildcard")
	}
}
