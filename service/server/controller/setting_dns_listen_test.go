package controller

import (
	"testing"

	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
)

func TestPutSettingValidatesDnsListenAddress(t *testing.T) {
	resetSetting(t)
	t.Cleanup(func() { _ = configure.SetSetting(configure.NewSetting()) })

	for _, body := range []string{
		`{"dnsListenAddr":"127.0.0.1"}`,
		`{"dnsListenAddr":"127.0.0.1:0"}`,
		`{"dnsListenAddr":"127.0.0.1:99999"}`,
		`{"dnsListenAddr":"127.0.0.1:+5353"}`,
		`{"dnsListenAddr":"127.0.0.1:-1"}`,
	} {
		recorder := putSettingBody(t, body)
		code, errorCode := codeOf(t, recorder)
		if code != common.FAIL || errorCode != "DNS_LISTEN_ADDR_INVALID" {
			t.Fatalf("%s: code=%v errorCode=%v (%s)", body, code, errorCode, recorder.Body.String())
		}
	}

	for _, body := range []string{
		`{"dnsListenAddr":"127.0.0.1:5353"}`,
		`{"dnsListenAddr":"[::1]:5300"}`,
		`{"dnsListenAddr":":5353"}`,
	} {
		resetSetting(t)
		recorder := putSettingBody(t, body)
		code, errorCode := codeOf(t, recorder)
		if code != common.SUCCESS {
			t.Fatalf("%s: code=%v errorCode=%v (%s)", body, code, errorCode, recorder.Body.String())
		}
	}
}
