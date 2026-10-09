package server

import (
	"net/http"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/locale"
)

// ---------- P3 agent triggers (custom agents only) ----------

func (s *Server) pgListTriggers(w http.ResponseWriter, r *http.Request) {
	pg, a, ok := s.agentByKey(w, r)
	if !ok {
		return
	}
	trs, err := pg.ListTriggersFor(a.Key)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"triggers": trs})
}

type triggerReq struct {
	Enabled            bool     `json:"enabled"`
	IntervalSec        int      `json:"interval_sec"`
	OnFinding          bool     `json:"on_finding"`
	OnGoalMet          bool     `json:"on_goal_met"`
	OnTaskTimeout      bool     `json:"on_task_timeout"`
	OnToolCall         bool     `json:"on_tool_call"`
	OnTaskCreate       bool     `json:"on_task_create"`
	IntervalMessage    string   `json:"interval_message"`
	FindingMessage     string   `json:"finding_message"`
	GoalMessage        string   `json:"goal_message"`
	TaskTimeoutMessage string   `json:"task_timeout_message"`
	ToolCallMessage    string   `json:"tool_call_message"`
	TaskCreateMessage  string   `json:"task_create_message"`
	ToolNames          []string `json:"tool_names"`
}

// validateTrigger enforces the shared trigger rules for create/update:
// at least one condition, and on_tool_call requires a non-empty tool set.
func validateTrigger(req *triggerReq, langs ...locale.Lang) string {
	if req.IntervalSec == 0 && !req.OnFinding && !req.OnGoalMet && !req.OnTaskTimeout && !req.OnToolCall && !req.OnTaskCreate {
		return locale.Text(locale.First(langs), "Select at least one trigger condition (schedule, finding, goal completion, task timeout, tool call, or task creation)")
	}
	if req.OnToolCall && len(req.ToolNames) == 0 {
		return locale.Text(locale.First(langs), "Select at least one tool for a tool-call trigger")
	}
	return ""
}

func (s *Server) pgCreateTrigger(w http.ResponseWriter, r *http.Request) {
	pg, a, ok := s.agentByKey(w, r)
	if !ok {
		return
	}
	if a.Builtin {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Triggers are supported only for custom agents"))
		return
	}
	var req triggerReq
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err)
		return
	}
	if req.IntervalSec < 0 {
		req.IntervalSec = 0
	}
	if msg := validateTrigger(&req, responseLanguage(w)); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	tr, err := pg.CreateTrigger(&db.AgentTrigger{
		AgentKey: a.Key, Enabled: req.Enabled, IntervalSec: req.IntervalSec,
		OnFinding: req.OnFinding, OnGoalMet: req.OnGoalMet, OnTaskTimeout: req.OnTaskTimeout, OnToolCall: req.OnToolCall, OnTaskCreate: req.OnTaskCreate,
		IntervalMessage: req.IntervalMessage, FindingMessage: req.FindingMessage,
		GoalMessage: req.GoalMessage, TaskTimeoutMessage: req.TaskTimeoutMessage,
		ToolCallMessage: req.ToolCallMessage, TaskCreateMessage: req.TaskCreateMessage, ToolNames: req.ToolNames,
	})
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, tr)
}

func (s *Server) pgUpdateTrigger(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad trigger id"))
		return
	}
	var req triggerReq
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err)
		return
	}
	if req.IntervalSec < 0 {
		req.IntervalSec = 0
	}
	if msg := validateTrigger(&req, responseLanguage(w)); msg != "" {
		writeErr(w, 400, msg)
		return
	}
	if err := pg.UpdateTrigger(&db.AgentTrigger{
		ID: id, Enabled: req.Enabled, IntervalSec: req.IntervalSec,
		OnFinding: req.OnFinding, OnGoalMet: req.OnGoalMet, OnTaskTimeout: req.OnTaskTimeout, OnToolCall: req.OnToolCall, OnTaskCreate: req.OnTaskCreate,
		IntervalMessage: req.IntervalMessage, FindingMessage: req.FindingMessage,
		GoalMessage: req.GoalMessage, TaskTimeoutMessage: req.TaskTimeoutMessage,
		ToolCallMessage: req.ToolCallMessage, TaskCreateMessage: req.TaskCreateMessage, ToolNames: req.ToolNames,
	}); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) pgDeleteTrigger(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad trigger id"))
		return
	}
	if err := pg.DeleteTrigger(id); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}
