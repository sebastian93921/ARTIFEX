package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// smokeEnv makes the smoke-test subprocess skip Bootstrap explicitly.
//
// Even without it, the subprocess executable is artex.new, so its derived
// paths have a .new prefix and cannot touch the real update files. Relying on
// that coincidence is fragile; an explicit bypass is clearer and avoids disk I/O.
const smokeEnv = "ARTEX_SELFUPDATE_SMOKE"

// Action is Bootstrap's instruction to main.
type Action int

const (
	// Continue starts the server normally.
	Continue Action = iota
	// Restart exits immediately with ExitRestart so the supervisor relaunches it.
	Restart
)

// State describes this startup's upgrade state so /api/update/check can report
// accurately whether the previous upgrade succeeded or was rolled back.
type State struct {
	Pending     bool   // Replacement has not yet been confirmed stable.
	RolledBack  bool   // An automatic rollback occurred during this startup.
	FailedStage bool   // Staged update failed verification/smoke testing and was discarded.
	Detail      string // One-sentence user-facing explanation.
}

// Bootstrap runs first in main, before opening a database or listening on ports.
//
// Three possible states:
//
// 1. artex.new exists: verify and smoke-test, then replace/restart; discard on failure.
// 2. Only a marker remains: increment the post-replacement attempt count; roll back after repeated failures.
// 3. Neither exists: start normally.
func Bootstrap() (Action, State) {
	if os.Getenv(smokeEnv) != "" {
		return Continue, State{}
	}
	p, err := ResolvePaths()
	if err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[update] Bootstrap skipped: %v"), err)
		return Continue, State{}
	}

	if _, err := os.Stat(p.New); err == nil {
		return applyStaged(p)
	}

	m, ok := readMarker(p.Marker)
	if !ok {
		return Continue, State{}
	}
	return confirmOrRollback(p, m)
}

// applyStaged verifies a staged update and replaces the executable, or discards it on failure.
//
// This is the only upgrade path that overwrites the executable, and the final gate.
// Smoke testing rejects corrupted downloads, wrong architectures, or missing libraries.
// Otherwise the supervisor could endlessly relaunch a binary that cannot even run rollback code.
func applyStaged(p Paths) (Action, State) {
	m, _ := readMarker(p.Marker)

	if err := verifyStaged(p); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[update] Staged update failed verification and was discarded; continuing with the current version: %v"), err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: locale.Text(locale.ServerDefault(), "New version failed verification and was discarded: ") + err.Error()}
	}

	if err := swap(p); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[update] Replacement failed; continuing with the current version: %v"), err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: locale.Text(locale.ServerDefault(), "Replacement failed: ") + err.Error()}
	}

	// Replacement succeeded. Retain the marker for the new version's next startup to confirm stability.
	m.Attempts = 0
	if m.StagedAt == 0 {
		m.StagedAt = time.Now().Unix()
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[update] Could not write upgrade marker (automatic rollback unavailable): %v"), err)
	}
	log.Printf(locale.Text(locale.ServerDefault(), "[update] Replaced with %s; exiting to restart (exit %d)"), orUnknown(m.To), ExitRestart)
	return Restart, State{Pending: true}
}

// confirmOrRollback counts post-replacement startups and restores the old version after the limit.
//
// Counting starts only once Go runs, covering initialization crashes caused by
// incompatible configuration, occupied ports, or migration failures. Inability
// to exec at all is caught by the pre-replacement smoke test; both checks are needed.
func confirmOrRollback(p Paths, m marker) (Action, State) {
	m.Attempts++
	if m.Attempts > maxAttempts {
		if err := rollback(p); err != nil {
			// If rollback also fails, clear the marker rather than entering an endless restart
			// loop. Attempt normal startup so the operator can at least see the cause in logs.
			log.Printf(locale.Text(locale.ServerDefault(), "[update] New version failed to start %d consecutive times; rollback also failed: %v"), maxAttempts, err)
			_ = os.Remove(p.Marker)
			return Continue, State{Detail: locale.Text(locale.ServerDefault(), "New version failed to start and rollback failed: ") + err.Error()}
		}
		log.Printf(locale.Text(locale.ServerDefault(), "[update] New version failed to start %d consecutive times; rolled back to %s and exiting to restart (exit %d)"),
			maxAttempts, orUnknown(m.From), ExitRestart)
		_ = os.Remove(p.Marker)
		return Restart, State{RolledBack: true, Detail: fmt.Sprintf(locale.Text(locale.ServerDefault(), "New version failed to start; rolled back to %s"), orUnknown(m.From))}
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[update] Could not update upgrade marker: %v"), err)
	}
	log.Printf(locale.Text(locale.ServerDefault(), "[update] Starting new version (attempt %d/%d); upgrade will be confirmed after stable operation"),
		m.Attempts, maxAttempts)
	return Continue, State{Pending: true}
}

// Settle confirms stable operation of the new version and clears the upgrade marker.
//
// main calls it after a delay following HTTP startup. Crashing before that point
// leaves the marker so subsequent attempts accumulate until rollback is triggered.
func Settle() {
	p, err := ResolvePaths()
	if err != nil {
		return
	}
	settle(p)
}

func settle(p Paths) {
	if _, ok := readMarker(p.Marker); !ok {
		return // This is not a post-upgrade startup; nothing to confirm.
	}
	if err := os.Remove(p.Marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf(locale.Text(locale.ServerDefault(), "[update] Could not clear upgrade marker: %v"), err)
		return
	}
	log.Printf(locale.Text(locale.ServerDefault(), "[update] New version is stable; upgrade complete (previous version retained as %s)"), p.Old)
}

// SettleDelay is the stable runtime required to confirm that the new version survived.
const SettleDelay = 30 * time.Second

// verifyStaged compares SHA256 and then actually runs the staged executable.
func verifyStaged(p Paths) error {
	want, err := os.ReadFile(p.Sum)
	if err != nil {
		return locale.Errorf("Read checksum: %w", err)
	}
	got, err := fileSHA256(p.New)
	if err != nil {
		return locale.Errorf("Compute checksum: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(string(want)), got) {
		return locale.NewError("SHA256 mismatch (download corrupted or tampered with)")
	}
	return smokeTest(p.New)
}

// smokeTest invokes the new executable with -h to verify it runs on this system.
// This rejects truncated downloads, wrong architectures, missing dependencies, and similar failures.
func smokeTest(bin string) error {
	if err := os.Chmod(bin, 0o755); err != nil {
		return locale.Errorf("Set executable permission: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-h")
	cmd.Env = append(os.Environ(), smokeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return locale.NewError("Smoke test timed out (new executable did not respond)")
	}
	if err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return locale.Errorf("Smoke test failed: %v: %s", err, snippet)
	}
	return nil
}

// swap replaces the current executable with the staged version.
//
// Unix and Windows permit renaming a running executable; Windows prohibits deletion
// and overwriting, not rename. No platform-specific branch or self-stop is needed.
func swap(p Paths) error {
	// Windows rename cannot overwrite a target, so remove any .old backup from the previous upgrade.
	if err := os.Remove(p.Old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return locale.Errorf("Remove previous backup %s: %w", p.Old, err)
	}
	if err := os.Rename(p.Current, p.Old); err != nil {
		return locale.Errorf("Back up current version: %w", err)
	}
	if err := os.Rename(p.New, p.Current); err != nil {
		// Replacement failed after moving the current version; restore it so the next startup has an executable.
		if rerr := os.Rename(p.Old, p.Current); rerr != nil {
			return locale.Errorf("Installing the new version failed (%v), and restoring the current version failed: %w", err, rerr)
		}
		return locale.Errorf("Install new version: %w", err)
	}
	_ = os.Remove(p.Sum)
	return nil
}

// rollback restores the previous version backed up by swap.
func rollback(p Paths) error {
	if _, err := os.Stat(p.Old); err != nil {
		return locale.Errorf("No rollback backup %s: %w", p.Old, err)
	}
	// Retain the failed new version as .failed for investigation instead of deleting it.
	failed := p.Current + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(p.Current, failed); err != nil {
		return locale.Errorf("Move failed version aside: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		return locale.Errorf("Restore previous version: %w", err)
	}
	return nil
}

// Rollback implements /api/update/rollback, explicitly restoring the previous version.
// It only swaps files; the caller exits with ExitRestart and the supervisor restarts it.
func Rollback() error {
	p, err := ResolvePaths()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.Old); err != nil {
		return errors.New(locale.Text(locale.ServerDefault(), "No previous version available for rollback (") + p.Old + locale.Text(locale.ServerDefault(), " does not exist)"))
	}
	cleanStaged(p)
	if err := smokeTest(p.Old); err != nil {
		return locale.Errorf("Previous version cannot run; refusing rollback: %w", err)
	}
	// Exchange current and backup so the rollback can itself be reversed.
	tmp := p.Current + ".swap"
	_ = os.Remove(tmp)
	if err := os.Rename(p.Current, tmp); err != nil {
		return locale.Errorf("Move current version aside: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		_ = os.Rename(tmp, p.Current)
		return locale.Errorf("Install previous version: %w", err)
	}
	if err := os.Rename(tmp, p.Old); err != nil {
		log.Printf(locale.Text(locale.ServerDefault(), "[update] Backup cleanup after rollback failed (operation unaffected): %v"), err)
	}
	_ = os.Remove(p.Marker)
	return nil
}

// HasBackup reports whether a previous version exists, controlling the rollback button.
func HasBackup() bool {
	p, err := ResolvePaths()
	if err != nil {
		return false
	}
	_, err = os.Stat(p.Old)
	return err == nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return locale.Text(locale.ServerDefault(), "Unknown version")
	}
	return s
}
