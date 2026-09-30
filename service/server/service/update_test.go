package service

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/v2rayA/v2rayA/conf"
)

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func serveUpdateJSON(t *testing.T, body string, status int) {
	t.Helper()
	old := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = old })
	http.DefaultClient = &http.Client{Transport: updateTransport(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/AnotherNot/v2rayB/releases/latest") {
			t.Errorf("wrong release feed: %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
}

func TestCheckUpdateReadsForkTag(t *testing.T) {
	oldVersion := conf.Version
	conf.Version = "1.0.0"
	t.Cleanup(func() { conf.Version = oldVersion })
	serveUpdateJSON(t, `{"tag_name":"v9.9.9","other":1}`, 200)
	found, remote, err := CheckUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if remote != "v9.9.9" {
		t.Errorf("remote = %q", remote)
	}
	if !found {
		t.Error("a newer tag was not reported")
	}
}

func TestCheckUpdateClosesFailedBody(t *testing.T) {
	serveUpdateJSON(t, `{"tag_name":`, 200)
	_, _, err := CheckUpdate()
	if err == nil {
		t.Error("partial response must fail")
	}
}

type failedUpdateBody struct{ closed bool }

func (b *failedUpdateBody) Read(p []byte) (int, error) {
	return copy(p, "Package: v2raya\n"), errors.New("interrupted")
}
func (b *failedUpdateBody) Close() error { b.closed = true; return nil }

func TestCheckUpdateClosesInterruptedBody(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	body := &failedUpdateBody{}
	http.DefaultClient = &http.Client{Transport: updateTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header)}, nil
	})}
	_, _, err := CheckUpdate()
	if err == nil {
		t.Error("partial response must fail")
	}
	if !body.closed {
		t.Error("partial response body leaked")
	}
}
