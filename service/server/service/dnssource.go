package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/v2ray"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

// A DNS source is a URL holding DNS rules in the shape this project stores
// them. The accepted document is either
//
//	{ "version": "…", "remarks": "…", "rules": [ … ] }
//
// or a bare rule array, in which case the version is taken from a hash of the
// content so an unchanged source is still recognised as unchanged.

// dnsSourceDocument is the versioned form.
type dnsSourceDocument struct {
	Version string              `json:"version"`
	Remarks string              `json:"remarks"`
	Rules   []configure.DnsRule `json:"rules"`
}

type DnsSource = configure.DnsSource

func GetDnsSource() DnsSource {
	var s DnsSource
	configure.GetDnsSource(&s)
	return s
}

func SetDnsSource(s *DnsSource) error {
	return configure.SetDnsSource(s)
}

// ClearDnsSource forgets the source. The imported rules stay, so that a user
// who applied a set is not left with none.
func ClearDnsSource() error {
	return configure.SetDnsSource(&DnsSource{})
}

// ErrDnsSourceUnchanged reports that the source's rules are already the ones in
// use. The caller presents that as a result, not a failure.
var ErrDnsSourceUnchanged = errors.New("the DNS source is unchanged")

// validateDnsRules rejects a set that would leave the core without an answer.
// A rule with no upstream is dropped by the generator, which turns a typo in a
// published document into silently unanswered queries.
func validateDnsRules(rules []configure.DnsRule) error {
	if len(rules) == 0 {
		return fmt.Errorf("the source holds no DNS rule")
	}
	for i, r := range rules {
		upstream := r.Upstream
		if upstream == "" {
			upstream = r.Server
		}
		if strings.TrimSpace(upstream) == "" {
			return fmt.Errorf("DNS rule %d names no upstream", i+1)
		}
		// The outbound is a tag the core resolves: an empty one means direct,
		// "block" is the blackhole, and any other name is a group the core
		// reaches through the local SOCKS inbound. There is nothing to reject.
		switch r.Action {
		case "", "route", "reject", "bypass":
		default:
			return fmt.Errorf("DNS rule %d has the unknown action %q; this project uses route, reject or bypass", i+1, r.Action)
		}
	}
	return nil
}

// parseDnsSource accepts both document shapes and reports the version to store.
func parseDnsSource(data []byte) (rules []configure.DnsRule, version, remarks string, err error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, "", "", fmt.Errorf("the DNS source is empty")
	}
	if strings.HasPrefix(trimmed, "[") {
		if err := jsoniter.Unmarshal(data, &rules); err != nil {
			return nil, "", "", fmt.Errorf("the DNS source is not a rule array: %w", err)
		}
		sum := sha256.Sum256(data)
		return rules, "sha256:" + hex.EncodeToString(sum[:8]), "", nil
	}
	var doc dnsSourceDocument
	if err := jsoniter.Unmarshal(data, &doc); err != nil {
		return nil, "", "", fmt.Errorf("the DNS source is neither a rule array nor a versioned document: %w", err)
	}
	version = strings.TrimSpace(doc.Version)
	if version == "" {
		sum := sha256.Sum256(data)
		version = "sha256:" + hex.EncodeToString(sum[:8])
	}
	return doc.Rules, version, doc.Remarks, nil
}

// ImportDnsSource downloads the rules at url and stores them. The download goes
// through the proxy the core is already running, the same way a rule source is
// fetched, so a host that cannot reach the source directly still can.
func ImportDnsSource(url string) (DnsSource, error) {
	var out DnsSource
	if strings.TrimSpace(url) == "" {
		return out, fmt.Errorf("no DNS source is configured")
	}
	b, err := fetchRuleDocument(url)
	if err != nil {
		return out, err
	}
	rules, version, remarks, err := parseDnsSource(b)
	if err != nil {
		return out, err
	}
	if err := validateDnsRules(rules); err != nil {
		return out, common.Coded("DNS_RULES_INCOMPATIBLE", err, map[string]interface{}{
			"url":    url,
			"detail": err.Error(),
		})
	}
	out = DnsSource{URL: url, Version: version, Remarks: remarks}

	previous := GetDnsSource()
	if previous.URL == url && previous.Version == version && dnsRulesEqual(configure.GetDnsRulesNotNil(), rules) {
		return out, ErrDnsSourceUnchanged
	}
	if err := configure.SetDnsRules(rules); err != nil {
		return out, err
	}
	if err := SetDnsSource(&out); err != nil {
		return out, err
	}
	log.Info("DNS rules imported from %v: %d rules", url, len(rules))
	return out, nil
}

// dnsRulesEqual compares through the migration the generator applies, so a
// stored rule that only differs by a defaulted field is not treated as a change.
func dnsRulesEqual(stored, incoming []configure.DnsRule) bool {
	a := configure.MigrateDnsRules(stored)
	b := configure.MigrateDnsRules(incoming)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Upstream != b[i].Upstream || a[i].Domain != b[i].Domain ||
			a[i].Outbound != b[i].Outbound || a[i].Action != b[i].Action {
			return false
		}
	}
	return true
}

// SyncDnsSource re-reads the configured source when its version moved. It never
// overwrites rules the user edited away from the source, because the comparison
// is against what is stored.
func SyncDnsSource() error {
	src := GetDnsSource()
	if strings.TrimSpace(src.URL) == "" {
		return nil
	}
	b, err := fetchRuleDocument(src.URL)
	if err != nil {
		return err
	}
	_, version, _, err := parseDnsSource(b)
	if err != nil {
		return err
	}
	if version == src.Version {
		return nil
	}
	_, err = ImportDnsSource(src.URL)
	if err != nil && !errors.Is(err, ErrDnsSourceUnchanged) {
		return err
	}
	return v2ray.UpdateV2RayConfig()
}
