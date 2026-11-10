package controller

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/common/parseGeoIP"
	"github.com/v2rayA/v2rayA/db/configure"
)

const (
	maxCountryCodes = 50
	maxCustomIPs    = 1000
)

var countryCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)

func GetTproxyWhiteIpGroups(ctx *gin.Context) {
	resp := configure.GetTproxyWhiteIpGroups()
	common.ResponseSuccess(ctx, gin.H{
		"countryCodes": resp.CountryCodes,
		"customIps":    resp.CustomIps,
	})
}

func PutTproxyWhiteIpGroups(ctx *gin.Context) {
	release, ok := beginMutation(ctx)
	if !ok {
		return
	}
	defer release()
	var data struct {
		CountryCodes []string `json:"countryCodes"`
		CustomIps    []string `json:"customIps"`
	}
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		common.ResponseError(ctx, badRequest("countryCodes and customIps", "request body must be {\"countryCodes\": [string], \"customIps\": [string]}"))
		return
	}
	codes, err := normalizeCountryCodes(data.CountryCodes)
	if err != nil {
		common.ResponseError(ctx, common.Coded("INVALID_COUNTRY_CODE", err, nil))
		return
	}
	ips, err := normalizeCustomIPs(data.CustomIps)
	if err != nil {
		common.ResponseError(ctx, common.Coded("INVALID_WHITELIST_IP", err, nil))
		return
	}
	if err := configure.SetTproxyWhiteIpGroups(codes, ips); err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, nil)
}

func normalizeCountryCodes(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	codes := make([]string, 0, len(raw))
	for _, value := range raw {
		code := strings.ToUpper(strings.TrimSpace(value))
		if code == "" {
			continue
		}
		if !countryCodePattern.MatchString(code) {
			return nil, fmt.Errorf("country code %q must be two ASCII letters", value)
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		codes = append(codes, code)
	}
	if len(codes) > maxCountryCodes {
		return nil, fmt.Errorf("at most %d country codes are accepted", maxCountryCodes)
	}
	if len(codes) == 0 {
		return codes, nil
	}
	// A two-letter code is not enough: Parser reads and scans the whole 22 MiB
	// asset for each code it does not know. Check the set once, here.
	available, err := parseGeoIP.CountryCodes("geoip.dat")
	if err != nil {
		return nil, fmt.Errorf("cannot read the geoip asset: %w", err)
	}
	known := make(map[string]struct{}, len(available))
	for _, code := range available {
		known[code] = struct{}{}
	}
	for _, code := range codes {
		if _, ok := known[code]; !ok {
			return nil, fmt.Errorf("country code %q is not present in geoip.dat", code)
		}
	}
	return codes, nil
}

func normalizeCustomIPs(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	ips := make([]string, 0, len(raw))
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if net.ParseIP(value) == nil {
			if _, _, err := net.ParseCIDR(value); err != nil {
				return nil, fmt.Errorf("%q is not an IP address or CIDR", value)
			}
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		ips = append(ips, value)
	}
	if len(ips) > maxCustomIPs {
		return nil, fmt.Errorf("at most %d whitelist addresses are accepted", maxCustomIPs)
	}
	return ips, nil
}
