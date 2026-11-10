package iptables

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestIsNftablesSupported(t *testing.T) {
	const (
		versionCommand = "iptables --version"
		mangleCommand  = "iptables -t mangle -L"
		legacyVersion  = "iptables v1.8.11 (legacy)\n"
	)

	tests := []struct {
		name         string
		support      string
		docker       bool
		noNft        bool
		versionOut   string
		versionErr   error
		mangleOut    string
		mangleErr    error
		want         bool
		wantCommands []string
	}{
		{
			name:    "explicit on overrides docker",
			support: "on",
			docker:  true,
			want:    true,
		},
		{
			name:    "explicit off",
			support: "off",
		},
		{
			name:   "docker auto remains legacy",
			docker: true,
		},
		{
			name:  "auto requires nft command",
			noNft: true,
		},
		{
			name:         "nf_tables backend needs no probe",
			versionOut:   "iptables v1.8.11 (nf_tables)\n",
			want:         true,
			wantCommands: []string{versionCommand},
		},
		{
			name:         "missing iptables preserves existing fallback",
			versionErr:   errors.New("executable file not found"),
			want:         true,
			wantCommands: []string{versionCommand},
		},
		{
			name:         "legacy with mangle remains legacy",
			versionOut:   legacyVersion,
			mangleOut:    "Chain PREROUTING (policy ACCEPT)\n",
			wantCommands: []string{versionCommand, mangleCommand},
		},
		{
			name:         "legacy without mangle selects nft",
			versionOut:   legacyVersion,
			mangleOut:    "iptables v1.8.11 (legacy): can't initialize iptables table 'mangle': Table does not exist\n",
			mangleErr:    errors.New("exit status 3"),
			want:         true,
			wantCommands: []string{versionCommand, mangleCommand},
		},
		{
			name:         "legacy permission error remains legacy",
			versionOut:   legacyVersion,
			mangleOut:    "iptables: Permission denied (you must be root)\n",
			mangleErr:    errors.New("exit status 4"),
			wantCommands: []string{versionCommand, mangleCommand},
		},
		{
			name:         "legacy arbitrary failure remains legacy",
			versionOut:   legacyVersion,
			mangleOut:    "iptables: Invalid argument\n",
			mangleErr:    errors.New("exit status 2"),
			wantCommands: []string{versionCommand, mangleCommand},
		},
		{
			name:         "unrelated missing table remains legacy",
			versionOut:   legacyVersion,
			mangleOut:    "iptables v1.8.11 (legacy): can't initialize iptables table 'filter': Table does not exist\n",
			mangleErr:    errors.New("exit status 3"),
			wantCommands: []string{versionCommand, mangleCommand},
		},
		{
			name:         "diagnostic without command failure remains legacy",
			versionOut:   legacyVersion,
			mangleOut:    "can't initialize iptables table 'mangle': Table does not exist\n",
			wantCommands: []string{versionCommand, mangleCommand},
		},
		{
			name:         "unidentified backend needs no probe",
			versionOut:   "iptables v1.4.21\n",
			wantCommands: []string{versionCommand},
		},
	}

	oldCommandAvailable := commandAvailable
	oldCommandOutput := commandOutput
	oldCommandCombinedOutput := commandCombinedOutput
	t.Cleanup(func() {
		commandAvailable = oldCommandAvailable
		commandOutput = oldCommandOutput
		commandCombinedOutput = oldCommandCombinedOutput
	})

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nftChecks := 0
			commandAvailable = func(command string) bool {
				if command != "nft" {
					t.Fatalf("unexpected command availability check: %q", command)
				}
				nftChecks++
				return !tc.noNft
			}

			var commands []string
			commandOutput = func(name string, args ...string) ([]byte, error) {
				command := strings.TrimSpace(name + " " + strings.Join(args, " "))
				commands = append(commands, command)
				if command != versionCommand {
					t.Fatalf("unexpected command: %q", command)
				}
				return []byte(tc.versionOut), tc.versionErr
			}
			commandCombinedOutput = func(name string, args ...string) ([]byte, error) {
				command := strings.TrimSpace(name + " " + strings.Join(args, " "))
				commands = append(commands, command)
				if command != mangleCommand {
					t.Fatalf("unexpected command: %q", command)
				}
				return []byte(tc.mangleOut), tc.mangleErr
			}

			if got := isNftablesSupported(tc.support, tc.docker); got != tc.want {
				t.Errorf("isNftablesSupported(%q, %t) = %t, want %t", tc.support, tc.docker, got, tc.want)
			}
			wantNftChecks := 0
			if tc.support != "on" && tc.support != "off" && !tc.docker {
				wantNftChecks = 1
			}
			if nftChecks != wantNftChecks {
				t.Errorf("nft availability checks = %d, want %d", nftChecks, wantNftChecks)
			}
			if !reflect.DeepEqual(commands, tc.wantCommands) {
				t.Errorf("commands = %#v, want %#v", commands, tc.wantCommands)
			}
		})
	}
}

func TestNormalizeCIDR(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{"1.2.3.4", "1.2.3.4/32", true},
		{" 1.2.3.0/24 ", "1.2.3.0/24", true},
		{"1.2.3.4/24", "1.2.3.0/24", true},
		{"2001:db8::1", "2001:db8::1/128", true},
		{"2001:db8::/32", "2001:db8::/32", true},
		{"", "", false},
		{"1.2.3.4/33", "", false},
		{"1.2.3.4; rm -rf /", "", false},
		{"example.com", "", false},
	}
	for _, tc := range cases {
		got, ok := normalizeCIDR(tc.raw)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("normalizeCIDR(%q) = %q, %v; want %q, %v", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNftSetFor(t *testing.T) {
	if got := nftSetFor("192.0.2.0/24"); got != "local_ips" {
		t.Errorf("IPv4 set = %q", got)
	}
	if got := nftSetFor("2001:db8::/32"); got != "local_ips6" {
		t.Errorf("IPv6 set = %q", got)
	}
}
