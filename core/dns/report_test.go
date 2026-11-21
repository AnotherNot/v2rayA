package dns

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func rrHeader(name string, rrtype uint16, ttl uint32) dns.RR_Header {
	return dns.RR_Header{Name: name, Rrtype: rrtype, Class: dns.ClassINET, Ttl: ttl}
}

func responseFor(name string, answers ...dns.RR) *DnsResponse {
	return &DnsResponse{
		Query:  DnsQuery{Name: name, QType: TypeA},
		Rcode:  dns.RcodeSuccess,
		Answer: answers,
	}
}

// The question and the names it points at are reported; a record the answer
// bundled for some other name is not, or a rule would route traffic by a name
// that merely appeared next to it.
func TestReportPairsFollowTheQuestion(t *testing.T) {
	cname := &dns.CNAME{
		Hdr:    rrHeader("www.example.com.", dns.TypeCNAME, 60),
		Target: "edge.cdn.test.",
	}
	a := &dns.A{
		Hdr: rrHeader("edge.cdn.test.", dns.TypeA, 30),
		A:   net.IPv4(10, 0, 0, 1),
	}
	aaaa := &dns.AAAA{
		Hdr:  rrHeader("edge.cdn.test.", dns.TypeAAAA, 30),
		AAAA: net.ParseIP("2001:db8::1"),
	}
	unrelated := &dns.A{
		Hdr: rrHeader("other.test.", dns.TypeA, 30),
		A:   net.IPv4(10, 0, 0, 2),
	}
	pairs := reportPairs(responseFor("WWW.Example.COM.", cname, a, aaaa, unrelated))

	seen := map[string]string{}
	for _, p := range pairs {
		seen[p.Name] = p.Addr
	}
	if seen["www.example.com"] != "2001:db8::1" && seen["www.example.com"] != "10.0.0.1" {
		t.Fatalf("the question was not reported: %+v", pairs)
	}
	if len(seen) != 2 {
		t.Fatalf("pairs: %+v", pairs)
	}
	if _, ok := seen["edge.cdn.test"]; !ok {
		t.Fatalf("the name the answer pointed at was not reported: %+v", pairs)
	}
	for _, p := range pairs {
		if p.Addr == "10.0.0.2" {
			t.Fatalf("a record outside the chain was reported: %+v", pairs)
		}
	}
}

func TestReportPairsIgnoreAnswersWithoutAddresses(t *testing.T) {
	if pairs := reportPairs(responseFor("name.test")); len(pairs) != 0 {
		t.Fatalf("an empty answer produced %+v", pairs)
	}
	if pairs := reportPairs(nil); len(pairs) != 0 {
		t.Fatalf("no response produced %+v", pairs)
	}
}

// The reporter writes one JSON line per pair and waits for the service to
// acknowledge it, so the name is stored before the answer goes to the client.
func TestReporterWaitsForTheAck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	lines := make(chan reportPair, 4)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var p reportPair
			if err := json.Unmarshal(scanner.Bytes(), &p); err != nil {
				return
			}
			lines <- p
			if _, err := conn.Write([]byte{dnsAckForTest}); err != nil {
				return
			}
		}
	}()

	r := NewReporter(&ReportConfig{Socket: path, AckTimeoutMS: 500})
	defer r.Close()
	r.report([]reportPair{
		{Name: "a.test", Addr: "10.0.0.1", TTL: 60},
		{Name: "b.test", Addr: "10.0.0.2", TTL: 60},
	})
	for _, want := range []reportPair{
		{Name: "a.test", Addr: "10.0.0.1", TTL: 60},
		{Name: "b.test", Addr: "10.0.0.2", TTL: 60},
	} {
		select {
		case got := <-lines:
			if got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the reporter did not deliver the pair")
		}
	}
}

// A service that is not there yet costs one failed dial, not a timeout on every
// query, and the next second is skipped entirely.
func TestReporterDoesNotWaitWithoutAService(t *testing.T) {
	r := NewReporter(&ReportConfig{Socket: filepath.Join(t.TempDir(), "missing.sock"), AckTimeoutMS: 500})
	defer r.Close()
	start := time.Now()
	r.report([]reportPair{{Name: "a.test", Addr: "10.0.0.1", TTL: 60}})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("a missing socket took %v", elapsed)
	}
	start = time.Now()
	r.report([]reportPair{{Name: "b.test", Addr: "10.0.0.2", TTL: 60}})
	if elapsed := time.Since(start); elapsed > 20*time.Millisecond {
		t.Fatalf("a report during the retry delay took %v", elapsed)
	}
}

// A service that reads but never answers must not hold a DNS answer forever.
func TestReporterGivesUpOnAServiceThatDoesNotAnswer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dns.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufio.NewReader(conn).ReadString('\n')
		time.Sleep(time.Second)
	}()

	r := NewReporter(&ReportConfig{Socket: path, AckTimeoutMS: 50})
	defer r.Close()
	start := time.Now()
	r.report([]reportPair{{Name: "a.test", Addr: "10.0.0.1", TTL: 60}})
	elapsed := time.Since(start)
	if elapsed < 40*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("the wait lasted %v", elapsed)
	}
}

// What the service writes is one ack byte; the test uses the same value.
const dnsAckForTest = '\n'
