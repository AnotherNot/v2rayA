package configure

import (
	"fmt"
	"runtime"
)

// ValidateTransparentType rejects an implementation this build cannot run. The
// value reaches writeTransparentProxyRules otherwise, where an unknown one
// fails only after the core has started and the setting has been stored.
func ValidateTransparentType(mode TransparentType) error {
	switch mode {
	case "", TransparentTproxy, TransparentRedirect, TransparentSystemProxy, TransparentTun:
		return nil
	case TransparentAppRouter:
		if runtime.GOOS != "linux" {
			return fmt.Errorf("the app router needs Linux cgroup BPF")
		}
		return nil
	}
	return fmt.Errorf("unknown transparentType %q; use tproxy, redirect, system_proxy, tun or app_router", string(mode))
}