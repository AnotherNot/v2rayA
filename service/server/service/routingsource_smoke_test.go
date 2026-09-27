package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/v2rayA/RoutingA"
	"github.com/v2rayA/v2rayA/conf"
	"github.com/v2rayA/v2rayA/db/configure"
)

// The whole point of the source is that a user can point v2rayA at a rule set
// published somewhere and have working rules. These walk that path end to end
// against the real published index and sets, served locally so the test does not
// depend on the network, and check the result reaches the place the core reads.

func serveTestdata(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	dir := "testdata"
	var mu sync.Mutex
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		name := filepath.Base(r.URL.Path)
		if name == "" || name == "/" {
			name = "template.json"
		}
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

func useTestDB(t *testing.T) {
	t.Helper()
	previous := *conf.GetEnvironmentConfig()
	c := previous
	c.Config = t.TempDir()
	conf.SetConfig(c)
	t.Cleanup(func() {
		conf.SetConfig(previous)
		configure.SetRoutingSource(&configure.RoutingSource{})
		var empty string
		_ = configure.SetRoutingA(&empty)
	})
}

// The published Russian index is the reference case: a version, three named
// sets, two of them downloaded by URL. Importing the default set must store
// rules the core can parse, and must record enough to tell a later run that
// nothing changed.
func TestImportRoutingSourceEndToEnd(t *testing.T) {
	useTestDB(t)
	srv, _ := serveTestdata(t)

	// the index points at absolute URLs on the real host; redirect them at the
	// test server so the download path is exercised too
	var template RoutingTemplate
	b, err := os.ReadFile(filepath.Join("testdata", "template.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &template); err != nil {
		t.Fatal(err)
	}
	if len(template.RoutingItems) == 0 {
		t.Fatal("the published index lists no rule set")
	}
	idx := rewriteIndex(t, b, srv.URL)

	src, err := ImportRoutingSource(idx.URL, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if src.Version != template.Version {
		t.Errorf("version %q, want %q", src.Version, template.Version)
	}
	if src.Remarks != template.RoutingItems[0].Remarks {
		t.Errorf("remarks %q, want the first set %q", src.Remarks, template.RoutingItems[0].Remarks)
	}
	if src.DomainStrategy == "" && template.RoutingItems[0].DomainStrategy != "" {
		t.Errorf("domain strategy of the chosen set was dropped")
	}

	stored := configure.GetRoutingA()
	parsed, err := RoutingA.Parse(stored)
	if err != nil {
		t.Fatalf("the stored rules do not parse: %v\n%s", err, stored)
	}
	count := 0
	for _, r := range parsed {
		if _, ok := r.(RoutingA.Routing); ok {
			count++
		}
	}
	if count == 0 {
		t.Fatalf("no routing rules were stored:\n%s", stored)
	}
	if !strings.Contains(stored, "default: proxy") {
		t.Errorf("stored rules have no default line:\n%s", stored)
	}

	// a second import of the same version is a no-op, not a rewrite
	if _, err := ImportRoutingSource(idx.URL, ""); !errors.Is(err, ErrRoutingSourceUnchanged) {
		t.Errorf("re-importing an unchanged source returned %v, want ErrRoutingSourceUnchanged", err)
	}
}

// A named set has to be selectable, and a name the source does not offer has to
// be refused with the list of what it does offer.
func TestImportRoutingSourceSelectsByName(t *testing.T) {
	useTestDB(t)
	srv, _ := serveTestdata(t)

	var template RoutingTemplate
	b, _ := os.ReadFile(filepath.Join("testdata", "template.json"))
	_ = json.Unmarshal(b, &template)
	if len(template.RoutingItems) < 2 {
		t.Skip("the index offers fewer than two sets")
	}
	want := template.RoutingItems[1].Remarks
	// serve the selected set's URL from the same server
	idx := rewriteIndex(t, b, srv.URL)

	src, err := ImportRoutingSource(idx.URL, want)
	if err != nil {
		t.Fatalf("import %q: %v", want, err)
	}
	if src.Remarks != want {
		t.Errorf("remarks %q, want %q", src.Remarks, want)
	}
	_, err = ImportRoutingSource(idx.URL, "no such set")
	if err == nil {
		t.Fatal("an unknown set name was accepted")
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("the error does not list what the source offers: %v", err)
	}
}

// The source is the only path a user behind a restrictive network has, so the
// failure modes have to say what went wrong rather than leave a half-imported
// state behind.
func TestImportRoutingSourceFailures(t *testing.T) {
	useTestDB(t)

	if _, err := ImportRoutingSource("", ""); err == nil {
		t.Error("an empty URL was accepted")
	}
	if _, err := ImportRoutingSource("http://127.0.0.1:1/none", ""); err == nil {
		t.Error("an unreachable source was accepted")
	}

	notATemplate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"nope": true}`))
	}))
	t.Cleanup(notATemplate.Close)
	if _, err := ImportRoutingSource(notATemplate.URL, ""); err == nil {
		t.Error("a document that is not a routing template was accepted")
	}

	// a template whose only set points at a rule set RoutingA cannot express
	// must not replace the stored rules
	var before string
	configure.GetRoutingARaw(&before)
	incompatible := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"X","routingItems":[{"remarks":"p","ruleSet":"[{\"outboundTag\":\"direct\",\"process\":[\"x.exe\"]}]"}]}`))
	}))
	t.Cleanup(incompatible.Close)
	if _, err := ImportRoutingSource(incompatible.URL, ""); err == nil {
		t.Error("a process-name rule set was accepted")
	}
	var after string
	configure.GetRoutingARaw(&after)
	if before != after {
		t.Errorf("a rejected import changed the stored rules:\nbefore %q\nafter  %q", before, after)
	}
}

// Sync is what the daily tick calls. It must re-import when the source's
// version moved and leave the rules alone when it did not.
func TestSyncRoutingSourceFollowsTheSourceVersion(t *testing.T) {
	useTestDB(t)
	srv, setHits := serveTestdata(t)
	b, err := os.ReadFile(filepath.Join("testdata", "template.json"))
	if err != nil {
		t.Fatal(err)
	}
	var template RoutingTemplate
	if err := json.Unmarshal(b, &template); err != nil {
		t.Fatal(err)
	}
	version := template.Version
	index, hits := countingIndex(t, b, srv.URL)

	// a stored version behind the source's makes sync import
	if err := configure.SetRoutingSource(&configure.RoutingSource{URL: index.URL, Version: "older"}); err != nil {
		t.Fatal(err)
	}
	var empty string
	_ = configure.SetRoutingA(&empty)
	if err := SyncRoutingSource(); err != nil {
		t.Fatalf("sync after a version change: %v", err)
	}
	imported := configure.GetRoutingA()
	if !strings.Contains(imported, "default: proxy") {
		t.Fatalf("sync did not import the newer version:\n%s", imported)
	}
	if src := GetRoutingSource(); src.Version != version {
		t.Errorf("stored version %q, want the source's %q", src.Version, version)
	}

	// The version now matches. Sync still has to ask the source once to learn
	// that, so the index is fetched exactly one more time and the rule set is
	// not fetched at all: the version check is what saves the second round trip
	// and the download, and the tick runs daily.
	before := hits()
	setsBefore := setHits()
	if err := SyncRoutingSource(); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if after := hits(); after != before+1 {
		t.Errorf("an unchanged source cost %d index requests, want 1", after-before)
	}
	if setHits() != setsBefore {
		t.Errorf("an unchanged source was downloaded again")
	}
	if again := configure.GetRoutingA(); again != imported {
		t.Errorf("sync rewrote rules while the version was unchanged:\n%q\n%q", imported, again)
	}
}

// rewriteIndex serves the published index with its set URLs pointed at the test
// server, so the download of a rule set is exercised and not stubbed out.
func rewriteIndex(t *testing.T, index []byte, base string) *httptest.Server {
	t.Helper()
	srv, _ := countingIndex(t, index, base)
	return srv
}

// countingIndex is rewriteIndex plus a counter of requests, so a test can tell
// that a cached path did not reach the network.
func countingIndex(t *testing.T, index []byte, base string) (*httptest.Server, func() int) {
	t.Helper()
	rewritten := strings.ReplaceAll(string(index),
		"https://raw.githubusercontent.com/runetfreedom/russia-v2ray-custom-routing-list/refs/heads/main/v2rayN/", base+"/")
	var mu sync.Mutex
	count := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		_, _ = w.Write([]byte(rewritten))
	}))
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}
