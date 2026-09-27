package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/v2rayA/RoutingA"
)

// The published Russian rule sets are the reference for this converter: they
// are the ones a user behind a network that blocks the ordinary sources needs.
// Every rule in all three has to survive the conversion and still parse as
// RoutingA, or the import produces a source that silently routes nothing.
func TestConvertPublishedRussianRuleSets(t *testing.T) {
	cases := []struct {
		file  string
		name  string
		rules int
	}{
		{"all.json", "Всё", 4},
		{"all_except_ru.json", "Всё, кроме РФ", 6},
		{"only_blocked.json", "Заблокированное", 9},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", c.file))
			if err != nil {
				t.Fatal(err)
			}
			out, err := ConvertV2rayNRouting(c.name, data)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			parsed, err := RoutingA.Parse(out)
			if err != nil {
				t.Fatalf("the converted source does not parse: %v\n%s", err, out)
			}
			routings := 0
			for _, r := range parsed {
				if _, ok := r.(RoutingA.Routing); ok {
					routings++
				}
			}
			if routings != c.rules {
				t.Fatalf("parsed %d routing rules, want %d\n%s", routings, c.rules, out)
			}
			if !strings.Contains(out, "default: proxy") {
				t.Errorf("no default line, so unmatched traffic has no route:\n%s", out)
			}
		})
	}
}

// The interesting shapes are the ones the converter has to get right rather
// than merely accept: a rule that conditions on several things at once, a
// range in a port, and a rule that only says where to send everything.
func TestConvertRuleShapes(t *testing.T) {
	data := []byte(`[
	  {"outboundTag":"direct","protocol":["bittorrent"],"enabled":true,"remarks":"Торрент на прямую"},
	  {"port":"50000-65535","network":"udp","outboundTag":"proxy","enabled":true,"remarks":"Дискорд (войс) в прокси"},
	  {"outboundTag":"direct","ip":["geoip:ru"],"domain":["geosite:ru-available-only-inside"],
	   "enabled":true,"remarks":"Доступно только в России напрямую"},
	  {"outboundTag":"block","domain":["geosite:category-ads-all"],"enabled":true,"remarks":"Реклама"},
	  {"enabled":true,"remarks":"Рабочее правило","outboundTag":"proxy","domain":["example.org"]},
	  {"enabled":false,"remarks":"выключенное правило","outboundTag":"proxy","domain":["example.invalid"]},
	  {"outboundTag":"direct","enabled":true}
	]`)
	out, err := ConvertV2rayNRouting("shapes", data)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	parsed, err := RoutingA.Parse(out)
	if err != nil {
		t.Fatalf("does not parse: %v\n%s", err, out)
	}
	got := make([]string, 0)
	for _, r := range parsed {
		if v, ok := r.(RoutingA.Routing); ok {
			got = append(got, v.Out+" <- "+v.And[0].Name)
		}
	}
	// five usable rules: the disabled one and the unconditional one are gone
	if len(got) != 5 {
		t.Fatalf("kept %d rules, want 5: %v\n%s", len(got), got, out)
	}
	joined := strings.Join(got, "|")
	for _, want := range []string{"direct <- protocol", "proxy <- port", "direct <- domain", "block <- domain", "proxy <- domain"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	// a rule conditioned on a domain and an ip must keep both
	if !strings.Contains(out, "domain(") || !strings.Contains(out, "ip(") {
		t.Errorf("a multi-condition rule lost a condition:\n%s", out)
	}
	if strings.Contains(out, "example.invalid") {
		t.Errorf("a rule marked enabled:false was imported:\n%s", out)
	}
	if !strings.Contains(out, "example.org") {
		t.Errorf("a rule with no enabled field must be kept, it defaults to on:\n%s", out)
	}
}

// Losing a condition sends traffic somewhere the author did not ask for, so
// the one field RoutingA cannot express has to stop the import.
func TestConvertRejectsProcessMatching(t *testing.T) {
	data := []byte(`[{"outboundTag":"direct","process":["telegram.exe"],"enabled":true}]`)
	_, err := ConvertV2rayNRouting("process", data)
	if err == nil {
		t.Fatal("a process-name rule was imported")
	}
	if !strings.Contains(err.Error(), "process") {
		t.Fatalf("error %q does not say what cannot be expressed", err)
	}
}

func TestConvertRejectsUnusableInput(t *testing.T) {
	for name, data := range map[string]string{
		"not json":        `{`,
		"not an array":    `{"outboundTag":"proxy"}`,
		"no usable rule":  `[{"enabled":false,"outboundTag":"proxy","domain":["x.invalid"]}]`,
		"no outboundTag":  `[{"enabled":true,"domain":["x.invalid"]}]`,
		"empty rule list": `[]`,
	} {
		if _, err := ConvertV2rayNRouting("x", []byte(data)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
