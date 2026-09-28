package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/v2rayA/RoutingA"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/common/httpClient"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/kernel/v2ray"
	"github.com/v2rayA/v2rayA/kernel/v2ray/asset/dat"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

// A rule source is a URL holding an index of named rule sets in the v2rayN
// format. It is the same idea v2rayN calls a routing rules template source, and
// it exists because the sources a user can reach are not the ones this project
// was written against: the ordinary geo hosts are exactly the ones a restrictive
// network blocks, while the rules themselves are small JSON documents that the
// running core can fetch through the proxy.

// maxRuleSetDocumentSize bounds a rule document. The published sets are a few
// kilobytes; a megabyte is already far past anything legitimate.
const maxRuleSetDocumentSize int64 = 1 << 20

// RoutingSource is an alias so callers in this package do not have to reach
// into configure for a type they only pass back and forth.
type RoutingSource = configure.RoutingSource

func GetRoutingSource() RoutingSource {
	var r RoutingSource
	configure.GetRoutingSource(&r)
	return r
}

func SetRoutingSource(r *RoutingSource) error {
	return configure.SetRoutingSource(r)
}

// ClearRoutingSource forgets the configured source. The imported rules stay in
// place: dropping them would leave a user who imported a set with no rules at
// all, which is worse than keeping rules whose source is no longer tracked.
func ClearRoutingSource() error {
	return configure.SetRoutingSource(&RoutingSource{})
}

func readRuleSetBody(resp *http.Response) ([]byte, error) {
	if resp.ContentLength > maxRuleSetDocumentSize {
		return nil, fmt.Errorf("the rule document exceeds the 1 MiB limit")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxRuleSetDocumentSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxRuleSetDocumentSize {
		return nil, fmt.Errorf("the rule document exceeds the 1 MiB limit")
	}
	return b, nil
}

// fetchRuleDocument downloads through the proxy the core is already running, the
// same way a subscription is fetched, so a source the network blocks directly is
// still reachable.
func fetchRuleDocument(url string) ([]byte, error) {
	c := *httpClient.GetHttpClientAutomatically()
	if c.Timeout == 0 {
		c.Timeout = 90 * time.Second
	}
	resp, err := c.Get(url)
	if err != nil {
		return nil, common.Coded("ROUTING_SOURCE_UNREACHABLE", err, map[string]interface{}{
			"url":    url,
			"detail": err.Error(),
		})
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, common.Coded("ROUTING_SOURCE_FAILED", fmt.Errorf("the rule source answered HTTP %s", resp.Status), map[string]interface{}{
			"url":    url,
			"status": resp.Status,
		})
	}
	return readRuleSetBody(resp)
}

// FetchRoutingTemplate reads the index at url without changing anything, so a
// page can offer the sets behind a source before applying one.
func FetchRoutingTemplate(url string) (*RoutingTemplate, error) {
	if strings.TrimSpace(url) == "" {
		return nil, fmt.Errorf("no rule source is configured")
	}
	b, err := fetchRuleDocument(url)
	if err != nil {
		return nil, err
	}
	var t RoutingTemplate
	if err := jsoniter.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("the rule source is not a v2rayN routing template: %w", err)
	}
	if len(t.RoutingItems) == 0 {
		return nil, fmt.Errorf("the rule source lists no rule set")
	}
	return &t, nil
}

// ErrRoutingSourceUnchanged reports that the source's rules are already the ones
// in use. The caller presents that as a result, not a failure.
var ErrRoutingSourceUnchanged = errors.New("the rule source is unchanged")

// ImportRoutingSource downloads the chosen set from url and stores it as the
// RoutingA source. An empty remarks takes the first set the index lists.
//
// The routing source replaces the RoutingA text, which is one document per
// instance: v2rayN keeps several rule sets and lets the user pick between them,
// v2rayA has one slot, so switching means pointing it at another source.
func ImportRoutingSource(url, remarks string) (RoutingSource, error) {
	var out RoutingSource
	template, err := FetchRoutingTemplate(url)
	if err != nil {
		return out, err
	}
	item, err := pickRoutingItem(template, remarks)
	if err != nil {
		return out, err
	}
	out = RoutingSource{
		URL:            url,
		Version:        template.Version,
		Remarks:        item.Remarks,
		DomainStrategy: item.DomainStrategy,
	}

	previous := GetRoutingSource()
	if previous.URL == url && previous.Version == template.Version && previous.Remarks == item.Remarks &&
		configure.GetRoutingA() == readStoredRoutingA() {
		return out, ErrRoutingSourceUnchanged
	}

	raw := []byte(item.RuleSet)
	if len(raw) == 0 {
		if strings.TrimSpace(item.URL) == "" {
			return out, fmt.Errorf("the rule set %q carries neither rules nor a URL", item.Remarks)
		}
		if raw, err = fetchRuleDocument(item.URL); err != nil {
			return out, err
		}
	}
	name := template.Version + "-" + item.Remarks
	source, err := ConvertV2rayNRouting(name, raw)
	if err != nil {
		return out, common.Coded("ROUTING_RULES_INCOMPATIBLE", err, map[string]interface{}{
			"set":    item.Remarks,
			"detail": err.Error(),
		})
	}
	// Name what the core would reject before storing: a restart with an
	// unparsable set or an unknown geo tag fails and rolls the import back,
	// and the core's log line is all the user would get. PutRoutingA does
	// the same for hand-written rules.
	if _, err := RoutingA.Parse(source); err != nil {
		return out, common.Coded("ROUTING_RULES_INCOMPATIBLE", fmt.Errorf("the rule set %q does not parse: %w", item.Remarks, err), map[string]interface{}{
			"set":    item.Remarks,
			"detail": err.Error(),
		})
	}
	if missing := dat.Missing(dat.RoutingATags(source)); len(missing) > 0 {
		names := make([]string, len(missing))
		for i, r := range missing {
			names[i] = r.String()
		}
		return out, common.Coded("UNKNOWN_GEO_TAGS", fmt.Errorf("the rule set %q needs geo tags this machine does not have: %v; install the geo files that hold them or pick another set", item.Remarks, strings.Join(names, ", ")), map[string]interface{}{
			"tags": names,
		})
	}
	if err := configure.SetRoutingA(&source); err != nil {
		return out, err
	}
	if err := SetRoutingSource(&out); err != nil {
		return out, err
	}
	log.Info("routing rules imported from %v: %v (%d bytes)", url, item.Remarks, len(source))
	return out, nil
}

// readStoredRoutingA returns the RoutingA text as stored, without the built-in
// template standing in for an empty slot, so "unchanged" compares what the user
// actually has against what the source would give.
func readStoredRoutingA() string {
	var r string
	configure.GetRoutingARaw(&r)
	return r
}

func pickRoutingItem(t *RoutingTemplate, remarks string) (*struct {
	Remarks        string `json:"remarks"`
	URL            string `json:"url"`
	RuleSet        string `json:"ruleSet"`
	DomainStrategy string `json:"domainStrategy"`
}, error) {
	if remarks == "" {
		return &t.RoutingItems[0], nil
	}
	for i := range t.RoutingItems {
		if t.RoutingItems[i].Remarks == remarks {
			return &t.RoutingItems[i], nil
		}
	}
	return nil, fmt.Errorf("the rule source has no set named %q; it offers %s", remarks, strings.Join(routingItemNames(t), ", "))
}

func routingItemNames(t *RoutingTemplate) []string {
	names := make([]string, 0, len(t.RoutingItems))
	for i := range t.RoutingItems {
		names = append(names, t.RoutingItems[i].Remarks)
	}
	return names
}

// SyncRoutingSource re-imports the configured source when its version moved. It
// runs on the update tick next to the geo files, and never touches rules the
// user has edited away from the source: the comparison is against the stored
// text, so a local edit makes the import a no-op rather than a silent overwrite.
func SyncRoutingSource() error {
	src := GetRoutingSource()
	if strings.TrimSpace(src.URL) == "" {
		return nil
	}
	template, err := FetchRoutingTemplate(src.URL)
	if err != nil {
		return err
	}
	if template.Version == src.Version {
		return nil
	}
	_, err = ImportRoutingSource(src.URL, src.Remarks)
	if err != nil && !errors.Is(err, ErrRoutingSourceUnchanged) {
		return err
	}
	return v2ray.UpdateV2RayConfig()
}
