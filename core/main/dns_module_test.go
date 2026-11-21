package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xtls/xray-core/common/cmdarg"
)

// The report section the service writes has to survive the merge of the config
// files, or a rule that names a domain would never learn a name.
func TestDnsModuleConfigCarriesTheReportSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"dns_module":{"report":{"socket":"/run/v2raya/approuter-dns.sock","ack_timeout_ms":50},` +
		`"listener":{"listen_addr":"127.0.0.1:0"},"upstreams":[]}}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := extractDnsModuleConfig(cmdarg.Arg{path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || cfg.Report == nil {
		t.Fatal("the report section was dropped")
	}
	if cfg.Report.Socket != "/run/v2raya/approuter-dns.sock" {
		t.Fatalf("socket %q", cfg.Report.Socket)
	}
	if cfg.Report.AckTimeoutMS != 50 {
		t.Fatalf("ack timeout %d", cfg.Report.AckTimeoutMS)
	}
}
