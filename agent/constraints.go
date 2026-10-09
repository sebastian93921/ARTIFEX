package agent

import (
	"github.com/sebastian93921/artifex/locale"
	"strings"

	"github.com/sebastian93921/artifex/db"
)

// constraintBlock renders this task's operation constraints (task_constraints) as a
// high-priority block appended to the planner/worker system prompt. allow/deny are
// grouped; empty string when there are no constraints (or ts is nil). The framing
// deliberately puts these ABOVE the exploration/expansion heuristics so a declared
// boundary wins the tug-of-war against "chase another entry surface".
func constraintBlock(ts *db.ExplorationStore, langs ...locale.Lang) string {
	if ts == nil {
		return ""
	}
	rows, err := ts.ListConstraints()
	if err != nil || len(rows) == 0 {
		return ""
	}
	var allow, deny []string
	for _, c := range rows {
		text := strings.TrimSpace(c.Text)
		if text == "" {
			continue
		}
		if c.Kind == "allow" {
			allow = append(allow, "- "+text)
		} else {
			deny = append(deny, "- "+text)
		}
	}
	if len(allow) == 0 && len(deny) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(locale.Text(locale.First(langs), "\n\n[Operating constraints: highest priority, above ALL exploration/expansion heuristics below. Before generating any intent or taking any action, check these constraints and do not proceed if it would violate them]:"))
	if len(allow) > 0 {
		b.WriteString(locale.Text(locale.First(langs), "\nAllowed operations:\n"))
		b.WriteString(strings.Join(allow, "\n"))
	}
	if len(deny) > 0 {
		b.WriteString(locale.Text(locale.First(langs), "\nProhibited operations:\n"))
		b.WriteString(strings.Join(deny, "\n"))
	}
	b.WriteString(locale.Text(locale.First(langs), "\n(Discovering a new target/port/host outside these constraints does NOT grant authorization. Unless within the allowed scope above, record an out-of-scope fact and skip it; do not derive intents or perform actions for it.)"))
	return b.String()
}
