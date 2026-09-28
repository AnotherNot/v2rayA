package dat

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/v2fly/v2ray-core/v5/app/router/routercommon"
	"google.golang.org/protobuf/proto"

	"github.com/v2rayA/v2rayA/kernel/v2ray/asset"
)

// TagRef names one geo reference a rule can carry: the file that must hold
// it and the tag to look up, both as written.
type TagRef struct {
	File string
	Tag  string
}

func (r TagRef) String() string {
	switch r.File {
	case "geosite.dat":
		return "geosite:" + r.Tag
	case "geoip.dat":
		return "geoip:" + r.Tag
	default:
		return "ext:\"" + r.File + ":" + r.Tag + "\""
	}
}

var (
	siteTag   = regexp.MustCompile(`geosite:([A-Za-z0-9_.@-]+)`)
	ipTag     = regexp.MustCompile(`geoip:([A-Za-z0-9_-]+)`)
	extTag    = regexp.MustCompile(`ext:"([^":]+):([^"]+)"`)
	commentRe = regexp.MustCompile(`(?m)#.*$`)
)

// RoutingATags reads the geo references out of RoutingA text: plain
// geosite:/geoip: tags and ext:"file:tag" forms. Comments are stripped; an
// @attribute suffix narrows a category and is not part of the tag.
func RoutingATags(text string) []TagRef {
	text = commentRe.ReplaceAllString(text, "")
	seen := make(map[TagRef]bool)
	var out []TagRef
	add := func(r TagRef) {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, m := range siteTag.FindAllStringSubmatch(text, -1) {
		tag := m[1]
		if i := strings.Index(tag, "@"); i >= 0 {
			tag = tag[:i]
		}
		if tag != "" {
			add(TagRef{File: "geosite.dat", Tag: tag})
		}
	}
	for _, m := range ipTag.FindAllStringSubmatch(text, -1) {
		if m[1] != "" {
			add(TagRef{File: "geoip.dat", Tag: m[1]})
		}
	}
	for _, m := range extTag.FindAllStringSubmatch(text, -1) {
		if m[1] != "" && m[2] != "" {
			add(TagRef{File: m[1], Tag: m[2]})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Tag < out[j].Tag
	})
	return out
}

var tagCache sync.Map // asset path -> map[UPPER_TAG]bool

// Missing returns the refs whose tag is absent from its file, compared
// case-insensitively like the core does. A file that cannot be read is not
// reported: the core restart stays the judge for unreadable assets, this
// check only names tags a readable file does not hold.
func Missing(refs []TagRef) []TagRef {
	byFile := make(map[string][]TagRef)
	for _, r := range refs {
		byFile[r.File] = append(byFile[r.File], r)
	}
	var missing []TagRef
	for file, rs := range byFile {
		have, err := loadTags(file)
		if err != nil {
			continue
		}
		for _, r := range rs {
			if !have[strings.ToUpper(r.Tag)] {
				missing = append(missing, r)
			}
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].String() < missing[j].String() })
	return missing
}

func loadTags(file string) (map[string]bool, error) {
	path, err := asset.GetV2rayLocationAsset(file)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	key := path + "\x00" + info.ModTime().UTC().String() + "\x00" + fmt.Sprint(info.Size())
	if cached, ok := tagCache.Load(key); ok {
		return cached.(map[string]bool), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tags := make(map[string]bool)
	lower := strings.ToLower(file)
	switch {
	case strings.Contains(lower, "geosite") || (strings.HasSuffix(lower, ".dat") && !strings.Contains(lower, "geoip")):
		var list routercommon.GeoSiteList
		if err := proto.Unmarshal(data, &list); err != nil {
			return nil, err
		}
		for _, e := range list.Entry {
			if e.CountryCode != "" {
				tags[strings.ToUpper(e.CountryCode)] = true
			}
		}
	default:
		var list routercommon.GeoIPList
		if err := proto.Unmarshal(data, &list); err != nil {
			return nil, err
		}
		for _, e := range list.Entry {
			if e.CountryCode != "" {
				tags[strings.ToUpper(e.CountryCode)] = true
			}
		}
	}
	tagCache.Store(key, tags)
	return tags, nil
}
