package configure

import "testing"

func TestDnsModulePortIsCanonicalOrASafeDefault(t *testing.T) {
	cases := []struct {
		addr string
		want string
	}{
		{"0.0.0.0:52353", "52353"},
		{"127.0.0.1:5353", "5353"},
		{"[::1]:5300", "5300"},
		{"127.0.0.1:+5353", "52353"},
		{"127.0.0.1:-1", "52353"},
		{"127.0.0.1:99999", "52353"},
		{"127.0.0.1:0", "52353"},
		{"127.0.0.1", "52353"},
		{"", "52353"},
	}
	for _, tc := range cases {
		setting := &Setting{DnsListenAddr: tc.addr}
		if got := setting.DnsModulePort(); got != tc.want {
			t.Errorf("DnsModulePort(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}
