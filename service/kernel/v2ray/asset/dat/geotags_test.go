package dat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"
)

func writeFixture(t *testing.T, dir string) {
	t.Helper()
	sites := &routercommon.GeoSiteList{Entry: []*routercommon.GeoSite{
		{CountryCode: "PRIVATE", Domain: []*routercommon.Domain{{Type: routercommon.Domain_Plain, Value: "example"}}},
		{CountryCode: "RU-BLOCKED", Domain: []*routercommon.Domain{{Type: routercommon.Domain_Plain, Value: "example"}}},
	}}
	b, err := proto.Marshal(sites)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), b, 0644); err != nil {
		t.Fatal(err)
	}
	ips := &routercommon.GeoIPList{Entry: []*routercommon.GeoIP{
		{CountryCode: "PRIVATE"},
		{CountryCode: "RU"},
	}}
	b, err = proto.Marshal(ips)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geoip.dat"), b, 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XRAY_LOCATION_ASSET", dir)
}

func TestRoutingATagsExtracts(t *testing.T) {
	refs := RoutingATags(`# a comment naming geosite:commented
default: proxy
domain(geosite:ru-blocked)->proxy
domain(geosite:apple@cn)->direct
ip(geoip:private, geoip:ru)->direct
domain(ext:"LoyalsoldierSite.dat:gfw")->proxy
`)
	var got []string
	for _, r := range refs {
		got = append(got, r.String())
	}
	want := []string{
		`ext:"LoyalsoldierSite.dat:gfw"`,
		"geoip:private",
		"geoip:ru",
		"geosite:apple",
		"geosite:ru-blocked",
	}
	if len(got) != len(want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("refs = %v, want %v", got, want)
		}
	}
}

func TestMissingNamesAbsentTags(t *testing.T) {
	writeFixture(t, t.TempDir())
	missing, err := Missing([]TagRef{
		{File: "geosite.dat", Tag: "ru-blocked"},
		{File: "geosite.dat", Tag: "ru"},         // absent, like the user's rule
		{File: "geosite.dat", Tag: "Ru-Blocked"}, // case does not matter
		{File: "geoip.dat", Tag: "ru"},
		{File: "geoip.dat", Tag: "cn"}, // absent from this fixture
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range missing {
		got = append(got, r.String())
	}
	want := []string{"geoip:cn", "geosite:ru"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("missing = %v, want %v", got, want)
	}
}

func TestMissingSkipsUnreadableFiles(t *testing.T) {
	writeFixture(t, t.TempDir())
	missing, err := Missing([]TagRef{{File: "nosuch.dat", Tag: "whatever"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none: an unreadable file is the core's call", missing)
	}
}

func TestMissingSkipsUnparseableFiles(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), []byte("not protobuf"), 0644); err != nil {
		t.Fatal(err)
	}
	missing, err := Missing([]TagRef{{File: "geosite.dat", Tag: "ru"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
}

// An ext tag is a file name the core opens inside the asset directory. A rule
// that points outside it must fail the rule, not resolve the file: the asset
// lookup removes and recreates a link at that path.
func TestMissingRejectsFileOutsideTheAssetDirectory(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir)
	victim := filepath.Join(filepath.Dir(dir), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	refs := RoutingATags(`domain(ext:"../victim:x")->proxy`)
	if len(refs) != 1 {
		t.Fatalf("refs = %v, want the ext tag", refs)
	}
	if _, err := Missing(refs); err == nil {
		t.Fatal("an ext file outside the asset directory was accepted")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("a file outside the asset directory was touched: %v", err)
	}
}
