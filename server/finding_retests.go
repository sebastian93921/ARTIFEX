package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sebastian93921/artifex/agent"
	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/intercept"
	"github.com/sebastian93921/artifex/locale"
	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) listActiveFindingRetests(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	items, err := pg.ListActiveFindingRetests(r.Context())
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"retests": items})
}

func (s *Server) listFindingRetests(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad finding id"))
		return
	}
	f, err := pg.GetFinding(id)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if f == nil {
		writeErr(w, 404, locale.Text(responseLanguage(w), "finding not found"))
		return
	}
	items, err := pg.ListFindingRetests(id)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"retests": items})
}

func (s *Server) startFindingRetest(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad finding id"))
		return
	}
	var req struct {
		Notes string `json:"notes"`
	}
	if !decodeConversationRequest(w, r, &req) {
		return
	}
	req.Notes = strings.TrimSpace(req.Notes)
	if utf8.RuneCountInString(req.Notes) > 4000 {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Retest notes must not exceed 4000 characters"))
		return
	}
	f, err := pg.GetFinding(id)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if f == nil {
		writeErr(w, 404, locale.Text(responseLanguage(w), "finding not found"))
		return
	}
	a, err := pg.GetAgentByKey(db.FindingRetestAgentKey)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if a == nil || !a.Enabled {
		writeErr(w, 409, locale.Text(responseLanguage(w), "The finding retest agent is missing or disabled; configure retester in agent management"))
		return
	}
	for _, key := range []string{"get_finding_retest_context", "record_finding_retest_result"} {
		t, err := pg.GetTool(key)
		if err != nil {
			writeError(w, 500, err)
			return
		}
		if t == nil || !t.Enabled || !slices.Contains(t.Agents, a.Key) {
			writeErr(w, 409, locale.Text(responseLanguage(w), "Enable and bind this tool for the retest agent: %s", key))
			return
		}
	}
	if s.resolveChatAgent(&db.Conversation{AgentKey: a.Key}) == nil {
		writeErr(w, 503, s.chatUnavailableReason(locale.FromRequest(r)))
		return
	}
	if s.ctx.Err() != nil {
		writeErr(w, 503, locale.Text(responseLanguage(w), "Service is stopping"))
		return
	}
	retest, conv, created, err := pg.CreateFindingRetest(r.Context(), id, req.Notes)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, locale.Text(responseLanguage(w), "finding not found"))
		return
	}
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if created {
		busyKey := s.convBusyKey(conv.ID)
		s.chatMu.Lock()
		s.chatBusy[busyKey] = true
		s.chatMu.Unlock()
		s.runConversationWithContext(r.Context(), conv, retest.InitialMessageForLanguage(locale.FromRequest(r)), busyKey)
	}
	code := http.StatusOK
	if created {
		code = http.StatusAccepted
	}
	writeJSON(w, code, map[string]any{"retest": retest, "created": created})
}

// Both tools derive the finding from server-owned conversation context. Tool
// arguments cannot redirect a result into a different finding or conversation.
func (s *Server) findingRetestTools(langs ...locale.Lang) []actool.CoreTool {
	lang := findingToolLanguage(langs)
	return []actool.CoreTool{
		roTool("get_finding_retest_context", locale.Text(lang, "Read the finding evidence snapshot, retest status, notes, and current task constraints for this retest conversation. Takes no arguments and can read only this conversation."),
			objSchema(map[string]any{}), func(ctx context.Context, _ json.RawMessage) (actool.Result, error) {
				r, err := s.m.pg.FindingRetestForConversation(ctx, intercept.ConvIDFromContext(ctx))
				if err != nil {
					return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
				}
				if r == nil {
					return actool.Errorf(locale.Text(locale.FromContext(ctx), "This conversation has no retest record; start a retest from finding details")), nil
				}
				var constraints []db.Constraint
				f, err := s.m.pg.GetFinding(r.FindingID)
				if err != nil {
					return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
				}
				if f != nil && f.TaskID != nil {
					if task, ok := s.m.Task(strconv.FormatInt(*f.TaskID, 10)); ok {
						constraints, err = task.Store.ListConstraints()
						if err != nil {
							return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
						}
					}
				}
				return jsonResult(map[string]any{"retest": r, "current_constraints": constraints})
			}),
		wrTool("record_finding_retest_result", locale.Text(lang, "Save the sole conclusion for the current retest conversation, preserving the original finding evidence and report. When the conversation completes successfully with verdict fixed, the system marks the finding fixed; other verdicts preserve its status. Supply evidence from checks actually performed and explain blockers when the result cannot be confirmed."),
			objSchema(map[string]any{
				"verdict":  map[string]any{"type": "string", "enum": []string{"reproduced", "fixed", "inconclusive"}},
				"summary":  strParam(locale.Text(lang, "Summary of this retest conclusion")),
				"evidence": strParam(locale.Text(lang, "Markdown: steps actually performed, observations, controls, and basis for the conclusion; if inconclusive, list completed checks and blockers")),
			}, "verdict", "summary", "evidence"), func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
				var a struct {
					Verdict  string `json:"verdict"`
					Summary  string `json:"summary"`
					Evidence string `json:"evidence"`
				}
				if err := json.Unmarshal(in, &a); err != nil {
					return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
				}
				if err := s.m.pg.RecordFindingRetestResult(ctx, intercept.ConvIDFromContext(ctx), a.Verdict, a.Summary, a.Evidence); err != nil {
					return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
				}
				return jsonResult(map[string]any{"saved": true, "verdict": a.Verdict})
			}),
	}
}

// Seed the editable agent atomically, without an automatic discovery trigger.
// Once seeded, user edits/deletion survive restarts; a pre-existing key is kept.
func (s *Server) seedFindingRetester() error {
	for _, t := range s.findingRetestTools() {
		schema, _ := json.Marshal(t.InputSchema())
		bindings, _ := json.Marshal([]string{db.FindingRetestAgentKey})
		if err := s.m.pg.SeedTool(t.Name(), t.Description(), schema, bindings); err != nil {
			return err
		}
	}
	const flag = "finding_retester_seed_v1"
	tx, err := s.m.pg.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Lock this migration, including concurrent server initialization.
	if _, err = tx.Exec(`SELECT pg_advisory_xact_lock(7337741010)`); err != nil {
		return err
	}
	var done string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key=$1`, flag).Scan(&done)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if done == "true" {
		return nil
	}
	var id int64
	err = tx.QueryRow(`INSERT INTO agents(key,name,description,role,builtin,enabled)
	VALUES ($1,$2,$3,'assistant',false,true)
	ON CONFLICT (key) DO NOTHING RETURNING id`, db.FindingRetestAgentKey, locale.Text(locale.En, "Finding retest"), locale.Text(locale.En, "Started manually from finding details; reads original evidence and saves an independent retest conclusion.")).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if id > 0 {
		var pid int64
		if err = tx.QueryRow(`INSERT INTO agent_prompts(agent_id,version,template_text,note,updated_by)
		VALUES ($1,1,$2,$3,'system') RETURNING id`, id, agent.RetesterDefaultPrompt, locale.Text(locale.En, "Built-in default")).Scan(&pid); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE agents SET current_prompt_id=$1 WHERE id=$2`, pid, id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES ($1,'true') ON CONFLICT(key) DO UPDATE SET value='true'`, flag); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) finishRetest(id int64, status, reason string, langs ...locale.Lang) {
	if err := s.m.pg.FinishFindingRetest(id, status, reason); err != nil {
		// Surface failure to the conversation runner rather than inventing a result.
		log.Printf(locale.Text(locale.First(langs), "[retest %d] finish: %v"), id, locale.ErrorMessage(locale.First(langs), err))
	}
}
