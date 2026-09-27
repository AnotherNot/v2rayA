package dns

import (
	"io"
	"net"
	"strconv"
	"testing"

	"github.com/miekg/dns"
)

// socksStub is a minimal SOCKS5 CONNECT server: greeting, one CONNECT,
// then a pipe to the target. Enough for the DNS module's proxy path.
func socksStub(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				greet := make([]byte, 3)
				if _, err := io.ReadFull(c, greet); err != nil {
					return
				}
				if _, err := c.Write([]byte{0x05, 0x00}); err != nil {
					return
				}
				head := make([]byte, 4)
				if _, err := io.ReadFull(c, head); err != nil {
					return
				}
				var host string
				switch head[3] {
				case 0x01:
					b := make([]byte, 4)
					if _, err := io.ReadFull(c, b); err != nil {
						return
					}
					host = net.IP(b).String()
				case 0x03:
					lb := make([]byte, 1)
					if _, err := io.ReadFull(c, lb); err != nil {
						return
					}
					b := make([]byte, int(lb[0]))
					if _, err := io.ReadFull(c, b); err != nil {
						return
					}
					host = string(b)
				case 0x04:
					b := make([]byte, 16)
					if _, err := io.ReadFull(c, b); err != nil {
						return
					}
					host = net.IP(b).String()
				default:
					return
				}
				pb := make([]byte, 2)
				if _, err := io.ReadFull(c, pb); err != nil {
					return
				}
				port := int(pb[0])<<8 | int(pb[1])
				target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
				if err != nil {
					c.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
					return
				}
				defer target.Close()
				c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
				go io.Copy(target, c)
				io.Copy(c, target)
			}(c)
		}
	}()
	return ln.Addr().String()
}

// A DoH upstream behind a group tag answers through the SOCKS stub: group
// selection, SOCKS CONNECT, TLS and the DoH POST all in one exchange.
func TestGroupDoHViaSocks(t *testing.T) {
	doh := dohServer(t)
	socks := socksStub(t)
	mgr := NewUpstreamManager([]UpstreamConfig{
		{ID: "doh", Addr: doh.URL + "/dns-query", Protocol: "https", ProxyTag: "regional"},
	})
	mgr.SetGroupTags([]string{"regional"})
	mgr.SetProxyAddrResolver(func(tag string) string {
		if tag == "regional" {
			return socks
		}
		return ""
	})
	up, ok := mgr.GetUpstream("doh")
	if !ok {
		t.Fatal("upstream not registered")
	}
	resp, err := mgr.Exchange(up, &DnsQuery{Name: "example.com", QType: TypeA})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if len(resp.Answer) != 1 || resp.Answer[0].(*dns.A).A.String() != "192.0.2.1" {
		t.Fatalf("answer = %v", resp.Answer)
	}
	if resp.ProxyTag != "regional" {
		t.Fatalf("proxyTag = %q", resp.ProxyTag)
	}
}

// A plain upstream behind a group tag speaks DNS over TCP on the SOCKS
// connection. The target is a local DNS server answering 192.0.2.2.
func TestGroupPlainViaSocks(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				for {
					lb := make([]byte, 2)
					if _, err := io.ReadFull(c, lb); err != nil {
						return
					}
					n := int(lb[0])<<8 | int(lb[1])
					b := make([]byte, n)
					if _, err := io.ReadFull(c, b); err != nil {
						return
					}
					q := new(dns.Msg)
					if err := q.Unpack(b); err != nil {
						return
					}
					a := new(dns.Msg)
					a.SetReply(q)
					a.Answer = append(a.Answer, &dns.A{
						Hdr: dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
						A:   net.ParseIP("192.0.2.2"),
					})
					out, _ := a.Pack()
					c.Write([]byte{byte(len(out) >> 8), byte(len(out))})
					c.Write(out)
				}
			}(c)
		}
	}()
	socks := socksStub(t)
	mgr := NewUpstreamManager([]UpstreamConfig{
		{ID: "plain", Addr: ln.Addr().String(), Protocol: "tcp", ProxyTag: "regional"},
	})
	mgr.SetGroupTags([]string{"regional"})
	mgr.SetProxyAddrResolver(func(string) string { return socks })
	up, ok := mgr.GetUpstream("plain")
	if !ok {
		t.Fatal("upstream not registered")
	}
	resp, err := mgr.Exchange(up, &DnsQuery{Name: "example.com", QType: TypeA})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if len(resp.Answer) != 1 || resp.Answer[0].(*dns.A).A.String() != "192.0.2.2" {
		t.Fatalf("answer = %v", resp.Answer)
	}
}
