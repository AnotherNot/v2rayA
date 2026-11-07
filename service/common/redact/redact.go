// Package redact keeps credentials out of the log. Subscription addresses
// carry provider tokens in the query string and sometimes in a path segment;
// share links carry UUIDs and passwords. A log line only needs to name the
// source, so the helpers return the scheme and host and drop the rest.
package redact

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// URL returns scheme://host for an address. Userinfo, path, query and
// fragment are dropped because any of them can hold a subscription token.
// A string that does not parse as a URL becomes "[redacted]".
func URL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "[redacted]"
	}
	return strings.ToLower(u.Scheme) + "://" + u.Host
}

// Link returns the scheme of a share link and nothing else. A vmess or
// shadowsocks link encodes the server and its credentials in the opaque part
// of the URL, so even the host is not safe to print.
func Link(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" {
		return "[redacted]"
	}
	return strings.ToLower(u.Scheme) + "://[redacted]"
}

// Host returns the hostname of a URL, or an empty string when there is none.
// It is for messages that name a source host without echoing the address.
func Host(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// Error removes the address from a transport error. net/http returns a
// *url.Error that carries the whole request URL in its text, including the
// subscription token in the query; the wrapped cause is kept so callers can
// still tell a timeout from a DNS failure.
func Error(err error) error {
	if err == nil {
		return nil
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Err == nil {
			return fmt.Errorf("%s", URL(urlErr.URL))
		}
		return fmt.Errorf("%s: %w", URL(urlErr.URL), urlErr.Err)
	}
	return err
}
