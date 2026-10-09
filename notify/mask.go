package notify

import (
	"encoding/json"
	"github.com/sebastian93921/artifex/locale"
	"strings"
)

// MaskedPrefix marks API credential placeholders; submitting one preserves the stored value. A prefix
// rather than an empty/fixed value permits a recognizable suffix without requiring operators to paste
// secrets again.
const MaskedPrefix = "__masked__"

// MaskedValue returns __masked__ for short secrets or __masked__:…ab12cd with the last six characters.
// Webhook identities vary at the end; shared prefixes are unhelpful. Six trailing characters help
// operators recognize a bot without exposing the full credential.
func MaskedValue(secret string) string {
	if len(secret) <= 6 {
		return MaskedPrefix
	}
	return MaskedPrefix + ":…" + secret[len(secret)-6:]
}

// IsMasked identifies an unchanged credential placeholder returned by the API.
func IsMasked(v string) bool { return strings.HasPrefix(v, MaskedPrefix) }

// MaskConfig copies configuration and masks credential fields. Unknown kinds return an empty map
// rather than risking exposure of unidentified secrets. Preserve non-secret fields for the UI.
func MaskConfig(kind string, cfg map[string]any) map[string]any {
	channel, ok := Get(kind)
	if !ok {
		return map[string]any{}
	}
	secrets := map[string]bool{}
	for _, k := range channel.SecretKeys() {
		secrets[k] = true
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if !secrets[k] {
			out[k] = v
			continue
		}
		// Treat nested structures such as headers as one credential. Per-child classification would require
		// additional adapter-specific rules with little benefit.
		if s, ok := v.(string); ok {
			out[k] = MaskedValue(s)
			continue
		}
		out[k] = MaskedPrefix
	}
	return out
}

// ErrDestinationChangedWithoutCredentials rejects a destination change without explicit credential
// choices. See PrepareConfigUpdate for why neither silently allowing nor silently discarding
// credentials is safe.
type ErrDestinationChangedWithoutCredentials struct {
	Changed []string // Changed destination keys.
	Missing []string // Credential keys without an explicit choice.
}

func (e *ErrDestinationChangedWithoutCredentials) Error() string {
	return e.Message(locale.En)
}

// PrepareConfigUpdate replaces bare MergeConfig on channel updates. Otherwise an attacker with PATCH
// access can change only a destination and inherit omitted stored secrets: webhook Authorization
// headers, Telegram /bot<token>/ paths, or SMTP credentials after STARTTLS. This requires no redirect
// and defeats browser masking. Any destination change requires an explicit choice for every secret: a
// new value, or an empty value to remove it. Masks and omitted keys are rejected because both retain
// old credentials. Do not automatically drop optional credentials; silent authentication loss with a
// successful update is harder to diagnose than a clear error.
func PrepareConfigUpdate(kind string, stored, incoming map[string]any) (map[string]any, error) {
	channel, ok := Get(kind)
	if !ok {
		return nil, locale.Errorf("Channel type %q is not registered", kind)
	}
	secrets := channel.SecretKeys()
	destinations := channel.DestinationKeys()

	// Reject masks inside non-string credential containers before any early return. MergeConfig recognizes
	// only whole string masks; nested markers would be saved literally and silently break authentication.
	// Checking after the unchanged-destination return previously left that path unprotected.
	if err := rejectMaskedInContainers(incoming, secrets); err != nil {
		return nil, err
	}

	// Find actually changed destination keys; a mask means unchanged.
	var changed []string
	for _, key := range destinations {
		raw, present := incoming[key]
		if !present {
			continue
		}
		s, isStr := raw.(string)
		if isStr && IsMasked(s) {
			continue
		}
		if !sameConfigValue(raw, stored[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		// An unchanged destination uses normal merging: masks preserve, empty strings clear, other values
		// replace.
		return MergeConfig(stored, incoming), nil
	}

	// A changed destination requires an explicit choice for every credential.
	var missing []string
	for _, key := range secrets {
		raw, present := incoming[key]
		if !present {
			missing = append(missing, key)
			continue
		}
		if s, isStr := raw.(string); isStr && IsMasked(s) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, &ErrDestinationChangedWithoutCredentials{Changed: changed, Missing: missing}
	}
	return MergeConfig(stored, incoming), nil
}

// rejectMaskedInContainers rejects mask sentinels nested in objects/arrays. Masking requires the
// entire value to be a string; headers must be wholly masked or wholly replaced. Nested masks neither
// preserve old values nor represent valid credentials.
func rejectMaskedInContainers(incoming map[string]any, secretKeys []string) error {
	for _, key := range secretKeys {
		raw, present := incoming[key]
		if !present {
			continue
		}
		if _, isStr := raw.(string); isStr {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if strings.Contains(string(encoded), MaskedPrefix) {
			return locale.Errorf("Field %s contains mask marker %q: omit the whole field to keep its value or submit a complete replacement; nested mask placeholders are not allowed",
				key, MaskedPrefix)
		}
	}
	return nil
}

// sameConfigValue compares JSON representations to tolerate int/float64 differences. Normalize empty
// strings and absent keys first because MergeConfig deletes empty values. Otherwise an unchanged
// optional Telegram base_url is stored empty, deleted on first save, then falsely treated as changed
// on every later save, forcing needless token re-entry.
func sameConfigValue(a, b any) bool {
	if isBlankConfigValue(a) && isBlankConfigValue(b) {
		return true
	}
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ra) == string(rb)
}

// isBlankConfigValue must use the same trimmed-empty rule as MergeConfig so equality and clearing
// agree.
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MergeConfig applies partial updates: masks preserve stored values, empty strings explicitly delete
// keys, other supplied values replace, and omitted keys remain. Empty form fields deliberately mean
// clear rather than retain, giving operators a way to remove incorrect configuration; omission and
// masking already express preservation.
func MergeConfig(stored, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range incoming {
		if s, ok := v.(string); ok {
			if IsMasked(s) {
				continue // A mask means unchanged; preserve the stored value.
			}
			if strings.TrimSpace(s) == "" {
				delete(out, k)
				continue
			}
			out[k] = s
			continue
		}
		out[k] = v
	}
	return out
}

// Message renders this validation error without translating destination keys.
func (e *ErrDestinationChangedWithoutCredentials) Message(lang locale.Lang) string {
	return locale.Text(lang, "Destination (%s) changed. Re-enter credential fields (%s): supply new values or explicitly leave them empty if no longer needed. Existing credentials belong to the old destination; reusing them would disclose them to the new destination.", strings.Join(e.Changed, ", "), strings.Join(e.Missing, ", "))
}
