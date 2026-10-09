package report

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/locale"
)

// Findings export renders summary Markdown, individual Markdown, or CSV.
// The server serializes JSON directly using DTOs.

// sortFindingsForExport orders by descending severity, then newest first.
func sortFindingsForExport(fs []*db.DBFinding) {
	sort.SliceStable(fs, func(i, j int) bool {
		ri, rj := sevRank[fs[i].Severity], sevRank[fs[j].Severity]
		if ri != rj {
			return ri < rj // A lower rank means higher severity.
		}
		return fs[i].CreatedAt.After(fs[j].CreatedAt)
	})
}

// findingTitle prefers a name, then a class, then the localized fallback.
func findingTitle(f *db.DBFinding, langs ...locale.Lang) string {
	lang := locale.First(langs)
	return nz(f.Name, nz(f.VulnClass, locale.Text(lang, "Unclassified")))
}

// FindingsMarkdown renders a severity-sorted report with summary counts,
// class, status, task, evidence, and the stored detailed report.
func FindingsMarkdown(fs []*db.DBFinding, generatedAt time.Time, langs ...locale.Lang) string {
	lang := locale.First(langs)
	items := append([]*db.DBFinding(nil), fs...)
	sortFindingsForExport(items)

	var b strings.Builder
	b.WriteString(locale.Text(lang, "# Vulnerability findings report\n\n"))
	fmt.Fprintf(&b, locale.Text(lang, "- **Generated at**: %s\n"), generatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, locale.Text(lang, "- **Total findings**: %d\n\n"), len(items))

	// Count findings by severity for the summary.
	counts := map[string]int{}
	for _, f := range items {
		counts[f.Severity]++
	}
	b.WriteString(locale.Text(lang, "## Summary\n\n"))
	b.WriteString(locale.Text(lang, "| Severity | Count |\n| --- | --- |\n"))
	for _, s := range []struct{ key, label string }{
		{"critical", locale.Text(lang, "Critical")}, {"high", locale.Text(lang, "High")}, {"medium", locale.Text(lang, "Medium")}, {"low", locale.Text(lang, "Low")},
	} {
		fmt.Fprintf(&b, "| %s | %d |\n", s.label, counts[s.key])
	}
	b.WriteString("\n")

	if len(items) == 0 {
		b.WriteString(locale.Text(lang, "_No matching vulnerabilities._\n"))
		return b.String()
	}

	b.WriteString(locale.Text(lang, "## Finding details\n\n"))
	for i, f := range items {
		fmt.Fprintf(&b, "### %d. [%s] %s\n\n", i+1, severityLabel(lang, nz(f.Severity, "info")), findingTitle(f, lang))
		if f.VulnClass != "" {
			fmt.Fprintf(&b, locale.Text(lang, "- **Class**: %s\n"), f.VulnClass)
		}
		fmt.Fprintf(&b, locale.Text(lang, "- **Status**: %s\n"), statusLabel(lang, nz(f.Status, "pending")))
		if desc := strings.TrimSpace(f.TaskDescription); desc != "" {
			fmt.Fprintf(&b, locale.Text(lang, "- **Task**: %s\n"), desc)
		}
		fmt.Fprintf(&b, locale.Text(lang, "- **Found at**: %s\n\n"), f.CreatedAt.Format("2006-01-02 15:04:05"))
		if s := strings.TrimSpace(f.Summary); s != "" {
			fmt.Fprintf(&b, "%s\n\n", s)
		}
		if e := strings.TrimSpace(f.Evidence); e != "" {
			fmt.Fprintf(&b, locale.Text(lang, "**Evidence:**\n\n```\n%s\n```\n\n"), e)
		}
		if rep := strings.TrimSpace(f.Report); rep != "" {
			b.WriteString(locale.Text(lang, "**Detailed report:**\n\n"))
			b.WriteString(rep)
			b.WriteString("\n\n")
		}
		b.WriteString(findingTrafficMarkdown(f, false, lang))
		b.WriteString("---\n\n")
	}
	return b.String()
}

// SingleFindingMarkdown renders one finding for individual-file archives.
func SingleFindingMarkdown(f *db.DBFinding, generatedAt time.Time, langs ...locale.Lang) string {
	lang := locale.First(langs)
	var b strings.Builder
	fmt.Fprintf(&b, "# [%s] %s\n\n", severityLabel(lang, nz(f.Severity, "info")), findingTitle(f, lang))
	if f.VulnClass != "" {
		fmt.Fprintf(&b, locale.Text(lang, "- **Class**: %s\n"), f.VulnClass)
	}
	fmt.Fprintf(&b, locale.Text(lang, "- **Severity**: %s\n"), severityLabel(lang, nz(f.Severity, "info")))
	fmt.Fprintf(&b, locale.Text(lang, "- **Status**: %s\n"), statusLabel(lang, nz(f.Status, "pending")))
	if desc := strings.TrimSpace(f.TaskDescription); desc != "" {
		fmt.Fprintf(&b, locale.Text(lang, "- **Task**: %s\n"), desc)
	}
	fmt.Fprintf(&b, locale.Text(lang, "- **Found at**: %s\n"), f.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, locale.Text(lang, "- **Generated at**: %s\n\n"), generatedAt.Format("2006-01-02 15:04:05"))
	if s := strings.TrimSpace(f.Summary); s != "" {
		fmt.Fprintf(&b, locale.Text(lang, "## Overview\n\n%s\n\n"), s)
	}
	if e := strings.TrimSpace(f.Evidence); e != "" {
		fmt.Fprintf(&b, locale.Text(lang, "## Evidence\n\n```\n%s\n```\n\n"), e)
	}
	if rep := strings.TrimSpace(f.Report); rep != "" {
		b.WriteString(locale.Text(lang, "## Detailed report\n\n"))
		b.WriteString(rep)
		b.WriteString("\n")
	}
	b.WriteString(findingTrafficMarkdown(f, true, lang))
	return b.String()
}

var unsafeFilenameChars = regexp.MustCompile(`[^\p{Han}\p{L}\p{N}._-]+`)

// FindingFilename generates a safe name such as critical_SQLi_#123.md.
// Path separators and control characters are removed for safe ZIP entries.
func FindingFilename(f *db.DBFinding) string {
	lang := locale.En
	sev := nz(f.Severity, "info")
	title := findingTitle(f, lang)
	name := fmt.Sprintf("%s_%s_#%d", sev, title, f.ID)
	name = unsafeFilenameChars.ReplaceAllString(name, "_")
	name = strings.Trim(name, "._")
	if name == "" {
		name = fmt.Sprintf("finding_%d", f.ID)
	}
	// Strip any remaining directory components to prevent ZIP path traversal.
	name = path.Base(name)
	if len(name) > 120 {
		name = name[:120]
	}
	return name + ".md"
}

// FindingsCSV renders summary fields with a UTF-8 BOM for Excel.
// Use Markdown or JSON for full report and evidence content.
func FindingsCSV(fs []*db.DBFinding, langs ...locale.Lang) []byte {
	lang := locale.First(langs)
	items := append([]*db.DBFinding(nil), fs...)
	sortFindingsForExport(items)

	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // UTF-8 BOM
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"ID", locale.Text(lang, "Name"), locale.Text(lang, "Class"), locale.Text(lang, "Severity"), locale.Text(lang, "Status"), locale.Text(lang, "Task"), locale.Text(lang, "Found at"), locale.Text(lang, "Overview"), locale.Text(lang, "Traffic evidence count"), locale.Text(lang, "Traffic evidence IDs")})
	for _, f := range items {
		_ = w.Write([]string{
			fmt.Sprintf("%d", f.ID),
			findingTitle(f, lang),
			f.VulnClass,
			nz(f.Severity, "info"),
			nz(f.Status, "pending"),
			f.TaskDescription,
			f.CreatedAt.Format("2006-01-02 15:04:05"),
			strings.TrimSpace(f.Summary),
			fmt.Sprint(len(f.TrafficBindings)), findingTrafficIDs(f),
		})
	}
	w.Flush()
	return buf.Bytes()
}

func findingTrafficIDs(f *db.DBFinding) string {
	ids := make([]string, 0, len(f.TrafficBindings))
	for _, b := range f.TrafficBindings {
		ids = append(ids, fmt.Sprint(b.ID))
	}
	return strings.Join(ids, ",")
}

func findingTrafficMarkdown(f *db.DBFinding, attachments bool, langs ...locale.Lang) string {
	lang := locale.First(langs)
	stale := f.Report != "" && f.EvidenceVersion != f.ReportEvidenceVersion
	if len(f.TrafficBindings) == 0 && !stale {
		return ""
	}
	var out strings.Builder
	out.WriteString(locale.Text(lang, "\n## Linked traffic evidence\n\n"))
	fmt.Fprintf(&out, locale.Text(lang, "Evidence version: %d; linked items: %d.\n\n"), f.EvidenceVersion, len(f.TrafficBindings))
	if stale {
		out.WriteString(locale.Text(lang, "Evidence has changed; the detailed report needs updating.\n\n"))
	}
	for i, b := range f.TrafficBindings {
		fmt.Fprintf(&out, locale.Text(lang, "%d. **Evidence #%d · %s** — `%s %s`, status %d\n"), i+1, b.ID, b.Role, b.Snapshot.Method, strings.ReplaceAll(b.Snapshot.URL, "`", "%60"), b.Snapshot.Status)
		if b.Note != "" {
			fmt.Fprintf(&out, "   %s\n", strings.ReplaceAll(b.Note, "\n", "\n   "))
		}
		if attachments {
			fmt.Fprintf(&out, locale.Text(lang, "   [Request](evidence/%d/%d/request.http) · [Response](evidence/%d/%d/response.http)\n"), f.ID, b.ID, f.ID, b.ID)
		}
	}
	out.WriteString("\n")
	return out.String()
}
