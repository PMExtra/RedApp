package networkproxy

// Redirect protection modes. Strict follows a redirect only to an allowed
// source, basic only applies the general rules (HTTPS across origins, no
// downgrade, no move between public and intranet addresses) and off checks
// nothing. Inherit defers to the vendor, then to the global setting.
const (
	RedirectInherit = "inherit"
	RedirectStrict  = "strict"
	RedirectBasic   = "basic"
	RedirectOff     = "off"
)

// ValidRedirectProtection reports whether mode is a redirect protection mode;
// inherit is valid only below the global setting.
func ValidRedirectProtection(mode string, allowInherit bool) bool {
	switch mode {
	case RedirectStrict, RedirectBasic, RedirectOff:
		return true
	case RedirectInherit:
		return allowInherit
	}
	return false
}

// RedirectEffective is the redirect protection an entity uses and the level
// that set it.
type RedirectEffective struct {
	Mode        string `json:"mode"`
	SourceScope string `json:"source_scope"`
	SourceID    string `json:"source_id"`
}

// ResolveRedirectProtection resolves app → vendor → global. An empty mode,
// as in a configuration written before the setting existed, means inherit.
func ResolveRedirectProtection(app, appID, vendor, vendorID, global string) RedirectEffective {
	mode, scope, id := app, "app", appID
	if mode == "" || mode == RedirectInherit {
		mode, scope, id = vendor, "vendor", vendorID
	}
	if mode == "" || mode == RedirectInherit {
		mode, scope, id = global, "global", ""
	}
	if !ValidRedirectProtection(mode, false) {
		mode = RedirectStrict
	}
	return RedirectEffective{Mode: mode, SourceScope: scope, SourceID: id}
}
