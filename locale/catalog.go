package locale

import "fmt"

// catalog holds every localizable human message keyed by a stable message key.
// English is authoritative: every key MUST have an En entry. An unknown key
// falls back to the key itself, so a lookup never panics.
//
// Keys are dotted, namespaced by domain (e.g. "api.task.not_found"). Interpolation
// uses fmt verbs; pass args to T in the same order the verbs appear.
var catalog = map[string]string{}

// Register adds (or overrides) a catalog entry for a message key. Packages call
// this from init() so their domain strings live next to the code that uses them
// while the negotiation/formatting logic stays here. En is required.
func Register(key, en string) {
	catalog[key] = en
}

// Lookup returns the raw (un-interpolated) template for key, falling back to the
// key itself. ok reports whether a catalog entry was found. The lang argument is
// retained for API compatibility; the catalog is English-only.
func Lookup(l Lang, key string) (string, bool) {
	s, exists := catalog[key]
	if !exists {
		return key, false
	}
	return s, true
}

// T resolves key for lang and interpolates args with fmt.Sprintf when any are
// given. Unknown keys return the key verbatim so a missing translation is visible
// but never fatal.
func T(l Lang, key string, args ...any) string {
	s, _ := Lookup(l, key)
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Has reports whether key exists in the catalog (any language).
func Has(key string) bool {
	_, ok := catalog[key]
	return ok
}

// Text translates an explicitly selected built-in English template only.
// Do not pass arbitrary user content or evidence to this function.
func Text(l Lang, english string, args ...any) string { return T(l, english, args...) }
