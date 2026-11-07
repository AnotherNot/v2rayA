package redact

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestURLDropsCredentialsAndPath(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "userinfo query and fragment",
			raw:  "https://user:pass@example.com:8443/sub/secret-path?token=top-secret#frag",
			want: "https://example.com:8443",
		},
		{
			name: "plain subscription",
			raw:  "http://127.0.0.1:2017/api/sub?token=abc",
			want: "http://127.0.0.1:2017",
		},
		{
			name: "path only",
			raw:  "https://provider.example/sub/0123456789abcdef",
			want: "https://provider.example",
		},
		{name: "empty"},
		{name: "not a url", raw: "not a url at all", want: "[redacted]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := URL(tc.raw); got != tc.want {
				t.Fatalf("URL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestLinkKeepsOnlyTheScheme(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"vmess://eyJ2IjoiMiIsInBzIjoic2VjcmV0In0=", "vmess://[redacted]"},
		{"ss://YWVzLTI1Ni1nY206c2VjcmV0@host:8388#name", "ss://[redacted]"},
		{"vless://uuid@host:443?type=tcp#name", "vless://[redacted]"},
		{"", ""},
		{"opaque", "[redacted]"},
	}
	for _, tc := range cases {
		if got := Link(tc.raw); got != tc.want {
			t.Fatalf("Link(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestHostNamesTheSource(t *testing.T) {
	if got := Host("https://user:pass@example.com:8443/path?token=x"); got != "example.com" {
		t.Fatalf("Host = %q", got)
	}
	if got := Host("not a url"); got != "" {
		t.Fatalf("Host of an invalid url = %q", got)
	}
}

func TestErrorRedactsTheRequestURL(t *testing.T) {
	cause := errors.New("dial failed")
	transport := &url.Error{
		Op:  "Get",
		URL: "https://user:pass@example.com/sub/secret-path?token=TOP_SECRET",
		Err: cause,
	}
	redacted := Error(transport)
	text := redacted.Error()
	for _, secret := range []string{"TOP_SECRET", "secret-path", "user:pass"} {
		if strings.Contains(text, secret) {
			t.Fatalf("redacted error %q still contains %q", text, secret)
		}
	}
	if !strings.Contains(text, "https://example.com") {
		t.Fatalf("redacted error %q lost the host", text)
	}
	if !errors.Is(redacted, cause) {
		t.Fatal("redacted error lost its cause")
	}
}
