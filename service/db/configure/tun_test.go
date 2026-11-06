package configure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeShell(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestValidateTunSettings(t *testing.T) {
	shell := writeShell(t, t.TempDir(), "bash", 0700)
	cases := []struct {
		name    string
		setting Setting
		wantErr bool
	}{
		{name: "defaults", setting: Setting{}},
		{name: "known type", setting: Setting{TunRouteShellType: "sh"}},
		{name: "custom shell", setting: Setting{TunRouteShellType: "custom", TunRouteShellPath: shell}},
		{name: "unknown type", setting: Setting{TunRouteShellType: "perl"}, wantErr: true},
		{name: "custom without path", setting: Setting{TunRouteShellType: "custom"}, wantErr: true},
		{name: "non-shell binary", setting: Setting{TunRouteShellType: "custom", TunRouteShellPath: writeShell(t, t.TempDir(), "rm", 0700)}, wantErr: true},
		{name: "world-writable shell", setting: Setting{TunRouteShellType: "custom", TunRouteShellPath: writeShell(t, t.TempDir(), "bash", 0777)}, wantErr: true},
		{name: "relative path", setting: Setting{TunRouteShellType: "custom", TunRouteShellPath: "bash"}, wantErr: true},
		{name: "oversize script", setting: Setting{TunSetupScript: strings.Repeat("a", maxTunScriptSize+1)}, wantErr: true},
		{name: "nul byte", setting: Setting{TunSetupScript: "ip route\x00add"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTunSettings(&tc.setting)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateTunSettings() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
