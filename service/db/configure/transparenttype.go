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

// AppRouterUnknownOwner is the policy for a connection whose program cannot be
// identified: the process exited before the lookup, or /proc does not show it.
type AppRouterUnknownOwner string

const (
	// AppRouterOwnerDirect leaves such a connection alone.
	AppRouterOwnerDirect = AppRouterUnknownOwner("direct")
	// AppRouterOwnerBlock refuses it. A rule that blocks should not fail open
	// because the process was gone a moment too soon.
	AppRouterOwnerBlock = AppRouterUnknownOwner("block")
)

// ValidateAppRouterUnknownOwner rejects a policy this build cannot honour. An
// empty value means the default, so a setting written before the field existed
// keeps working.
func ValidateAppRouterUnknownOwner(policy AppRouterUnknownOwner) error {
	switch policy {
	case "", AppRouterOwnerDirect, AppRouterOwnerBlock:
		return nil
	}
	return fmt.Errorf("unknown appRouterUnknownOwner %q; use direct or block", string(policy))
}
