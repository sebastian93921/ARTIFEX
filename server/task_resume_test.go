package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sebastian93921/artifex/db"
)

func resumeTestServer(t *testing.T) (*Server, *Manager, string) {
	t.Helper()
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	td := t.TempDir()
	s := New(context.Background(), m, td, td, td)
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return s, m, token
}

func resumeRequest(t *testing.T, s *Server, token, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+id+"/resume", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func resumeCreateTaskWithIntents(t *testing.T, m *Manager, status string) (*Task, map[string]int64) {
	t.Helper()
	task, err := m.CreateTask("resume target", "unfinished scan", nil, 120, 0)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	// Pin a nonexistent profile so resume queues instead of starting real workers:
	// with a usable provider present (CI), an unguarded resume would otherwise claim
	// intents and write transcripts after the test's TempDir cleanup has begun.
	bogus := int64(999999999)
	task.LLMProfileID = &bogus
	task.ActiveLLMProfileID = &bogus
	if _, err := m.pg.Exec(`UPDATE tasks SET llm_profile_id=$1, active_llm_profile_id=$1 WHERE id=$2`, bogus, task.ID); err != nil {
		t.Fatalf("pin bogus profile: %v", err)
	}
	states := map[string]int64{}
	for _, state := range []string{"open", "running", "done", "blocked"} {
		id, err := task.Store.AddNode(db.KindIntent, map[string]any{"summary": state + " work"}, 0, "open", "system", nil)
		if err != nil {
			t.Fatalf("add %s intent: %v", state, err)
		}
		if state != "open" {
			if err := task.Store.SetIntentState(id, state); err != nil {
				t.Fatalf("set %s intent: %v", state, err)
			}
		}
		states[state] = id
	}
	if err := m.SetTaskStatus(task.ID, status); err != nil {
		t.Fatalf("force %s status: %v", status, err)
	}
	if _, err := m.pg.Exec(`UPDATE tasks SET first_run_at=now()-interval '2 hours', deadline_at=now()-interval '1 hour' WHERE id=$1`, task.ID); err != nil {
		t.Fatalf("backdate clock: %v", err)
	}
	return task, states
}

func intentState(t *testing.T, m *Manager, task *Task, id int64) string {
	t.Helper()
	var state string
	if err := m.pg.QueryRow(`SELECT state FROM exploration_nodes WHERE id=$1`, id).Scan(&state); err != nil {
		t.Fatalf("read intent state: %v", err)
	}
	return state
}

func TestResumeTerminalTaskReopensWorkAndClock(t *testing.T) {
	s, m, token := resumeTestServer(t)
	task, ids := resumeCreateTaskWithIntents(t, m, "timeout")

	rec := resumeRequest(t, s, token, task.ID)
	if rec.Code != 200 {
		t.Fatalf("resume failed: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Status   string `json:"status"`
		Reopened int64  `json:"reopened"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Reopened != 1 {
		t.Fatalf("expected exactly one orphaned running intent reopened, got %d", body.Reopened)
	}
	if got := intentState(t, m, task, ids["running"]); got != "open" {
		t.Fatalf("running intent not reopened: %s", got)
	}
	if got := intentState(t, m, task, ids["done"]); got != "done" {
		t.Fatalf("completed intent must be preserved, got %s", got)
	}
	if got := intentState(t, m, task, ids["blocked"]); got != "blocked" {
		t.Fatalf("blocked intent must be preserved, got %s", got)
	}
	var firstRun, deadline *time.Time
	if err := m.pg.QueryRow(`SELECT first_run_at, deadline_at FROM tasks WHERE id=$1`, task.ID).Scan(&firstRun, &deadline); err != nil {
		t.Fatalf("read clock: %v", err)
	}
	if firstRun != nil || deadline != nil {
		t.Fatalf("timeout resume must clear first_run_at/deadline_at for a fresh budget, got %v/%v", firstRun, deadline)
	}
	if s.engine.isSettling(task.ID) {
		t.Fatal("settling gate must be cleared after resume")
	}
	if body.Status != "running" && body.Status != "queued" {
		t.Fatalf("unexpected post-resume status %q", body.Status)
	}
}

func TestResumeTerminalTaskFailedAlsoResetsClock(t *testing.T) {
	s, m, token := resumeTestServer(t)
	task, _ := resumeCreateTaskWithIntents(t, m, "failed")
	rec := resumeRequest(t, s, token, task.ID)
	if rec.Code != 200 {
		t.Fatalf("resume failed: %d %s", rec.Code, rec.Body.String())
	}
	var firstRun, deadline *time.Time
	if err := m.pg.QueryRow(`SELECT first_run_at, deadline_at FROM tasks WHERE id=$1`, task.ID).Scan(&firstRun, &deadline); err != nil {
		t.Fatalf("read clock: %v", err)
	}
	if firstRun != nil || deadline != nil {
		t.Fatalf("failed resume must clear the stale deadline, got %v/%v", firstRun, deadline)
	}
	if s.engine.isSettling(task.ID) {
		t.Fatal("settling gate must be cleared after failed resume")
	}
}

func TestResumeTerminalTaskRejectsNonTerminal(t *testing.T) {
	s, m, token := resumeTestServer(t)
	for _, status := range []string{"done", "created", "paused"} {
		task, err := m.CreateTask("non terminal "+status, "reject", nil, 0, 0)
		if err != nil {
			t.Fatalf("create %s task: %v", status, err)
		}
		if status == "done" {
			if err := m.SetTaskStatus(task.ID, "done"); err != nil {
				t.Fatalf("mark done: %v", err)
			}
		}
		if status == "paused" {
			if _, err := m.pg.Exec(`UPDATE tasks SET paused=true WHERE id=$1`, task.ID); err != nil {
				t.Fatalf("mark paused: %v", err)
			}
		}
		if rec := resumeRequest(t, s, token, task.ID); rec.Code != 409 {
			t.Fatalf("status %s: expected 409, got %d %s", status, rec.Code, rec.Body.String())
		}
	}
}

func TestResumeTerminalTaskSecondCallConflicts(t *testing.T) {
	s, m, token := resumeTestServer(t)
	task, _ := resumeCreateTaskWithIntents(t, m, "timeout")
	if rec := resumeRequest(t, s, token, task.ID); rec.Code != 200 {
		t.Fatalf("first resume failed: %d %s", rec.Code, rec.Body.String())
	}
	if rec := resumeRequest(t, s, token, task.ID); rec.Code != 409 {
		t.Fatalf("second resume must conflict, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestResumeTerminalTaskCancelledRejected(t *testing.T) {
	s, m, token := resumeTestServer(t)
	task, _ := resumeCreateTaskWithIntents(t, m, "timeout")
	if _, err := m.pg.Exec(`UPDATE tasks SET cancel_requested_at=now() WHERE id=$1`, task.ID); err != nil {
		t.Fatalf("mark cancelled: %v", err)
	}
	if rec := resumeRequest(t, s, token, task.ID); rec.Code != 409 {
		t.Fatalf("cancelled task must be rejected, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestResumeTerminalTaskArchivedRejected(t *testing.T) {
	s, m, token := resumeTestServer(t)
	task, _ := resumeCreateTaskWithIntents(t, m, "timeout")
	if _, err := m.pg.Exec(`INSERT INTO task_archives (task_id, state) VALUES ($1, 'archive_queued')`, task.ID); err != nil {
		t.Fatalf("insert archive job: %v", err)
	}
	if rec := resumeRequest(t, s, token, task.ID); rec.Code != 409 {
		t.Fatalf("archived task must be rejected, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestResumeTerminalTaskNotFound(t *testing.T) {
	s, _, token := resumeTestServer(t)
	if rec := resumeRequest(t, s, token, "999999"); rec.Code != 404 {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
