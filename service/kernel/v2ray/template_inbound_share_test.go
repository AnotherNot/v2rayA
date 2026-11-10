package v2ray

import (
	"testing"

	"github.com/v2rayA/v2rayA/db/configure"
)

func withPorts(t *testing.T) {
	t.Helper()
	previous := *configure.GetPortsNotNil()
	t.Cleanup(func() { _ = configure.SetPorts(&previous) })
	ports := previous
	ports.Socks5 = 20180
	ports.Http = 20181
	ports.Socks5WithPac = 20182
	ports.HttpWithPac = 20183
	ports.Vmess = 20184
	if err := configure.SetPorts(&ports); err != nil {
		t.Fatal(err)
	}
}

func TestPortSharingWithoutCredentialsStaysOnLoopback(t *testing.T) {
	withPorts(t)
	setting := configure.NewSetting()
	setting.PortSharing = true
	tmpl := baseTemplate(t, setting)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	for _, ib := range tmpl.Inbounds {
		if ib.Tag == "api-in" || ib.Tag == "transparent" {
			continue
		}
		if ib.Listen != "127.0.0.1" {
			t.Errorf("%s listens on %s without credentials", ib.Tag, ib.Listen)
		}
		if ib.Settings != nil && ib.Settings.Auth == "password" {
			t.Errorf("%s unexpectedly requires a password", ib.Tag)
		}
	}
}

func TestPortSharingWithCredentialsAuthenticatesAndBindsAllInterfaces(t *testing.T) {
	withPorts(t)
	setting := configure.NewSetting()
	setting.PortSharing = true
	setting.PortSharingUsername = "share"
	setting.PortSharingPassword = "share-password"
	tmpl := baseTemplate(t, setting)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	for _, ib := range tmpl.Inbounds {
		switch ib.Tag {
		case "socks", "http", "rule-socks", "rule-http":
			if ib.Settings == nil {
				t.Errorf("%s has no settings", ib.Tag)
				continue
			}
			t.Logf("%s listen=%s auth=%s accounts=%v", ib.Tag, ib.Listen, ib.Settings.Auth, ib.Settings.Accounts)
			if ib.Listen != "0.0.0.0" {
				t.Errorf("%s listens on %s, want 0.0.0.0", ib.Tag, ib.Listen)
			}
			if ib.Settings == nil || ib.Settings.Auth != "password" {
				t.Errorf("%s has no password authentication: %+v", ib.Tag, ib.Settings)
				continue
			}
			if len(ib.Settings.Accounts) != 1 ||
				ib.Settings.Accounts[0].User != "share" ||
				ib.Settings.Accounts[0].Pass != "share-password" {
				t.Errorf("%s accounts = %+v", ib.Tag, ib.Settings.Accounts)
			}
		case "vmess":
			if ib.Listen != "0.0.0.0" {
				t.Errorf("vmess listens on %s, want 0.0.0.0", ib.Listen)
			}
		}
	}
}

func TestPortSharingCredentialsAreIgnoredWhenSharingIsOff(t *testing.T) {
	withPorts(t)
	setting := configure.NewSetting()
	setting.PortSharing = false
	setting.PortSharingUsername = "share"
	setting.PortSharingPassword = "share-password"
	tmpl := baseTemplate(t, setting)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	for _, ib := range tmpl.Inbounds {
		if ib.Tag == "api-in" || ib.Tag == "transparent" {
			continue
		}
		if ib.Listen != "127.0.0.1" {
			t.Errorf("%s listens on %s while sharing is off", ib.Tag, ib.Listen)
		}
	}
}

func TestCustomInboundWithoutCredentialsStaysOnLoopback(t *testing.T) {
	withPorts(t)
	previous := configure.GetCustomInbounds()
	t.Cleanup(func() { _ = configure.SetCustomInbounds(previous) })
	if err := configure.SetCustomInbounds([]configure.CustomInbound{
		{Port: 20200, Protocol: "socks", Tag: "open"},
		{Port: 20201, Protocol: "http", Tag: "closed", Username: "u", Password: "password-1"},
	}); err != nil {
		t.Fatal(err)
	}
	setting := configure.NewSetting()
	setting.PortSharing = true
	tmpl := baseTemplate(t, setting)
	if err := tmpl.setInbound(setting); err != nil {
		t.Fatal(err)
	}
	for _, ib := range tmpl.Inbounds {
		switch ib.Tag {
		case "open":
			if ib.Listen != "127.0.0.1" {
				t.Errorf("open custom inbound listens on %s", ib.Listen)
			}
		case "closed":
			if ib.Listen != "0.0.0.0" {
				t.Errorf("authenticated custom inbound listens on %s", ib.Listen)
			}
		}
	}
}
