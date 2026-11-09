package controller

import (
	"testing"

	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/db/configure"
)

func TestPutSettingValidatesPortSharingCredentials(t *testing.T) {
	resetSetting(t)
	t.Cleanup(func() { _ = configure.SetSetting(configure.NewSetting()) })

	cases := []struct {
		name   string
		body   string
		ok     bool
		code   string
		stored bool
	}{
		{
			name: "username without password",
			body: `{"portSharing":true,"portSharingUsername":"share"}`,
			code: "PORT_SHARING_CREDENTIALS",
		},
		{
			name: "password without username",
			body: `{"portSharing":true,"portSharingPassword":"share-password"}`,
			code: "PORT_SHARING_CREDENTIALS",
		},
		{
			name: "password too short",
			body: `{"portSharing":true,"portSharingUsername":"share","portSharingPassword":"short"}`,
			code: "PORT_SHARING_CREDENTIALS",
		},
		{
			name: "username with a colon",
			body: `{"portSharing":true,"portSharingUsername":"sh:are","portSharingPassword":"share-password"}`,
			code: "PORT_SHARING_CREDENTIALS",
		},
		{
			name:   "valid pair",
			body:   `{"portSharing":true,"portSharingUsername":"share","portSharingPassword":"share-password"}`,
			ok:     true,
			stored: true,
		},
		{
			name: "sharing without credentials stays allowed",
			body: `{"portSharing":true}`,
			ok:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetSetting(t)
			recorder := putSettingBody(t, tc.body)
			code, errorCode := codeOf(t, recorder)
			if tc.ok {
				if code != common.SUCCESS {
					t.Fatalf("code=%v errorCode=%v (%s)", code, errorCode, recorder.Body.String())
				}
			} else if code != common.FAIL || errorCode != tc.code {
				t.Fatalf("code=%v errorCode=%v, want %s (%s)", code, errorCode, tc.code, recorder.Body.String())
			}
			stored := configure.GetSettingNotNil().PortSharingUsername != ""
			if stored != tc.stored {
				t.Fatalf("stored username = %v, want %v", stored, tc.stored)
			}
		})
	}
}
