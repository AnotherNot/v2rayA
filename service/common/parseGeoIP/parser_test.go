package parseGeoIP

import (
	"net"
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestParseGeoIPStopsAtRequestedEntry(t *testing.T) {
	data, err := proto.Marshal(&GeoIPList{Entry: []*GeoIP{
		{
			CountryCode: "PRIVATE",
			Cidr: []*CIDR{
				{Ip: net.ParseIP("10.0.0.0").To4(), Prefix: 8},
				{Ip: net.ParseIP("198.18.0.0").To4(), Prefix: 15},
				{Ip: net.ParseIP("2001:db8::"), Prefix: 32},
			},
		},
		{
			CountryCode: "US",
			Cidr: []*CIDR{
				{Ip: net.ParseIP("192.0.2.0").To4(), Prefix: 24},
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	// A malformed record after the requested entry must not be decoded.
	data = append(data, 0x0a, 0x02, 0xff)

	ipv4, ipv6, found, err := parseGeoIP(data, "PRIVATE")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("the requested country was not reported as found")
	}
	if want := []string{"10.0.0.0/8"}; !reflect.DeepEqual(ipv4, want) {
		t.Fatalf("IPv4 entries = %v, want %v", ipv4, want)
	}
	if want := []string{"2001:db8::/32"}; !reflect.DeepEqual(ipv6, want) {
		t.Fatalf("IPv6 entries = %v, want %v", ipv6, want)
	}
}

func TestParseGeoIPRejectsMalformedData(t *testing.T) {
	if _, _, _, err := parseGeoIP([]byte{0x0a, 0x02, 0xff}, "PRIVATE"); err == nil {
		t.Fatal("parseGeoIP() accepted malformed data")
	}
}

func TestParseGeoIPReportsAMissingCountry(t *testing.T) {
	data, err := proto.Marshal(&GeoIPList{Entry: []*GeoIP{
		{CountryCode: "US", Cidr: []*CIDR{{Ip: net.ParseIP("192.0.2.0").To4(), Prefix: 24}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, found, err := parseGeoIP(data, "DE")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("a missing country was reported as found")
	}
}

func TestCountryCodesFromData(t *testing.T) {
	data, err := proto.Marshal(&GeoIPList{Entry: []*GeoIP{
		{CountryCode: "us", Cidr: []*CIDR{{Ip: net.ParseIP("192.0.2.0").To4(), Prefix: 24}}},
		{CountryCode: "DE", Cidr: []*CIDR{{Ip: net.ParseIP("198.51.100.0").To4(), Prefix: 24}}},
		{CountryCode: "US", Cidr: []*CIDR{{Ip: net.ParseIP("203.0.113.0").To4(), Prefix: 24}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	codes, err := countryCodesFromData(data)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"DE", "US"}; !reflect.DeepEqual(codes, want) {
		t.Fatalf("codes = %v, want %v", codes, want)
	}
}

func TestParserCacheIsBounded(t *testing.T) {
	for i := 0; i < 200; i++ {
		parserCache.Add(parserCacheKey{path: "test", countryCode: string(rune(i))}, parserResult{})
	}
	if parserCache.Len() > 128 {
		t.Fatalf("parser cache holds %d entries, want at most 128", parserCache.Len())
	}
}
