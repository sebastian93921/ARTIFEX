package notify

import (
	"encoding/json"
	"github.com/sebastian93921/artifex/locale"
	"slices"
	"strings"
)

// Filter defines the notification_channels.filter JSONB contract. All fields are optional; zero values
// mean no filtering, including malformed-config fallback (see ParseFilter).
type Filter struct {
	// MinSeverity is low/medium/high/critical; empty disables the threshold.
	MinSeverity string `json:"min_severity"`
	// Empty TaskIDs/AssetIDs means unrestricted; otherwise the event must intersect the selected scope.
	TaskIDs  []int64 `json:"task_ids"`
	AssetIDs []int64 `json:"asset_ids"`
	// Empty VulnClassInclude accepts all classes; otherwise any keyword must match. Any exclusion match
	// takes precedence. Case-insensitive substring matching avoids malformed regexes silently disabling
	// delivery.
	VulnClassInclude []string `json:"vulnclass_include"`
	VulnClassExclude []string `json:"vulnclass_exclude"`
	// OnStatusChange enables status-change events, relevant only to realtime channels.
	OnStatusChange bool `json:"on_status_change"`
}

// ParseFilter never returns an error. Malformed configuration falls back to a zero Filter and
// unrestricted matching: an extra finding notification is preferable to silently dropping a high-
// severity finding behind an apparently configured channel.
func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) == 0 {
		return f
	}
	// On parse failure, retain the zero Filter and disable filtering.
	_ = json.Unmarshal(raw, &f)
	return f
}

// ValidMinSeverity accepts supported thresholds and an empty unrestricted value.
func ValidMinSeverity(s string) bool {
	if s == "" {
		return true
	}
	_, ok := severityRank[s]
	return ok
}

// Validate checks constrained fields on write. Unknown thresholds rank zero, so a typo such as hgih
// silently accepts everything; although this avoids missed findings, it defeats expected severity
// filtering with no warning. Reject such values at the boundary. Reads remain tolerant through
// ParseFilter so existing bad values do not make historical channels unreadable.
func (f Filter) Validate() error {
	if !ValidMinSeverity(f.MinSeverity) {
		return locale.Errorf("Invalid minimum severity %q; use low / medium / high / critical, or leave empty for no limit", f.MinSeverity)
	}
	return nil
}

// Match determines delivery eligibility and never returns an error, favoring a match over internal
// failure as ParseFilter does. Evaluate event kind, severity, task/asset scope, then class keywords.
func Match(f Filter, s Snapshot) bool {
	// Status changes require explicit opt-in. Most operators expect newly discovered findings rather than
	// every workflow transition.
	if s.Kind == EventFindingStatusChanged && !f.OnStatusChange {
		return false
	}
	if !AtLeast(s.Severity, f.MinSeverity) {
		return false
	}
	if len(f.TaskIDs) > 0 && !slices.Contains(f.TaskIDs, s.TaskID) {
		return false
	}
	if len(f.AssetIDs) > 0 && !intersectsInt(f.AssetIDs, s.AssetIDs) {
		return false
	}
	// Exclusions win even when an inclusion keyword also matches.
	if len(f.VulnClassExclude) > 0 && containsAnyFold(s.VulnClass, f.VulnClassExclude) {
		return false
	}
	if len(f.VulnClassInclude) > 0 && !containsAnyFold(s.VulnClass, f.VulnClassInclude) {
		return false
	}
	return true
}

func intersectsInt(a, b []int64) bool {
	// Linear scans suffice for manually selected sets of a few dozen entries; map allocation offers little
	// benefit.
	for _, v := range b {
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

// containsAnyFold tests case-insensitive substring matches against any keyword.
func containsAnyFold(s string, keywords []string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
