package notify

import (
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"strings"
)

// Shared Markdown rendering for DingTalk and WeCom. Feishu cards, Telegram HTML, and email HTML render
// in their own adapters.

// maxAssetsShown limits displayed assets. Findings can span dozens; listing every domain would
// overwhelm an IM notification without useful detail.
const maxAssetsShown = 3

// maxSummaryRunes bounds summaries: IM prompts readers to open the full finding, rather than replacing
// the report.
const maxSummaryRunes = 120

// markdownReservedBytes reserves the digest header, severity distribution, continuation notice, and
// dashboard footer. Without this allowance, truncation could hide the batch identity or remaining
// count.
const markdownReservedBytes = 320

// markdownEscape escapes structural Markdown characters in untrusted target/model titles, summaries,
// classes, and asset URLs. A forged verification link could become clickable; an injected image beacon
// could reveal reads and reader IPs, while even harmless formatting could push severe findings below a
// fold. Escape headings, links, emphasis, lists, quotes, and strikeout characters. Escape backslashes
// first to avoid double-escaping inserted slashes.
func markdownEscape(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"[", `\[`,
		"]", `\]`,
		"(", `\(`,
		")", `\)`,
		"!", `\!`,
		"#", `\#`,
		">", `\>`,
		"|", `\|`,
		"~", `\~`,
	)
	return replacer.Replace(s)
}

// markdownText normalizes to one line and escapes Markdown. Both are required: newlines alone can
// forge list entries or quote blocks despite escaped punctuation.
func markdownText(s string, maxRunes int) string {
	return markdownEscape(OneLine(s, maxRunes))
}

// markdownTitle returns unescaped text shared by Markdown, Telegram HTML, Feishu plain_text, webhook
// JSON, and email subjects. Each output must escape for its own context (writeItem, feishuItemLines,
// telegramEscape); shared Markdown escaping previously exposed visible backslashes such as \(1\) in
// Telegram and polluted JSON.
func markdownTitle(m Message) string {
	if m.Batch {
		return fmt.Sprintf(locale.Text(locale.Resolve(m.Language), "Finding digest · %d total"), len(m.Items))
	}
	if len(m.Items) == 0 {
		return locale.Text(locale.Resolve(m.Language), "Finding notification")
	}
	it := m.Items[0]
	return fmt.Sprintf("[%s] %s", SeverityLabel(it.Severity, locale.Resolve(m.Language)), OneLine(it.Title(locale.Resolve(m.Language)), 0))
}

// markdownBody returns text and the number of included items. Mark only the first kept entries
// delivered; size-limited omissions must remain for later batches. Otherwise truncated findings
// disappear while history falsely records success. maxBytes<=0 disables the limit.
func markdownBody(m Message, maxBytes int) (string, int) {
	if !m.Batch {
		if len(m.Items) == 0 {
			return "", 0
		}
		var b strings.Builder
		writeItem(&b, m.Items[0], "", true, locale.Resolve(m.Language))
		// Even an oversized single item is sent with final truncation: partial information is preferable to no
		// notification.
		return TruncateBytes(b.String(), maxBytes), 1
	}

	footer := ""
	if m.HomeURL != "" {
		footer = fmt.Sprintf(locale.Text(locale.Resolve(m.Language), "\n[View all in ARTEX](%s)\n"), m.HomeURL)
	}
	kept := packItemCount(m.Items, maxBytes, markdownReservedBytes, footer, byteSize, func(it Item, idx int) string {
		var b strings.Builder
		writeItem(&b, it, fmt.Sprintf("%d. ", idx+1), false, locale.Resolve(m.Language))
		return b.String()
	})

	items := m.Items[:kept]
	var b strings.Builder
	b.WriteString(markdownBatchIntro(m, items, len(m.Items)))
	for i, it := range items {
		writeItem(&b, it, fmt.Sprintf("%d. ", i+1), false, locale.Resolve(m.Language))
	}
	b.WriteString(footer)
	return TruncateBytes(b.String(), maxBytes), kept
}

// markdownBatchIntro includes the time window, count, and severity distribution for quick triage.
// items contains only entries that fit; total counts the entire batch. Explicitly announce remaining
// entries so readers do not mistake a partial delivery for the complete batch.
func markdownBatchIntro(m Message, items []Item, total int) string {
	var b strings.Builder
	if m.WindowMinutes > 0 {
		fmt.Fprintf(&b, locale.Text(locale.Resolve(m.Language), "**%d-minute window: %d new findings**"), m.WindowMinutes, total)
	} else {
		fmt.Fprintf(&b, locale.Text(locale.Resolve(m.Language), "**%d new findings**"), total)
	}
	if extra := total - len(items); extra > 0 {
		fmt.Fprintf(&b, locale.Text(locale.Resolve(m.Language), " (showing the first %d; the remaining %d will follow in the next message)"), len(items), extra)
	}
	// Count only included items so severity totals match the visible entries.
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Severity]++
	}
	var parts []string
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", SeverityLabel(sev, locale.Resolve(m.Language)), n))
		}
	}
	if len(parts) > 0 {
		b.WriteString("\n" + strings.Join(parts, " · "))
	}
	b.WriteString("\n\n")
	return b.String()
}

// writeItem renders one finding. prefix numbers digest entries; single includes summary and detail
// link, while digests stay compact. Normalize and escape all external title/class/asset/summary fields
// through markdownText. Administrator-configured detail links remain clickable URLs.
func writeItem(b *strings.Builder, it Item, prefix string, single bool, langs ...locale.Lang) {
	line := fmt.Sprintf("%s**%s · %s**", prefix, SeverityLabel(it.Severity, locale.First(langs)), markdownText(it.Title(locale.First(langs)), 0))
	if !single {
		// Digest entries occupy one line with shortened assets and summary.
		var extras []string
		if a := assetLine(it.Assets, maxAssetsShown, locale.First(langs)); a != "" {
			extras = append(extras, markdownText(a, 0))
		}
		if it.Summary != "" {
			extras = append(extras, markdownText(it.Summary, 60))
		}
		if len(extras) > 0 {
			line += " — " + strings.Join(extras, " · ")
		}
		b.WriteString(line + "\n")
		return
	}
	b.WriteString(line + "\n")
	if it.IsStatusChange() {
		fmt.Fprintf(b, locale.Text(locale.First(langs), "**Status change**: %s → %s\n"),
			markdownText(StatusLabel(it.FromStatus, locale.First(langs)), 0), markdownText(StatusLabel(it.ToStatus, locale.First(langs)), 0))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title(locale.First(langs)) {
		fmt.Fprintf(b, locale.Text(locale.First(langs), "**Type**: %s\n"), markdownText(it.VulnClass, 0))
	}
	if a := assetLine(it.Assets, maxAssetsShown, locale.First(langs)); a != "" {
		fmt.Fprintf(b, locale.Text(locale.First(langs), "**Assets**: %s\n"), markdownText(a, 0))
	}
	if it.Summary != "" {
		if s := markdownText(it.Summary, maxSummaryRunes); s != "" {
			fmt.Fprintf(b, locale.Text(locale.First(langs), "**Summary**: %s\n"), s)
		}
	}
	if it.DetailURL != "" {
		fmt.Fprintf(b, locale.Text(locale.First(langs), "[View details](%s)\n"), it.DetailURL)
	}
}
