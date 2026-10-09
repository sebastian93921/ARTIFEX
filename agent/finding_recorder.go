package agent

import (
	"context"

	"github.com/sebastian93921/artifex/db"
)

// FindingRecorder is injected by the host; agents never synthesize or copy
// evidence bodies themselves. Its implementation owns the atomic write.
type FindingRecorder interface {
	Record(context.Context, db.RecordFindingInput, []db.TrafficRef) (*db.RecordedFinding, error)
}

// Tool-use guidance is appended without replacing the user's editable prompt.
// It does not require capture or claim that unavailable traffic tools exist.
const findingTrafficGuidance = "\n\n**Finding traffic evidence (optional)**: When reporting with report_finding, use traffic_refs to bind actual HTTP request/response IDs in reproduction order only after inspecting and confirming they support the finding. Domain/time filters select candidates, not presumed associations. For TCP/non-HTTP findings, absent capture, or no exact match, omit traffic_refs or pass []; retain command output, logs, or other verifiable evidence and explain missing bindings where appropriate. Do not guess IDs or repeat probes merely to capture packets."

func (t *ToolSet) SetFindingRecorder(r FindingRecorder)   { t.findingRecorder = r }
func (w *Worker) SetFindingRecorder(r FindingRecorder)    { w.findingRecorder = r }
func (p *Planner) SetFindingRecorder(r FindingRecorder)   { p.findingRecorder = r }
func (m *MainAgent) SetFindingRecorder(r FindingRecorder) { m.findingRecorder = r }
