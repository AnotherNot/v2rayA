package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db/configure"
)

func useDnsTestDB(t *testing.T) {
	t.Helper()
	previous := *conf.GetEnvironmentConfig()
	c := previous
	c.Config = t.TempDir()
	conf.SetConfig(c)
	t.Cleanup(func() {
		conf.SetConfig(previous)
		configure.SetDnsSource(&configure.DnsSource{})
	})
}

func dnsServer(t *testing.T, body string) (*httptest.Server, func() int) {
	t.Helper()
	var mu sync.Mutex
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

// The versioned document is the form a source with a release history uses; the
// bare array is what a hand-written file looks like. Both have to work, and the
// array has to get a version from its own content so it too is recognised as
// unchanged on a second run.
func TestImportDnsSourceAcceptsBothShapes(t *testing.T) {
	useDnsTestDB(t)
	versioned := `{"version":"RUdns1","remarks":"Россия","rules":[
	  {"upstream":"127.0.0.1","domain":"geosite:private","outbound":"direct","action":"route"},
	  {"upstream":"77.88.8.8","domain":"geosite:cn","outbound":"direct","action":"route"},
	  {"upstream":"1.0.0.1","outbound":"proxy","action":"route"}
	]}`
	srv, _ := dnsServer(t, versioned)

	src, err := ImportDnsSource(srv.URL)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if src.Version != "RUdns1" {
		t.Errorf("version %q, want RUDns1", src.Version)
	}
	if src.Remarks != "Россия" {
		t.Errorf("remarks %q", src.Remarks)
	}
	rules := configure.GetDnsRulesNotNil()
	if len(rules) != 3 {
		t.Fatalf("stored %d rules, want 3: %+v", len(rules), rules)
	}
	// the generator defaults the legacy Server field from Upstream, and the
	// stored rules must survive the same migration
	if rules[1].Outbound != "direct" {
		t.Errorf("second rule outbound %q", rules[1].Outbound)
	}
	if _, err := ImportDnsSource(srv.URL); !errors.Is(err, ErrDnsSourceUnchanged) {
		t.Errorf("re-importing an unchanged source returned %v", err)
	}

	// a bare array gets a content-derived version
	bare := `[{"upstream":"9.9.9.9","outbound":"direct"}]`
	bareSrv, _ := dnsServer(t, bare)
	if _, err := ImportDnsSource(bareSrv.URL); err != nil {
		t.Fatalf("bare array: %v", err)
	}
	if got := GetDnsSource().Version; !strings.HasPrefix(got, "sha256:") {
		t.Errorf("a bare array got version %q, want a content hash", got)
	}
	if _, err := ImportDnsSource(bareSrv.URL); !errors.Is(err, ErrDnsSourceUnchanged) {
		t.Errorf("a bare array was not recognised as unchanged: %v", err)
	}
}

// A rule the generator would drop turns into a query that nothing answers, so
// the import has to stop rather than store a set that quietly does nothing.
func TestImportDnsSourceRejectsUnusableRules(t *testing.T) {
	useDnsTestDB(t)
	cases := map[string]string{
		"no rules":        `{"version":"v","rules":[]}`,
		"no upstream":     `{"version":"v","rules":[{"domain":"geosite:cn","outbound":"direct"}]}`,
		"unknown action":  `{"version":"v","rules":[{"upstream":"1.1.1.1","action":"deny"}]}`,
		"empty document":  `   `,
		"not json":        `{`,
		"not a rule list": `{"version":"v","rules":"nope"}`,
	}
	for name, body := range cases {
		srv, _ := dnsServer(t, body)
		if _, err := ImportDnsSource(srv.URL); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ImportDnsSource(""); err == nil {
		t.Error("an empty URL was accepted")
	}
	if _, err := ImportDnsSource("http://127.0.0.1:1/none"); err == nil {
		t.Error("an unreachable source was accepted")
	}
}

// An action the core understands but this project does not is refused by name,
// because accepting it would store a rule the generator ignores.
func TestImportDnsSourceNamesTheUnknownAction(t *testing.T) {
	useDnsTestDB(t)
	srv, _ := dnsServer(t, `{"version":"v","rules":[{"upstream":"1.1.1.1","action":"sinkhole"}]}`)
	_, err := ImportDnsSource(srv.URL)
	if err == nil {
		t.Fatal("an unknown action was accepted")
	}
	if !strings.Contains(err.Error(), "sinkhole") {
		t.Errorf("error %q does not name the action", err)
	}
}

func TestSyncDnsSourceFollowsTheSourceVersion(t *testing.T) {
	useDnsTestDB(t)
	body := `{"version":"v1","rules":[{"upstream":"1.1.1.1","outbound":"direct"}]}`
	srv, hits := dnsServer(t, body)

	if err := configure.SetDnsSource(&configure.DnsSource{URL: srv.URL, Version: "older"}); err != nil {
		t.Fatal(err)
	}
	if err := SyncDnsSource(); err != nil {
		t.Fatalf("sync after a version change: %v", err)
	}
	if got := GetDnsSource().Version; got != "v1" {
		t.Errorf("version %q, want v1", got)
	}
	stored := configure.GetDnsRulesNotNil()

	before := hits()
	if err := SyncDnsSource(); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if after := hits(); after != before+1 {
		t.Errorf("an unchanged source cost %d requests, want 1", after-before)
	}
	if got := configure.GetDnsRulesNotNil(); len(got) != len(stored) {
		t.Errorf("sync changed the rules while the version stood")
	}
}

func TestClearDnsSourceKeepsRules(t *testing.T) {
	useDnsTestDB(t)
	srv, _ := dnsServer(t, `{"version":"v1","rules":[{"upstream":"1.1.1.1","outbound":"direct"}]}`)
	if _, err := ImportDnsSource(srv.URL); err != nil {
		t.Fatal(err)
	}
	if err := ClearDnsSource(); err != nil {
		t.Fatal(err)
	}
	if got := GetDnsSource(); got.URL != "" {
		t.Errorf("source not forgotten: %+v", got)
	}
	if len(configure.GetDnsRulesNotNil()) == 0 {
		t.Error("clearing the source also removed the rules")
	}
}
