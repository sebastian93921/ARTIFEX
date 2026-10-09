package notify

import (
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"strings"
)

// Email HTML uses inline styles and simple layouts because clients, especially Outlook and enterprise
// mail, differ widely in style-block and flex/grid support.

// htmlSeverityColor provides the accent color for the left border and title.
func htmlSeverityColor(severity string) string {
	switch severity {
	case "critical":
		return "#d32029"
	case "high":
		return "#e8830c"
	case "medium":
		return "#d4b106"
	case "low":
		return "#1677ff"
	default:
		return "#8c8c8c"
	}
}

// htmlTitle returns the email subject.
func htmlTitle(m Message) string {
	return markdownTitle(m)
}

// htmlBody renders email HTML; maxRunes<=0 disables truncation.
func htmlBody(m Message, maxRunes int) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Microsoft YaHei',sans-serif;font-size:14px;color:#262626;line-height:1.6;">`)
	if m.Batch {
		b.WriteString(htmlBatchIntro(m))
		for _, it := range m.Items {
			b.WriteString(htmlItem(it, false, locale.Resolve(m.Language)))
		}
	} else if len(m.Items) > 0 {
		b.WriteString(htmlItem(m.Items[0], true, locale.Resolve(m.Language)))
	}
	if m.HomeURL != "" {
		fmt.Fprintf(&b, locale.Text(locale.Resolve(m.Language), `<p style="margin:16px 0 0;"><a href="%s" style="color:#1677ff;">View all in ARTEX</a></p>`), htmlEscapeAttr(m.HomeURL))
	}
	b.WriteString(`</div>`)
	return TruncateHTML(b.String(), maxRunes)
}

// htmlBatchIntro renders the digest count and severity distribution.
func htmlBatchIntro(m Message) string {
	var b strings.Builder
	if m.WindowMinutes > 0 {
		fmt.Fprintf(&b, locale.Text(locale.Resolve(m.Language), `<h2 style="font-size:16px;margin:0 0 4px;">%d-minute window: %d new findings</h2>`), m.WindowMinutes, len(m.Items))
	} else {
		fmt.Fprintf(&b, locale.Text(locale.Resolve(m.Language), `<h2 style="font-size:16px;margin:0 0 4px;">%d new findings</h2>`), len(m.Items))
	}
	counts := map[string]int{}
	for _, it := range m.Items {
		counts[it.Severity]++
	}
	var parts []string
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf(`<span style="color:%s;font-weight:600;">%s %d</span>`,
				htmlSeverityColor(sev), htmlEscape(SeverityLabel(sev, locale.Resolve(m.Language))), n))
		}
	}
	if len(parts) > 0 {
		fmt.Fprintf(&b, `<p style="margin:0 0 12px;">%s</p>`, strings.Join(parts, " &middot; "))
	}
	return b.String()
}

// htmlItem includes summary and detail link when full is true; otherwise it produces a compact digest
// entry.
func htmlItem(it Item, full bool, langs ...locale.Lang) string {
	color := htmlSeverityColor(it.Severity)
	var b strings.Builder
	if full {
		fmt.Fprintf(&b, `<div style="border-left:4px solid %s;padding:8px 0 8px 12px;margin-bottom:12px;">`, color)
	} else {
		fmt.Fprintf(&b, `<div style="border-left:3px solid %s;padding:4px 0 4px 10px;margin-bottom:8px;">`, color)
	}
	fmt.Fprintf(&b, `<div style="font-weight:600;">%s &middot; %s</div>`,
		htmlEscape(SeverityLabel(it.Severity, locale.First(langs))), htmlEscape(it.Title(locale.First(langs))))

	if !full {
		var extras []string
		if a := assetLine(it.Assets, maxAssetsShown, locale.First(langs)); a != "" {
			extras = append(extras, htmlEscape(a))
		}
		if it.Summary != "" {
			extras = append(extras, htmlEscape(OneLine(it.Summary, 60)))
		}
		if len(extras) > 0 {
			fmt.Fprintf(&b, `<div style="color:#595959;font-size:13px;">%s</div>`, strings.Join(extras, " &middot; "))
		}
		b.WriteString(`</div>`)
		return b.String()
	}

	if it.IsStatusChange() {
		fmt.Fprintf(&b, locale.Text(locale.First(langs), `<div><b>Status change</b>: %s → %s</div>`),
			htmlEscape(StatusLabel(it.FromStatus, locale.First(langs))), htmlEscape(StatusLabel(it.ToStatus, locale.First(langs))))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title(locale.First(langs)) {
		fmt.Fprintf(&b, locale.Text(locale.First(langs), `<div><b>Type</b>: %s</div>`), htmlEscape(it.VulnClass))
	}
	if a := assetLine(it.Assets, maxAssetsShown, locale.First(langs)); a != "" {
		fmt.Fprintf(&b, locale.Text(locale.First(langs), `<div><b>Assets</b>: %s</div>`), htmlEscape(a))
	}
	if s := OneLine(it.Summary, maxSummaryRunes); s != "" {
		fmt.Fprintf(&b, locale.Text(locale.First(langs), `<div><b>Summary</b>: %s</div>`), htmlEscape(s))
	}
	if it.DetailURL != "" {
		fmt.Fprintf(&b, locale.Text(locale.First(langs), `<div style="margin-top:6px;"><a href="%s" style="color:#1677ff;">View details</a></div>`), htmlEscapeAttr(it.DetailURL))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// htmlEscape protects HTML text. Finding titles and summaries originate from untrusted targets/model
// output; escaping prevents arbitrary tags and external images in email.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// htmlEscapeAttr additionally escapes quotes so URLs cannot prematurely close href attributes.
func htmlEscapeAttr(s string) string {
	s = htmlEscape(s)
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
