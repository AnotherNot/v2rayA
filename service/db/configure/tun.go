package configure

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/v2rayA/v2rayA/common/fileperm"
)

// maxTunScriptSize bounds a user script. The scripts run as root, so a
// document that large is a mistake or an attempt to exhaust memory, not a
// route table.
const maxTunScriptSize = 64 << 10

var tunShellTypes = map[string]struct{}{
	"":                   {},
	"bash":               {},
	"zsh":                {},
	"sh":                 {},
	"fish":               {},
	"windows_powershell": {},
	"pwsh":               {},
	"cmd":                {},
	"git_bash":           {},
	"custom":             {},
}

var allowedTunShellBases = map[string]struct{}{
	"bash":           {},
	"sh":             {},
	"zsh":            {},
	"dash":           {},
	"ksh":            {},
	"mksh":           {},
	"fish":           {},
	"pwsh":           {},
	"pwsh.exe":       {},
	"powershell.exe": {},
	"cmd.exe":        {},
	"bash.exe":       {},
}

// ValidateTunSettings checks the fields that turn into a root shell. It is
// called when settings are saved and again before a script runs, so a
// hand-edited database does not bypass the API check.
func ValidateTunSettings(s *Setting) error {
	if s == nil {
		return nil
	}
	if _, ok := tunShellTypes[s.TunRouteShellType]; !ok {
		return fmt.Errorf("unknown TUN shell type %q", s.TunRouteShellType)
	}
	if s.TunRouteShellType == "custom" || strings.TrimSpace(s.TunRouteShellPath) != "" {
		if err := validateTunShellPath(s.TunRouteShellPath); err != nil {
			return err
		}
	}
	if err := validateTunScript("setup", s.TunSetupScript); err != nil {
		return err
	}
	return validateTunScript("teardown", s.TunTeardownScript)
}

func validateTunScript(name, script string) error {
	if len(script) > maxTunScriptSize {
		return fmt.Errorf("the TUN %s script exceeds the 64 KiB limit", name)
	}
	if strings.IndexByte(script, 0) >= 0 {
		return fmt.Errorf("the TUN %s script contains a NUL byte", name)
	}
	return nil
}

func validateTunShellPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("a custom TUN shell path is empty")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("the TUN shell path %q is not absolute", path)
	}
	if filepath.Clean(path) != path {
		return fmt.Errorf("the TUN shell path %q is not a clean path", path)
	}
	if _, ok := allowedTunShellBases[strings.ToLower(filepath.Base(path))]; !ok {
		return fmt.Errorf("the TUN shell %q is not an allowed shell", filepath.Base(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("the TUN shell %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("the TUN shell %q is not a regular file", path)
	}
	if info.Mode().Perm()&0111 == 0 {
		return fmt.Errorf("the TUN shell %q is not executable", path)
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("the TUN shell %q is writable by group or others", path)
	}
	// A root service must not execute a shell owned by another user: that
	// user could replace it between saves. A lite service may legitimately
	// use the system shell, which root owns.
	if os.Geteuid() == 0 && !fileperm.OwnedByCurrentUser(info) {
		return fmt.Errorf("the TUN shell %q is not owned by root", path)
	}
	return nil
}
