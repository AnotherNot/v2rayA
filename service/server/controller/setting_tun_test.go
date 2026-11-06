package controller

import (
	"testing"

	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
)

func TestPutSettingRequiresTunScriptAcknowledgement(t *testing.T) {
	resetSetting(t)
	t.Cleanup(func() { _ = configure.SetSetting(configure.NewSetting()) })

	body := `{"tunAutoRoute":false,"tunRouteShellType":"sh",` +
		`"tunSetupScript":"ip route add default dev tun0","tunTeardownScript":""}`
	recorder := putSettingBody(t, body)
	if code, errorCode := codeOf(t, recorder); code != common.FAIL || errorCode != "TUN_SCRIPT_ACK_REQUIRED" {
		t.Fatalf("without an acknowledgement: code=%v errorCode=%v (%s)", code, errorCode, recorder.Body.String())
	}
	if got := configure.GetSettingNotNil().TunSetupScript; got != "" {
		t.Fatalf("a rejected script was stored: %q", got)
	}

	body = `{"tunAutoRoute":false,"tunRouteShellType":"sh",` +
		`"tunSetupScript":"ip route add default dev tun0","tunTeardownScript":"",` +
		`"tunScriptAcknowledged":true}`
	recorder = putSettingBody(t, body)
	if code, errorCode := codeOf(t, recorder); code != common.SUCCESS {
		t.Fatalf("with an acknowledgement: code=%v errorCode=%v (%s)", code, errorCode, recorder.Body.String())
	}
	if got := configure.GetSettingNotNil().TunSetupScript; got == "" {
		t.Fatal("the acknowledged script was not stored")
	}

	body = `{"tunAutoRoute":false,"tunRouteShellType":"custom","tunRouteShellPath":"/bin/rm",` +
		`"tunSetupScript":"id","tunScriptAcknowledged":true}`
	recorder = putSettingBody(t, body)
	if code, errorCode := codeOf(t, recorder); code != common.FAIL || errorCode != "TUN_SCRIPT_INVALID" {
		t.Fatalf("a non-shell binary: code=%v errorCode=%v (%s)", code, errorCode, recorder.Body.String())
	}
}
