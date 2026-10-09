package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/sebastian93921/artifex/db"
)

// Manual goal CRUD from overview management. These handlers write the same goal nodes as the agent set_goals tool,
// but are invoked directly by users. Add/edit reuses task revival (admitTask resume: terminal -> running,
// unpause, queue when required); deletion does not revive by product design. Each mutation uses
// beginTaskOperation/decInflight to avoid task-deletion races, like intent CRUD.

// listGoals returns task goals with text/vulnclass/state fields for goal-management cards.
func (s *Server) listGoals(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	goals, err := t.Store.ListByKind(db.KindGoal, 10000)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"goals": goalDTOs(goals)})
}

// addGoal manually inserts a goal under the task origin's spawns edge, records a goal-added trigger,
// wakes the planner, and revives the task to reassess completion against the new goal.
func (s *Server) addGoal(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, "The task is being deleted; goals cannot be added")
		return
	}
	defer s.engine.decInflight(t.ID)

	var body struct {
		Text      string `json:"text"`
		VulnClass string `json:"vulnclass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErr(w, 400, "Goal text must not be empty")
		return
	}
	payload := map[string]any{"text": text}
	if vc := strings.TrimSpace(body.VulnClass); vc != "" {
		payload["vulnclass"] = vc
	}
	id, err := t.Store.AddGoal(payload, "human")
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if of, _ := t.Store.OriginFactID(); of > 0 && id > 0 {
		_ = t.Store.Link(of, db.RelSpawns, id) // goal descends from the task root (origin fact)
	}
	t.NotifyGoal([]string{text}) // Record the user-added-goal trigger and wake the planner.
	s.reviveTask(t)              // Resume completed/paused tasks.
	node, _ := t.Store.GetNode(id)
	if node == nil {
		writeErr(w, 500, "Could not read the goal after creating it")
		return
	}
	writeJSON(w, 200, goalDTO(node))
}

// editGoal changes goal text/vulnclass, records the old-to-new user-edit trigger,
// wakes the planner, and revives the task to adjust direction against the new goal.
func (s *Server) editGoal(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, "The task is being deleted; goals cannot be edited")
		return
	}
	defer s.engine.decInflight(t.ID)

	gid, err := strconv.ParseInt(r.PathValue("gid"), 10, 64)
	if err != nil || gid <= 0 {
		writeErr(w, 400, "bad goal id")
		return
	}
	var body struct {
		Text      string `json:"text"`
		VulnClass string `json:"vulnclass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		writeErr(w, 400, "Goal text must not be empty")
		return
	}
	node, err := t.Store.GetNode(gid)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if node == nil || node.Kind != db.KindGoal {
		writeErr(w, 404, "Goal not found")
		return
	}
	oldText := goalDTO(node).Text
	if err := t.Store.UpdateGoalPayload(gid, text, strings.TrimSpace(body.VulnClass)); err != nil {
		writeError(w, 500, err)
		return
	}
	t.NotifyGoalEdited(oldText, text) // Record the old-to-new user-edited-goal trigger and wake the planner.
	s.reviveTask(t)                   // As with creation, revive the task to reassess the new goal.
	updated, _ := t.Store.GetNode(gid)
	if updated == nil {
		writeErr(w, 500, "Could not read the goal after updating it")
		return
	}
	writeJSON(w, 200, goalDTO(updated))
}

// deleteGoal hard-deletes a goal and cascades edges/anchors, then records the user-deleted-goal trigger
// and wakes the planner to reassess remaining goals. Deletion does not revive the task by product design.
func (s *Server) deleteGoal(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if !s.engine.beginTaskOperation(t.ID) {
		writeErr(w, 409, "The task is being deleted; goals cannot be deleted")
		return
	}
	defer s.engine.decInflight(t.ID)

	gid, err := strconv.ParseInt(r.PathValue("gid"), 10, 64)
	if err != nil || gid <= 0 {
		writeErr(w, 400, "bad goal id")
		return
	}
	node, err := t.Store.GetNode(gid)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if node == nil || node.Kind != db.KindGoal {
		writeErr(w, 404, "Goal not found")
		return
	}
	text := goalDTO(node).Text
	if err := t.Store.DeleteGoal(gid); err != nil {
		writeError(w, 500, err)
		return
	}
	t.NotifyGoalDeleted(text) // Record the user-deleted-goal trigger and wake the planner without reviving the task.
	writeJSON(w, 200, map[string]bool{"ok": true})
}
