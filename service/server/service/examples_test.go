package service

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/v2rayA/RoutingA"
)

// The example documents under docs/examples/rule-sources are what a user
// hosts and pastes into the Rule source fields. They must stay importable:
// this serves them over HTTP the way a raw repository file would be served.
func serveExample(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "examples", "rule-sources", name))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestExampleRoutingTemplateImports(t *testing.T) {
	useTestDB(t)
	index := serveExample(t, "routing-template.json")
	src, err := ImportRoutingSource(index, "RU direct")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if src.Remarks != "RU direct" {
		t.Errorf("remarks %q, want RU direct", src.Remarks)
	}
	if _, err := RoutingA.Parse(readStoredRoutingA()); err != nil {
		t.Fatalf("imported rules do not parse: %v", err)
	}
	if _, err := ImportRoutingSource(index, "no such set"); err == nil {
		t.Fatal("an unknown set name was accepted")
	} else if !strings.Contains(err.Error(), "RU direct") {
		t.Errorf("the error does not list the offered sets: %v", err)
	}
}

func TestExampleDnsRulesImport(t *testing.T) {
	useTestDB(t)
	url := serveExample(t, "dns-rules.json")
	src, err := ImportDnsSource(url)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if src.Version == "" {
		t.Error("no version was recorded")
	}
}
