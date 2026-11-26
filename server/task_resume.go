package server

import (
	"net/http"
)

// resumeTerminalTask re-opens a terminal timeout/failed scan so unfinished work
// continues: orphaned running intents return to open, completed facts, findings
// and evidence are preserved, and the shared admission path stamps a fresh
// timeout budget. Cancelled, archived or non-terminal tasks are rejected.
func (s *Server) resumeTerminalTask(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	if s.engine.IsDeleting(t.ID) {
		writeErr(w, 409, "Task is being deleted and cannot be resumed")
		return
	}
	var cancelled bool
	if err := s.m.pg.QueryRow(`SELECT cancel_requested_at IS NOT NULL FROM tasks WHERE id=$1`, t.ID).Scan(&cancelled); err != nil {
		writeError(w, 500, err)
		return
	}
	if cancelled {
		writeErr(w, 409, "Cancelled tasks cannot be resumed")
		return
	}
	var archived bool
	if err := s.m.pg.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_archives WHERE task_id=$1 AND state IN ('archive_queued','archiving','ready','delete_queued','deleting'))`, t.ID).Scan(&archived); err != nil {
		writeError(w, 500, err)
		return
	}
	if archived {
		writeErr(w, 409, "Archived tasks cannot be resumed; restore the archive first")
		return
	}
	lifecycle := t.lifecycleSnapshot()
	if lifecycle.Status != "timeout" && lifecycle.Status != "failed" {
		writeErr(w, 409, "Only timeout or failed tasks can be resumed")
		return
	}
	reopened, err := t.Store.ResetRunningIntents()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	queued, err := s.admitTask(t, "resume")
	if err != nil {
		writeError(w, 409, err)
		return
	}
	status := "running"
	if queued {
		status = "queued"
	}
	writeJSON(w, 200, map[string]any{"id": t.ID, "queued": queued, "status": status, "reopened": reopened})
}
