package report

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

type DnsReporter struct {
}

var DefaultDnsReporter DnsReporter

func (r *DnsReporter) DialDefaultDns() (ok bool, report string) {
	defer func() {
		report = "Default DNS: " + report
	}()
	_, err := net.LookupHost("apple.com")
	if err != nil {
		return false, "failed: " + err.Error()
	}
	return true, "OK"
}

func (r *DnsReporter) Dial() (ok bool, report string) {
	var list = [][2]string{
		{"1.0.0.1:53", "udp"},
		{"8.8.8.8:53", "udp"},
		{"1.0.0.1:53", "tcp"},
		{"8.8.8.8:53", "tcp"},
	}
	var lines []string
	for i := range list {
		_, line := r.dial(list[i][1], list[i][0])
		lines = append(lines, line)
	}
	return true, strings.Join(lines, "\n")
}

func (r *DnsReporter) dial(network string, server string) (ok bool, report string) {
	defer func() {
		report = fmt.Sprintf("DNS: %v(%v): %v", server, network, report)
	}()
	dialer := net.Dialer{Timeout: 1000 * time.Millisecond}
	resolver := &net.Resolver{
		PreferGo:     true,
		StrictErrors: false,
	}
	resolver.Dial = func(ctx context.Context, _network, _address string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, server)
	}
	_, err := resolver.LookupHost(context.Background(), "apple.com")
	if err != nil {
		return false, "failed: " + err.Error()
	}
	return true, "OK"
}
