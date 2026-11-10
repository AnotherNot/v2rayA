package controller

import (
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/v2rayA/v2rayA/common/parseGeoIP"
	"google.golang.org/protobuf/proto"
)

func writeGeoIPAsset(t *testing.T, codes ...string) {
	t.Helper()
	entries := make([]*parseGeoIP.GeoIP, 0, len(codes))
	for _, code := range codes {
		entries = append(entries, &parseGeoIP.GeoIP{
			CountryCode: code,
			Cidr:        []*parseGeoIP.CIDR{{Ip: net.ParseIP("192.0.2.0").To4(), Prefix: 24}},
		})
	}
	data, err := proto.Marshal(&parseGeoIP.GeoIPList{Entry: entries})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "geoip.dat"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XRAY_LOCATION_ASSET", dir)
}

func TestNormalizeCountryCodes(t *testing.T) {
	writeGeoIPAsset(t, "DE", "US")
	codes, err := normalizeCountryCodes([]string{" de ", "US", "de", ""})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"DE", "US"}; !reflect.DeepEqual(codes, want) {
		t.Fatalf("codes = %v, want %v", codes, want)
	}
}

func TestNormalizeCountryCodesRejectsBadValues(t *testing.T) {
	writeGeoIPAsset(t, "DE")
	for _, value := range []string{"USA", "D1", "D", "🇩🇪"} {
		if _, err := normalizeCountryCodes([]string{value}); err == nil {
			t.Errorf("%q was accepted", value)
		}
	}
	if _, err := normalizeCountryCodes([]string{"ZZ"}); err == nil {
		t.Error("a code missing from geoip.dat was accepted")
	}
}

func TestNormalizeCountryCodesBoundsTheList(t *testing.T) {
	ensure := make([]string, 0, maxCountryCodes+1)
	codes := make([]string, 0, maxCountryCodes+1)
	for i := 0; i <= maxCountryCodes; i++ {
		code := string(rune('A'+i/26)) + string(rune('A'+i%26))
		ensure = append(ensure, code)
		codes = append(codes, code)
	}
	writeGeoIPAsset(t, ensure...)
	if _, err := normalizeCountryCodes(codes); err == nil {
		t.Fatalf("%d codes were accepted, want at most %d", len(codes), maxCountryCodes)
	}
}

func TestNormalizeCustomIPs(t *testing.T) {
	ips, err := normalizeCustomIPs([]string{" 192.0.2.1 ", "192.0.2.1", "2001:db8::/32", ""})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"192.0.2.1", "2001:db8::/32"}; !reflect.DeepEqual(ips, want) {
		t.Fatalf("ips = %v, want %v", ips, want)
	}
	if _, err := normalizeCustomIPs([]string{"192.0.2.1/33"}); err == nil {
		t.Fatal("an invalid CIDR was accepted")
	}
	if _, err := normalizeCustomIPs([]string{"not-an-ip"}); err == nil {
		t.Fatal("a hostname was accepted")
	}
}
