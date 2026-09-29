package configure

// MigrateRuleModes renames the China-bound traffic-splitting modes to their
// neutral spellings: whitelist→allowlist, gfwlist→blocklist, in both the
// rule-port mode and the transparent mode. The old spellings stay readable
// everywhere (Normalize* maps them), so databases written by earlier
// releases keep working; the next save persists the new spelling.
//
// The transparent "pac" (follow the rule port) and the other modes keep
// their values: they name no region.
func MigrateRuleModes(setting *Setting) {
	setting.RulePortMode = NormalizeRulePortMode(setting.RulePortMode)
	setting.Transparent = NormalizeTransparentMode(setting.Transparent)
}

// NormalizeRulePortMode maps a stored rule-port mode to its canonical
// spelling. Unknown values pass through untouched: rejecting them here
// would break a database holding a mode this release does not know.
func NormalizeRulePortMode(mode RulePortMode) RulePortMode {
	switch mode {
	case "whitelist":
		return AllowlistMode
	case "gfwlist":
		return BlocklistMode
	default:
		return mode
	}
}

// NormalizeTransparentMode maps a stored transparent mode to its canonical
// spelling, with the same pass-through for unknown values.
func NormalizeTransparentMode(mode TransparentMode) TransparentMode {
	switch mode {
	case "whitelist":
		return TransparentAllowlist
	case "gfwlist":
		return TransparentBlocklist
	default:
		return mode
	}
}
