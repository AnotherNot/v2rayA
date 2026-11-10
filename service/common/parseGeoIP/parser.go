package parseGeoIP

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/v2rayA/v2rayA/kernel/v2ray/asset"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

// ErrCountryNotFound reports a code that the asset does not carry. It is
// distinct from a country whose list is empty, which is a valid answer.
var ErrCountryNotFound = errors.New("country code not found in the geoip asset")

type parserResult struct {
	ipv4 []string
	ipv6 []string
}

type parserCacheKey struct {
	path        string
	size        int64
	modTime     int64
	countryCode string
}

type codesCacheKey struct {
	path    string
	size    int64
	modTime int64
}

// Both caches are bounded: a caller can ask for arbitrary two-letter codes,
// and an unbounded map of decoded CIDR lists is a memory leak with a remote
// trigger. The asset is one file per path/size/mtime, so a changed file is a
// new key and the old entries age out.
var (
	parserCache, _       = lru.New[parserCacheKey, parserResult](128)
	countryCodesCache, _ = lru.New[codesCacheKey, []string](8)
)

func Parser(filename string, countryCode string) ([]string, []string, error) {
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode == "" {
		return nil, nil, ErrCountryNotFound
	}
	realpath, info, err := assetInfo(filename)
	if err != nil {
		return nil, nil, err
	}

	cacheKey := parserCacheKey{
		path:        realpath,
		size:        info.Size(),
		modTime:     info.ModTime().UnixNano(),
		countryCode: countryCode,
	}
	if cached, ok := parserCache.Get(cacheKey); ok {
		return cached.ipv4, cached.ipv6, nil
	}

	data, err := os.ReadFile(realpath)
	if err != nil {
		return nil, nil, err
	}

	ipv4List, ipv6List, found, err := parseGeoIP(data, countryCode)
	if err != nil {
		return ipv4List, ipv6List, err
	}
	if !found {
		return nil, nil, ErrCountryNotFound
	}

	parserCache.Add(cacheKey, parserResult{ipv4: ipv4List, ipv6: ipv6List})
	return ipv4List, ipv6List, nil
}

// CountryCodes lists the codes the asset carries. The controller checks a
// submitted code against this set before storing it, so an unknown code never
// reaches the per-code parser above.
func CountryCodes(filename string) ([]string, error) {
	realpath, info, err := assetInfo(filename)
	if err != nil {
		return nil, err
	}
	key := codesCacheKey{path: realpath, size: info.Size(), modTime: info.ModTime().UnixNano()}
	if cached, ok := countryCodesCache.Get(key); ok {
		return cached, nil
	}
	data, err := os.ReadFile(realpath)
	if err != nil {
		return nil, err
	}
	codes, err := countryCodesFromData(data)
	if err != nil {
		return nil, err
	}
	countryCodesCache.Add(key, codes)
	return codes, nil
}

func assetInfo(filename string) (string, os.FileInfo, error) {
	realpath, err := asset.GetV2rayLocationAsset(filename)
	if err != nil {
		return "", nil, err
	}
	info, err := os.Stat(realpath)
	if err != nil {
		return "", nil, err
	}
	return realpath, info, nil
}

// parseGeoIP decodes one entry at a time and stops at the requested country.
// Decoding the complete GeoIPList expands a roughly 22 MiB asset to hundreds
// of MiB in memory.
func parseGeoIP(data []byte, countryCode string) ([]string, []string, bool, error) {
	var ipv4List []string
	var ipv6List []string
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))

	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return ipv4List, ipv6List, false, protowire.ParseError(n)
		}
		data = data[n:]

		if number == 1 && wireType == protowire.BytesType {
			entryData, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return ipv4List, ipv6List, false, protowire.ParseError(consumed)
			}
			data = data[consumed:]

			var geo GeoIP
			if err := proto.Unmarshal(entryData, &geo); err != nil {
				return ipv4List, ipv6List, false, err
			}
			if strings.ToUpper(strings.TrimSpace(geo.CountryCode)) != countryCode {
				continue
			}

			for _, c := range geo.Cidr {
				ip := net.IP(c.Ip)
				if strings.Contains(ip.String(), ":") {
					ipv6List = append(ipv6List, fmt.Sprintf("%s/%d", ip.String(), c.Prefix))
				} else {
					if strings.Contains(ip.String(), "198.18.0.0") {
						// 跳过fakeip
						continue
					}
					ipv4List = append(ipv4List, fmt.Sprintf("%s/%d", ip.String(), c.Prefix))
				}
			}
			return ipv4List, ipv6List, true, nil
		}

		consumed := protowire.ConsumeFieldValue(number, wireType, data)
		if consumed < 0 {
			return ipv4List, ipv6List, false, protowire.ParseError(consumed)
		}
		data = data[consumed:]
	}

	return ipv4List, ipv6List, false, nil
}

func countryCodesFromData(data []byte) ([]string, error) {
	seen := make(map[string]struct{})
	var codes []string
	for len(data) > 0 {
		number, wireType, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		if number == 1 && wireType == protowire.BytesType {
			entryData, consumed := protowire.ConsumeBytes(data)
			if consumed < 0 {
				return nil, protowire.ParseError(consumed)
			}
			data = data[consumed:]
			var geo GeoIP
			if err := proto.Unmarshal(entryData, &geo); err != nil {
				return nil, err
			}
			code := strings.ToUpper(strings.TrimSpace(geo.CountryCode))
			if code != "" {
				if _, ok := seen[code]; !ok {
					seen[code] = struct{}{}
					codes = append(codes, code)
				}
			}
			continue
		}
		consumed := protowire.ConsumeFieldValue(number, wireType, data)
		if consumed < 0 {
			return nil, protowire.ParseError(consumed)
		}
		data = data[consumed:]
	}
	sort.Strings(codes)
	return codes, nil
}
