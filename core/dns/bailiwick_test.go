package dns

import (
	"testing"

	"github.com/miekg/dns"
)

func mustRR(t *testing.T, text string) dns.RR {
	t.Helper()
	rr, err := dns.NewRR(text)
	if err != nil {
		t.Fatal(err)
	}
	return rr
}

func TestFilterBailiwickKeepsTheChainAndDropsForeignRecords(t *testing.T) {
	msg := new(dns.Msg)
	msg.SetQuestion("www.example.com.", dns.TypeA)
	msg.Answer = []dns.RR{
		mustRR(t, "www.example.com. 60 IN CNAME cdn.example.net."),
		mustRR(t, "cdn.example.net. 60 IN A 203.0.113.10"),
		mustRR(t, "bank.example. 60 IN A 6.6.6.6"),
		mustRR(t, "bank.example. 60 IN TXT \"poison\""),
	}
	msg.Ns = []dns.RR{
		mustRR(t, "example.com. 60 IN SOA ns1.example.com. hostmaster.example.com. 1 2 3 4 5"),
		mustRR(t, "evil.example. 60 IN NS ns.evil.example."),
		mustRR(t, "example.com. 60 IN NS ns1.example.com."),
	}
	msg.Extra = []dns.RR{
		mustRR(t, "ns1.example.com. 60 IN A 203.0.113.53"),
		mustRR(t, "6.6.6.6. 60 IN PTR poison.example."),
		mustRR(t, "cdn.example.net. 60 IN A 203.0.113.10"),
	}
	opt := new(dns.OPT)
	opt.Hdr.Name = "."
	opt.Hdr.Rrtype = dns.TypeOPT
	msg.Extra = append(msg.Extra, opt)

	removed := FilterBailiwick(msg)
	if removed != 4 {
		t.Fatalf("removed %d records, want 4", removed)
	}

	wantAnswer := []string{
		"www.example.com.\t60\tIN\tCNAME\tcdn.example.net.",
		"cdn.example.net.\t60\tIN\tA\t203.0.113.10",
	}
	assertRecords(t, "answer", msg.Answer, wantAnswer)
	wantAuthority := []string{
		"example.com.\t60\tIN\tSOA\tns1.example.com. hostmaster.example.com. 1 2 3 4 5",
		"example.com.\t60\tIN\tNS\tns1.example.com.",
	}
	assertRecords(t, "authority", msg.Ns, wantAuthority)
	wantExtra := []string{
		"ns1.example.com.\t60\tIN\tA\t203.0.113.53",
		"cdn.example.net.\t60\tIN\tA\t203.0.113.10",
	}
	if len(msg.Extra) != 3 {
		t.Fatalf("extra has %d records, want 2 + OPT: %v", len(msg.Extra), msg.Extra)
	}
	if msg.Extra[2].Header().Rrtype != dns.TypeOPT {
		t.Fatalf("OPT record was dropped: %v", msg.Extra)
	}
	assertRecords(t, "extra", msg.Extra[:2], wantExtra)
}

func TestFilterBailiwickHandlesOutOfOrderCNAME(t *testing.T) {
	msg := new(dns.Msg)
	msg.SetQuestion("a.example.com.", dns.TypeA)
	msg.Answer = []dns.RR{
		mustRR(t, "cdn.example.net. 60 IN A 203.0.113.10"),
		mustRR(t, "a.example.com. 60 IN CNAME cdn.example.net."),
	}
	if removed := FilterBailiwick(msg); removed != 0 {
		t.Fatalf("removed %d records, want 0", removed)
	}
	if len(msg.Answer) != 2 {
		t.Fatalf("answer = %v, want both records retained", msg.Answer)
	}
}

func TestFilterBailiwickKeepsNegativeAnswerSOA(t *testing.T) {
	msg := new(dns.Msg)
	msg.SetQuestion("missing.example.com.", dns.TypeA)
	msg.Rcode = dns.RcodeNameError
	msg.Ns = []dns.RR{
		mustRR(t, "example.com. 60 IN SOA ns1.example.com. hostmaster.example.com. 1 2 3 4 5"),
		mustRR(t, "other.example. 60 IN SOA ns.other.example. hostmaster.other.example. 1 2 3 4 5"),
	}
	if removed := FilterBailiwick(msg); removed != 1 {
		t.Fatalf("removed %d records, want 1", removed)
	}
	if len(msg.Ns) != 1 || msg.Ns[0].Header().Name != "example.com." {
		t.Fatalf("authority = %v, want the question's zone", msg.Ns)
	}
}

func TestFilterBailiwickKeepsMXGlue(t *testing.T) {
	msg := new(dns.Msg)
	msg.SetQuestion("example.com.", dns.TypeMX)
	msg.Answer = []dns.RR{
		mustRR(t, "example.com. 60 IN MX 10 mail.example.com."),
	}
	msg.Extra = []dns.RR{
		mustRR(t, "mail.example.com. 60 IN A 203.0.113.25"),
		mustRR(t, "other.example.com. 60 IN A 203.0.113.26"),
	}
	if removed := FilterBailiwick(msg); removed != 1 {
		t.Fatalf("removed %d records, want 1", removed)
	}
	if len(msg.Extra) != 1 || msg.Extra[0].Header().Name != "mail.example.com." {
		t.Fatalf("extra = %v, want only the MX target", msg.Extra)
	}
}

func assertRecords(t *testing.T, section string, got []dns.RR, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s has %d records, want %d: %v", section, len(got), len(want), got)
	}
	for i := range want {
		if got[i].String() != want[i] {
			t.Errorf("%s[%d] = %q, want %q", section, i, got[i].String(), want[i])
		}
	}
}
