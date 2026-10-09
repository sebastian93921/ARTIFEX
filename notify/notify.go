// Package notify adapts IM and email finding notifications. It depends only on the standard library
// and the locale catalog, not db/server. JSONB configuration arrives as map[string]any and content as
// Message, allowing signatures, UTF-8 truncation, and filters to be tested without PostgreSQL while
// server handles orchestration. Channel implementations must be stateless because concurrent
// configurations and bot instances share them; credentials always come from cfg and must never be
// cached in adapter fields.
package notify

import "github.com/sebastian93921/artifex/locale"

// Channel identifiers also define valid notification_channels.kind values. The server uses an
// allowlist instead of a DB CHECK, as with finding statuses, to simplify adding adapters.
const (
	KindDingTalk = "dingtalk" // DingTalk custom bot.
	KindFeishu   = "feishu"   // Feishu/Lark custom bot.
	KindWeCom    = "wecom"    // WeCom group bot.
	KindWebhook  = "webhook"  // Generic webhook with custom method, headers, and JSON template.
	KindTelegram = "telegram" // Telegram Bot API
	KindEmail    = "email"    // SMTP email.
)

// Event identifiers for notification_events.kind.
const (
	EventFindingCreated       = "finding_created"
	EventFindingStatusChanged = "finding_status_changed"
)

// InitKind is the fallback when configuration omits kind.
const InitKind = KindDingTalk

// severityRank orders severity values. Unknown values rank zero and fail any valid minimum threshold,
// avoiding noisy uncertain alerts.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// SeverityRank returns the ordinal, or zero for unknown values.
func SeverityRank(severity string) int { return severityRank[severity] }

// SeverityLabel returns a localized emoji label for titles. Unknown values remain verbatim.
func SeverityLabel(severity string, langs ...locale.Lang) string {
	switch severity {
	case "critical":
		return locale.Text(locale.First(langs), "🔴 Critical")
	case "high":
		return locale.Text(locale.First(langs), "🟠 High")
	case "medium":
		return locale.Text(locale.First(langs), "🟡 Medium")
	case "low":
		return locale.Text(locale.First(langs), "🔵 Low")
	default:
		return severity
	}
}

// StatusLabel localizes built-in workflow statuses for transition messages.
func StatusLabel(status string, langs ...locale.Lang) string {
	switch status {
	case "pending":
		return locale.Text(locale.First(langs), "Pending")
	case "in_progress":
		return locale.Text(locale.First(langs), "In progress")
	case "confirmed":
		return locale.Text(locale.First(langs), "Confirmed")
	case "resolved":
		return locale.Text(locale.First(langs), "Resolved")
	case "fixed":
		return locale.Text(locale.First(langs), "Fixed")
	case "false_positive":
		return locale.Text(locale.First(langs), "False positive")
	case "ignored":
		return locale.Text(locale.First(langs), "Ignored")
	case "duplicate":
		return locale.Text(locale.First(langs), "Duplicate")
	case "risk_accepted":
		return locale.Text(locale.First(langs), "Risk accepted")
	default:
		return status
	}
}

// AtLeast checks a severity threshold. Empty min accepts everything; unknown severity ranks zero and
// fails supported nonempty thresholds (see severityRank).
func AtLeast(severity, min string) bool {
	if min == "" {
		return true
	}
	return SeverityRank(severity) >= SeverityRank(min)
}
