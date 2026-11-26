package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sebastian93921/artifex/agent"
	pgdb "github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/traffic"
)

const taskArchivePollInterval = 2 * time.Second

type archiveBatchRequest struct {
	TaskIDs    []string `json:"task_ids"`
	ArchiveIDs []int64  `json:"archive_ids"`
}

type archiveBatchItem struct {
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Queued  bool   `json:"queued"`
	Error   string `json:"error,omitempty"`
	Archive int64  `json:"archive_id,omitempty"`
}

func (s *Server) startTaskArchiveWorker() {
	if s.m == nil || s.m.pg == nil {
		return
	}
	// Recovery renames staged files and resets interrupted state. Never run it
	// while a second server is executing an archive job against the same DB.
	locked, err := s.m.pg.WithTaskArchiveWorkerLock(s.ctx, func() error {
		root, err := filepath.Abs(s.m.dir)
		if err != nil {
			return err
		}
		for _, parent := range []string{taskArchiveRoot(root), filepath.Join(taskArchiveRoot(root), ".staging"), filepath.Join(taskArchiveRoot(root), ".restore")} {
			if err := validateTaskManagedPath(root, parent); err != nil {
				return err
			}
		}
		if err := s.m.pg.RecoverTaskArchiveJobs(); err != nil {
			return err
		}
		return errors.Join(recoverTaskArchiveStages(s.m.dir, s.m.pg),
			recoverTaskArchiveRestoreStages(s.m.dir, s.m.pg), recoverTaskArchiveDeletePackages(s.m.dir, s.m.pg))
	})
	if err != nil || !locked {
		log.Printf("[task-archive] worker and retention disabled: startup recovery incomplete or another process owns archive worker (lock=%v): %v", locked, err)
		return
	}
	s.archiveWG.Add(1)
	go func() {
		defer s.archiveWG.Done()
		ticker := time.NewTicker(taskArchivePollInterval)
		defer ticker.Stop()
		for {
			if err := s.runOneTaskArchiveJob(); err != nil {
				log.Printf(locale.Text(locale.ServerDefault(), "[task-archive] worker: %v"), err)
			}
			select {
			case <-s.ctx.Done():
				return
			case <-s.archiveWake:
			case <-ticker.C:
			}
		}
	}()
	s.notifyTaskArchiveWorker()
	// Retention starts only after archive recovery has succeeded. This native
	// scheduler is opt-in and never runs a destructive sweep during startup.
	s.startTaskRetentionScheduler()
}

func (s *Server) notifyTaskArchiveWorker() {
	if s == nil || s.archiveWake == nil {
		return
	}
	select {
	case s.archiveWake <- struct{}{}:
	default:
	}
}

func (s *Server) runOneTaskArchiveJob() error {
	_, err := s.m.pg.WithTaskArchiveWorkerLock(s.ctx, s.runClaimedTaskArchiveJob)
	return err
}

func (s *Server) runClaimedTaskArchiveJob() error {
	job, err := s.m.pg.ClaimTaskArchiveJob(s.ctx)
	if err != nil || job == nil {
		return err
	}
	var runErr error
	switch job.State {
	case pgdb.Archiving:
		runErr = s.archiveTask(job)
	case pgdb.Restoring:
		runErr = s.restoreTaskArchive(job)
	case pgdb.Deleting:
		runErr = s.deleteTaskArchive(job)
	default:
		runErr = locale.Errorf("unknown claimed archive state %q", job.State)
	}
	if runErr != nil {
		if failErr := s.m.pg.FailTaskArchiveJob(job.ID, job.State, errors.New(locale.ErrorMessage(taskLanguage(s.m.pg, strconv.FormatInt(job.TaskID, 10)), runErr))); failErr != nil {
			return errors.Join(runErr, failErr)
		}
		return locale.Errorf("archive job %d (%s): %w", job.ID, job.State, runErr)
	}
	// Drain another queued item without waiting for the periodic poll.
	s.notifyTaskArchiveWorker()
	return nil
}

func (s *Server) archiveTask(job *pgdb.TaskArchive) (runErr error) {
	taskID := strconv.FormatInt(job.TaskID, 10)
	if !s.beginTaskDelete(taskID) {
		return locale.NewError("Another archive or deletion operation is already running for this task")
	}
	committed := false
	defer func() {
		if !committed {
			s.abortTaskDelete(taskID)
		}
	}()
	s.cancelTaskChat(taskID, agent.AbortTaskDeleted)
	drainCtx, cancel := context.WithTimeout(s.ctx, taskDeleteDrainTimeout)
	defer cancel()
	if err := s.waitTaskQuiescent(drainCtx, taskID); err != nil {
		return locale.NewError("Agents are still running for this task; pause it before retrying the archive")
	}
	if err := s.drainTaskSideQuestions(drainCtx, taskID); err != nil {
		return err
	}
	task, err := s.m.pg.GetTask(job.TaskID)
	if err != nil {
		return err
	}
	if task == nil {
		return pgdb.ErrTaskArchiveNotFound
	}
	root, err := filepath.Abs(s.m.dir)
	if err != nil {
		return err
	}
	f, err := openTaskArchiveFS(root)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, path := range []string{filepath.Join(root, "tasks"), filepath.Join(root, "transcripts"), taskArchiveRoot(root), filepath.Join(taskArchiveRoot(root), ".staging", strconv.FormatInt(job.ID, 10))} {
		if err := validateTaskManagedPath(root, path); err != nil {
			return err
		}
	}
	if err := validateTaskManagedPath(root, filepath.Join(root, "tasks", taskID)); err != nil {
		return err
	}
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "stage_files", 10)
	fileStage, err := stageTaskArchiveFiles(s.m.dir, job.ID, taskID, task.ExplorationID)
	if err != nil {
		return err
	}
	defer func() {
		if !committed {
			runErr = errors.Join(runErr, fileStage.rollback())
		}
	}()
	streamPath := filepath.Join(fileStage.payload, filepath.FromSlash(pgdb.TaskArchiveLLMRecordsPath))
	if err := f.MkdirAll(filepath.Dir(streamPath), archiveDirMode); err != nil {
		return err
	}
	streamFile, err := f.OpenFile(streamPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, archiveFileMode)
	if err != nil {
		return err
	}
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "snapshot_database", 20)
	snapshot, snapshotErr := s.m.pg.SnapshotTaskArchiveWithLLMRecords(job.TaskID, streamFile)
	var syncErr error
	if snapshotErr == nil {
		syncErr = streamFile.Sync()
	}
	closeErr := streamFile.Close()
	if err := errors.Join(snapshotErr, syncErr, closeErr); err != nil {
		return err
	}
	if snapshot.ExplorationID != task.ExplorationID {
		return locale.NewError("The task exploration record changed while taking the archive snapshot")
	}
	if s.m.traffic != nil && len(snapshot.Hosts) > 0 {
		_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "snapshot_traffic", 38)
		trafficDir := filepath.Join(fileStage.payload, "traffic")
		if err := f.MkdirAll(trafficDir, archiveDirMode); err != nil {
			return err
		}
		trafficOut, err := f.OpenRoot(trafficDir)
		if err != nil {
			return err
		}
		defer trafficOut.Close()
		trafficSource, err := f.OpenRoot(filepath.Join(root, "traffic"))
		if err != nil {
			return err
		}
		defer trafficSource.Close()
		trafficCount, err := s.m.traffic.ExportHostsAt(snapshot.Hosts, trafficSource, trafficOut)
		if err != nil {
			return err
		}
		snapshot.DataCounts["traffic"] = trafficCount
	}
	archivePath := taskArchivePath(s.m.dir, job.ID, taskID)
	evidenceSnapshots, err := pgdb.ArchiveEvidenceSnapshots(snapshot)
	if err != nil {
		return err
	}
	var evidenceSource, evidenceOut *os.Root
	if len(evidenceSnapshots) > 0 {
		evidenceDir := filepath.Join(fileStage.payload, "evidence")
		if err := f.MkdirAll(evidenceDir, archiveDirMode); err != nil {
			return err
		}
		evidenceOut, err = f.OpenRoot(evidenceDir)
		if err != nil {
			return err
		}
		defer evidenceOut.Close()
		evidenceSource, err = f.OpenRoot(filepath.Join(root, "evidence"))
		if err != nil {
			return err
		}
		defer evidenceSource.Close()
	}
	if err = s.evidenceStore().CopySnapshotsAt(s.ctx, evidenceSnapshots, evidenceSource, evidenceOut); err != nil {
		return err
	}
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "compress", 50)
	originalSize, compressedSize, checksum, err := writeTaskArchivePackage(s.m.dir, archivePath, fileStage.payload, snapshot)
	if err != nil {
		return err
	}
	removePackage := true
	defer func() {
		if removePackage {
			_ = f.Remove(archivePath)
		}
	}()
	var trafficStage *traffic.HostDeleteStage
	if s.m.traffic != nil && len(snapshot.ExclusiveHosts) > 0 {
		_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "compact_traffic", 68)
		trafficStage, err = s.m.traffic.StageDeleteHostsExactForArchive(snapshot.ExclusiveHosts, job.ID, job.TaskID)
		if err != nil {
			return err
		}
		defer func() {
			if !committed {
				runErr = errors.Join(runErr, trafficStage.Rollback())
			}
		}()
	}
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "compact_database", 78)
	if err := s.m.pg.CompleteTaskArchive(job.ID, snapshot, archivePath, checksum, originalSize, compressedSize); err != nil {
		return err
	}
	committed = true
	removePackage = false
	cleanupComplete := true
	if trafficStage != nil {
		if err := trafficStage.Commit(); err != nil {
			cleanupComplete = false
			warning := locale.Text(taskLanguage(s.m.pg, strconv.FormatInt(job.TaskID, 10)), "Archive complete, but exclusive traffic hot-storage cleanup failed (the archive remains restorable): ") + err.Error()
			log.Printf(locale.Text(locale.ServerDefault(), "[task-archive] task %s: %s"), taskID, warning)
			_ = s.m.pg.AppendTaskArchiveWarning(job.ID, warning)
		}
	}
	if err := fileStage.commit(); err != nil {
		cleanupComplete = false
		warning := locale.Text(taskLanguage(s.m.pg, strconv.FormatInt(job.TaskID, 10)), "Archive complete, but staging-directory cleanup failed: ") + err.Error()
		log.Printf(locale.Text(locale.ServerDefault(), "[task-archive] task %s: %s"), taskID, warning)
		_ = s.m.pg.AppendTaskArchiveWarning(job.ID, warning)
	}
	s.engine.StopTask(taskID)
	s.taskAgentMu.Lock()
	delete(s.taskAgents, taskID)
	s.taskAgentMu.Unlock()
	s.m.forgetTask(taskID, job.TaskID)
	if cleanupComplete {
		if err := s.m.pg.ConfirmTaskArchiveCleanup(job.ID); err != nil {
			// Preserve the ready package; do not relabel a committed archive as a
			// hot archive failure. Automatic deletion stays blocked until reviewed.
			log.Printf("[task-archive] archive=%d cleanup confirmation failed; retention deletion blocked: %v", job.ID, err)
		}
	}
	return nil
}

func (s *Server) restoreTaskArchive(job *pgdb.TaskArchive) (runErr error) {
	if err := validateArchivePath(s.m.dir, job.ArchivePath); err != nil {
		return err
	}
	f, err := openTaskArchiveFS(s.m.dir)
	if err != nil {
		return err
	}
	defer f.Close()
	restoreParent := filepath.Join(taskArchiveRoot(f.dir), ".restore")
	if err := f.check(restoreParent); err != nil {
		return err
	}
	// A prior attempt may have committed PostgreSQL and files, then failed only
	// while consuming the package. Finish that cleanup without replaying rows.
	if restored, err := s.m.pg.IsTaskArchiveRestored(job.ID); err != nil {
		return err
	} else if restored {
		if err := f.Remove(job.ArchivePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := s.m.pg.CompleteTaskArchiveRestore(job.ID); err != nil {
			return err
		}
		s.loadRestoredTask(job.TaskID)
		return nil
	}
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "verify_package", 10)
	if err := f.MkdirAll(restoreParent, archiveDirMode); err != nil {
		return err
	}
	extracted, err := f.MkdirTemp(restoreParent, fmt.Sprintf("%d-", job.ID))
	if err != nil {
		return err
	}
	defer func() { _ = f.RemoveAll(extracted) }()
	if err := f.check(extracted); err != nil {
		return err
	}
	if err := extractTaskArchivePackage(s.m.dir, job.ArchivePath, job.SHA256, extracted); err != nil {
		return err
	}
	manifestRaw, err := f.ReadFile(filepath.Join(extracted, "manifest.json"))
	if err != nil {
		return err
	}
	var snapshot pgdb.TaskArchiveSnapshot
	if err := json.Unmarshal(manifestRaw, &snapshot); err != nil {
		return err
	}
	if snapshot.TaskID != job.TaskID || !pgdb.IsTaskArchiveFormatSupported(snapshot.FormatVersion) {
		return pgdb.ErrTaskArchiveFormatMismatch
	}
	evidenceSnapshots, err := pgdb.ArchiveEvidenceSnapshots(&snapshot)
	if err != nil {
		return err
	}
	var evidenceSource, evidenceDestination *os.Root
	if len(evidenceSnapshots) > 0 {
		evidenceSource, err = f.OpenRoot(filepath.Join(extracted, "evidence"))
		if err != nil {
			return err
		}
		defer evidenceSource.Close()
		evidenceDir := filepath.Join(f.dir, "evidence")
		if err := f.MkdirAll(evidenceDir, archiveDirMode); err != nil {
			return err
		}
		evidenceDestination, err = f.OpenRoot(evidenceDir)
		if err != nil {
			return err
		}
		defer evidenceDestination.Close()
	}
	return s.evidenceStore().WithInstalledSnapshotsAt(s.ctx, evidenceSnapshots, evidenceSource, evidenceDestination, func() error {
		return s.restoreTaskArchivePayloadAt(f, job, &snapshot, extracted)
	})
}

func (s *Server) restoreTaskArchivePayload(job *pgdb.TaskArchive, snapshot *pgdb.TaskArchiveSnapshot, extracted string) (runErr error) {
	f, err := openTaskArchiveFS(s.m.dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return s.restoreTaskArchivePayloadAt(f, job, snapshot, extracted)
}

func (s *Server) restoreTaskArchivePayloadAt(f *taskArchiveFS, job *pgdb.TaskArchive, snapshot *pgdb.TaskArchiveSnapshot, extracted string) (runErr error) {
	var err error
	var llmRecordsFile *os.File
	relative, hasStreamedLLMRecords := snapshot.StreamedTables["llm_records"]
	if len(snapshot.StreamedTables) > 0 &&
		(snapshot.FormatVersion < 2 || !hasStreamedLLMRecords || len(snapshot.StreamedTables) != 1 ||
			relative != pgdb.TaskArchiveLLMRecordsPath) {
		return pgdb.ErrTaskArchiveFormatMismatch
	}
	if hasStreamedLLMRecords {
		llmRecordsFile, err = f.OpenFile(filepath.Join(extracted, filepath.FromSlash(relative)), os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		defer func() {
			if llmRecordsFile != nil {
				_ = llmRecordsFile.Close()
			}
		}()
	}
	if s.m.traffic != nil {
		_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "restore_traffic", 35)
		trafficSource, err := f.OpenRoot(filepath.Join(extracted, "traffic"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			defer trafficSource.Close()
			trafficDestination, err := f.OpenRoot(filepath.Join(f.dir, "traffic"))
			if err != nil {
				return err
			}
			defer trafficDestination.Close()
			if _, err := s.m.traffic.ImportArchiveAt(trafficSource, trafficDestination); err != nil {
				return err
			}
		}
	}
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "restore_files", 55)
	files, err := installTaskArchiveFiles(s.m.dir, extracted, strconv.FormatInt(job.TaskID, 10), job.ID)
	if err != nil {
		return err
	}
	databaseRestored := false
	defer func() {
		if !databaseRestored {
			runErr = errors.Join(runErr, files.rollback())
		}
	}()
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "restore_database", 70)
	var warnings []string
	if llmRecordsFile != nil {
		warnings, err = s.m.pg.RestoreTaskArchiveWithLLMRecords(
			job.ID, snapshot, job.RemainingTimeoutSeconds, llmRecordsFile,
		)
		closeErr := llmRecordsFile.Close()
		llmRecordsFile = nil
		err = errors.Join(err, closeErr)
	} else {
		warnings, err = s.m.pg.RestoreTaskArchive(job.ID, snapshot, job.RemainingTimeoutSeconds)
	}
	if err != nil {
		return err
	}
	for _, warning := range warnings {
		log.Printf(locale.Text(locale.ServerDefault(), "[task-archive] restored task %d with warning: %s"), job.TaskID, warning)
	}
	if len(warnings) > 0 {
		if _, err := s.m.pg.Exploration(snapshot.ExplorationID).AppendActivity(pgdb.Activity{
			Worker: "system", Kind: "system", Summary: locale.Text(taskLanguage(s.m.pg, strconv.FormatInt(job.TaskID, 10)), "Task restored, but some associations could not be fully restored"), Detail: strings.Join(warnings, "\n"),
		}); err != nil {
			log.Printf(locale.Text(locale.ServerDefault(), "[task-archive] persist restore warnings for task %d: %v"), job.TaskID, err)
		}
	}
	databaseRestored = true
	if err := files.commit(); err != nil {
		return err
	}
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "consume_package", 95)
	if err := f.Remove(job.ArchivePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := s.m.pg.CompleteTaskArchiveRestore(job.ID); err != nil {
		return err
	}
	s.loadRestoredTask(job.TaskID)
	return nil
}

func (s *Server) loadRestoredTask(taskID int64) {
	id := strconv.FormatInt(taskID, 10)
	var restored *Task
	for _, task := range s.m.LoadExisting() {
		if task.ID == id {
			restored = task
			break
		}
	}
	if restored == nil {
		if task, ok := s.m.Task(id); ok {
			restored = task
		}
	}
	if restored == nil {
		return
	}
	lifecycle := restored.lifecycleSnapshot()
	if lifecycle.Paused {
		s.engine.Pause(id, agent.AbortPausedOnReload)
	}
	if !isTerminalStatus(lifecycle.Status) {
		s.engine.startDeadlineCoordinator(s.ctx, restored)
	}
}

func (s *Server) deleteTaskArchive(job *pgdb.TaskArchive) (runErr error) {
	taskID := strconv.FormatInt(job.TaskID, 10)
	if !s.beginTaskDelete(taskID) {
		return locale.NewError("Another archive or deletion operation is already running for this task")
	}
	committed := false
	defer func() {
		if !committed {
			s.abortTaskDelete(taskID)
		}
	}()
	s.cancelTaskChat(taskID, agent.AbortTaskDeleted)
	drainCtx, cancel := context.WithTimeout(s.ctx, taskDeleteDrainTimeout)
	defer cancel()
	if err := s.waitTaskQuiescent(drainCtx, taskID); err != nil {
		return locale.NewError("Agents are still running for this task; deletion was cancelled")
	}
	if err := s.drainTaskSideQuestions(drainCtx, taskID); err != nil {
		return err
	}
	if err := validateArchivePath(s.m.dir, job.ArchivePath); err != nil {
		return err
	}
	f, err := openTaskArchiveFS(s.m.dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if job.RetentionDeleteAt != nil {
		// Confirmed cold deletion is not authority to purge newly appeared hot
		// files. Detection must not rename a workspace even briefly: a crash
		// would hide it in an unjournalled deletion stage and permit a later purge.
		var expID int64
		if err := s.m.pg.QueryRowContext(s.ctx, `SELECT exploration_id FROM tasks WHERE id=$1`, job.TaskID).Scan(&expID); err != nil {
			return err
		}
		hot, err := hasTaskRetentionHotPayload(s.m.dir, taskID, expID)
		if err != nil {
			return err
		}
		if hot {
			return locale.NewError("Automatic archive deletion blocked: unexpected hot task files need review")
		}
	}
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "stage_package_delete", 35)
	staged, moved, err := stageTaskArchivePackageDelete(s.m.dir, job.ArchivePath, job.ID)
	if err != nil {
		return err
	}
	_ = s.m.pg.UpdateTaskArchiveProgress(job.ID, "delete_metadata", 70)
	if err := s.m.pg.DeleteTaskArchiveStub(job.ID); err != nil {
		if moved {
			_ = f.Rename(staged, job.ArchivePath)
		}
		return err
	}
	committed = true
	if moved {
		if err := f.Remove(staged); err != nil && !os.IsNotExist(err) {
			log.Printf(locale.Text(locale.ServerDefault(), "[task-archive] archived task %d metadata deleted; stale package cleanup failed: %v"), job.TaskID, err)
		}
	}
	s.engine.StopTask(taskID)
	s.taskAgentMu.Lock()
	delete(s.taskAgents, taskID)
	s.taskAgentMu.Unlock()
	s.m.forgetTask(taskID, job.TaskID)
	return nil
}

func validateArchivePath(dataDir, candidate string) error {
	if strings.TrimSpace(candidate) == "" {
		return locale.NewError("Archive package path is empty")
	}
	root, err := filepath.Abs(taskArchiveRoot(dataDir))
	if err != nil {
		return err
	}
	path, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return locale.NewError("Archive package path is outside the managed directory")
	}
	// Lexical containment alone follows symlinked ancestors during Rename/Remove.
	// The managed archive tree must not redirect into arbitrary host directories.
	dataRoot, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	if err := validateTaskManagedPath(dataRoot, path); err != nil {
		return err
	}
	return nil
}

// Lstat every ancestor below the configured data root: Rename follows ancestor
// links even though Remove does not follow a final symlink. Operator-controlled
// managed roots must not redirect archive staging into outside host directories.
func validateTaskManagedPath(dataRoot, path string) error {
	f, err := openTaskArchiveFS(dataRoot)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.check(path)
}

func (s *Server) listTaskArchives(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	result, err := s.m.pg.ListTaskArchives(r.URL.Query().Get("q"), r.URL.Query().Get("state"), page, size)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, result)
}

func (s *Server) getTaskArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "Invalid archive ID")
		return
	}
	item, err := s.m.pg.GetTaskArchive(id)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if item == nil {
		writeErr(w, 404, "Archive not found")
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) queueTaskArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := canonicalTaskID(r.PathValue("id"))
	if !ok {
		writeErr(w, 400, "Invalid task ID")
		return
	}
	numeric, _ := strconv.ParseInt(id, 10, 64)
	item, err := s.m.pg.QueueTaskArchive(numeric)
	if err != nil {
		writeArchiveError(w, err)
		return
	}
	s.notifyTaskArchiveWorker()
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) queueTaskArchiveRestore(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "Invalid archive ID")
		return
	}
	item, err := s.m.pg.QueueTaskArchiveRestore(id)
	if err != nil {
		writeArchiveError(w, err)
		return
	}
	s.notifyTaskArchiveWorker()
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) queueTaskArchiveDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, "Invalid archive ID")
		return
	}
	item, err := s.m.pg.QueueTaskArchiveDelete(id)
	if err != nil {
		writeArchiveError(w, err)
		return
	}
	s.notifyTaskArchiveWorker()
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) queueTaskArchivesBatch(w http.ResponseWriter, r *http.Request) {
	var request archiveBatchRequest
	if err := decode(w, r, &request); err != nil {
		writeError(w, 400, err)
		return
	}
	parsed := normalizeBatchTaskIDs(request.TaskIDs)
	if len(parsed) == 0 {
		writeErr(w, 400, "task_ids must not be empty")
		return
	}
	if len(parsed) > 100 {
		writeErr(w, 400, "At most 100 tasks may be processed at once")
		return
	}
	ids := make([]string, 0, len(parsed))
	items := make([]archiveBatchItem, 0, len(parsed))
	for _, item := range parsed {
		if !item.valid {
			items = append(items, archiveBatchItem{ID: item.id, Error: locale.Text(responseLanguage(w), "Invalid task ID")})
			continue
		}
		ids = append(ids, item.id)
	}
	ids = s.orderLiveTaskArchiveBatch(ids)
	for _, id := range ids {
		numeric, _ := strconv.ParseInt(id, 10, 64)
		archive, err := s.m.pg.QueueTaskArchive(numeric)
		item := archiveBatchItem{ID: id, OK: err == nil, Queued: err == nil}
		if err != nil {
			item.Error = locale.ErrorMessage(responseLanguage(w), err)
		} else {
			item.Archive = archive.ID
		}
		items = append(items, item)
	}
	s.notifyTaskArchiveWorker()
	writeJSON(w, http.StatusAccepted, map[string]any{"items": items})
}

func (s *Server) queueTaskArchiveActionBatch(w http.ResponseWriter, r *http.Request, action string) {
	var request archiveBatchRequest
	if err := decode(w, r, &request); err != nil {
		writeError(w, 400, err)
		return
	}
	ids, err := normalizeArchiveIDs(request.ArchiveIDs)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	ids = s.orderColdTaskArchiveBatch(ids, action == "restore")
	items := make([]archiveBatchItem, 0, len(ids))
	for _, id := range ids {
		var archive *pgdb.TaskArchive
		if action == "restore" {
			archive, err = s.m.pg.QueueTaskArchiveRestore(id)
		} else {
			archive, err = s.m.pg.QueueTaskArchiveDelete(id)
		}
		item := archiveBatchItem{ID: strconv.FormatInt(id, 10), Archive: id, OK: err == nil, Queued: err == nil}
		if err != nil {
			item.Error = locale.ErrorMessage(responseLanguage(w), err)
		} else if archive != nil {
			item.Archive = archive.ID
		}
		items = append(items, item)
	}
	s.notifyTaskArchiveWorker()
	writeJSON(w, http.StatusAccepted, map[string]any{"items": items})
}

func (s *Server) restoreTaskArchivesBatch(w http.ResponseWriter, r *http.Request) {
	s.queueTaskArchiveActionBatch(w, r, "restore")
}

func (s *Server) deleteTaskArchivesBatch(w http.ResponseWriter, r *http.Request) {
	s.queueTaskArchiveActionBatch(w, r, "delete")
}

func normalizeArchiveIDs(ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, locale.NewError("archive_ids must not be empty")
	}
	if len(ids) > 100 {
		return nil, locale.NewError("At most 100 archives may be processed at once")
	}
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, locale.Errorf("Invalid archive ID %d", id)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

func (s *Server) orderLiveTaskArchiveBatch(ids []string) []string {
	sources := map[string][]string{}
	for _, id := range ids {
		if task, ok := s.m.Task(id); ok {
			for _, sourceID := range task.lifecycleSnapshot().SourceTaskIDs {
				sources[id] = append(sources[id], strconv.FormatInt(sourceID, 10))
			}
		}
	}
	return topoTaskIDs(ids, sources, false)
}

func (s *Server) orderColdTaskArchiveBatch(ids []int64, restore bool) []int64 {
	taskToArchive := map[string]int64{}
	sources := map[string][]string{}
	var taskIDs []string
	for _, id := range ids {
		archive, err := s.m.pg.GetTaskArchive(id)
		if err != nil || archive == nil {
			continue
		}
		taskID := strconv.FormatInt(archive.TaskID, 10)
		taskToArchive[taskID] = id
		taskIDs = append(taskIDs, taskID)
		for _, sourceID := range archive.SourceTaskIDs {
			sources[taskID] = append(sources[taskID], strconv.FormatInt(sourceID, 10))
		}
	}
	orderedTasks := topoTaskIDs(taskIDs, sources, restore)
	ordered := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, taskID := range orderedTasks {
		id := taskToArchive[taskID]
		ordered = append(ordered, id)
		seen[id] = true
	}
	for _, id := range ids {
		if !seen[id] {
			ordered = append(ordered, id)
		}
	}
	return ordered
}

// topoTaskIDs orders source->dependent for restore and dependent->source for
// archive/delete. Cycles are impossible at creation, but stable fallback keeps a
// damaged database operable.
func topoTaskIDs(ids []string, sources map[string][]string, restore bool) []string {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	adjacency := map[string][]string{}
	indegree := map[string]int{}
	for id := range set {
		indegree[id] = 0
	}
	for dependent, sourceIDs := range sources {
		for _, source := range sourceIDs {
			if !set[dependent] || !set[source] {
				continue
			}
			from, to := dependent, source
			if restore {
				from, to = source, dependent
			}
			adjacency[from] = append(adjacency[from], to)
			indegree[to]++
		}
	}
	queue := []string{}
	for _, id := range ids {
		if indegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	ordered := make([]string, 0, len(ids))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		ordered = append(ordered, id)
		for _, next := range adjacency[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if len(ordered) != len(ids) {
		seen := map[string]bool{}
		for _, id := range ordered {
			seen[id] = true
		}
		for _, id := range ids {
			if !seen[id] {
				ordered = append(ordered, id)
			}
		}
	}
	return ordered
}

func writeArchiveError(w http.ResponseWriter, err error) {
	status := http.StatusConflict
	if errors.Is(err, pgdb.ErrTaskArchiveNotFound) {
		status = http.StatusNotFound
	} else if !errors.Is(err, pgdb.ErrTaskArchiveIneligible) &&
		!errors.Is(err, pgdb.ErrTaskArchiveQueued) &&
		!errors.Is(err, pgdb.ErrTaskArchiveDependent) &&
		!errors.Is(err, pgdb.ErrTaskArchiveState) &&
		!errors.Is(err, pgdb.ErrTaskArchiveDeleteBlocked) {
		status = http.StatusInternalServerError
	}
	writeError(w, status, err)
}
