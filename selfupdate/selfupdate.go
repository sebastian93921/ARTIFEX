// Package selfupdate implements ARTEX's one-click update: download a GitHub
// release executable, verify and stage it, then atomically replace it on startup.
//
// Responsibilities (see start.sh / start.bat):
//
// Startup scripts: supervise the process and decide whether to restart from its exit code.
// This package: download, SHA256 verification, smoke testing, replacement, and rollback.
//
// Replacement lives in Go because shell and batch would need separate checksum
// and smoke-test implementations (sha256sum/shasum/certutil) at the most critical
// safety boundary. An unexecutable update would otherwise require manual recovery.
//
// A complete upgrade involves three process starts:
//
// 1. Old server receives /api/update/apply, downloads/verifies, stages artex.new, exits 75.
// 2. Supervisor restarts old server; Bootstrap verifies/smoke-tests/replaces the staged file, exits 75.
// 3. Supervisor starts the new version; Bootstrap records an attempt and clears the marker after stability.
//
// Failure preserves/restores the old version: step 2 discards invalid staging;
// three consecutive unstable starts in step 3 automatically restore artex.old.
package selfupdate

import (
	"encoding/json"
	"github.com/Autumn-27/artex/locale"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExitRestart asks the supervisor to relaunch immediately (EX_TEMPFAIL), without
// crash backoff. Exit 0 is a normal stop; other exit codes indicate crashes.
const ExitRestart = 75

// maxAttempts limits post-replacement starts. Each new-version start increments
// the count; surviving SettleDelay clears the marker, while repeated crashes roll back.
const maxAttempts = 3

// Paths contains all upgrade files, rooted in the executable's directory.
// Do not use CWD: a service may run from / or any other directory, which would
// put staging files elsewhere and break replacement.
type Paths struct {
	Dir     string // Executable directory.
	Current string // Current executable: artex / artex.exe.
	New     string // Staged version: artex.new / artex.new.exe.
	Sum     string // Staged SHA256 (hex): artex.new.sha256 / artex.new.exe.sha256.
	Old     string // Previous executable: artex.old / artex.old.exe.
	Marker  string // Upgrade state marker: artex.upgrade.json.
}

// ResolvePaths derives all upgrade paths from the current executable.
//
// Windows .new/.old files must retain .exe for smoke testing and execution.
// Strip the extension before adding the suffix to keep platform naming symmetric.
func ResolvePaths() (Paths, error) {
	exe, err := os.Executable()
	if err != nil {
		return Paths{}, locale.Errorf("Locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	name := filepath.Base(exe)
	ext := filepath.Ext(name) // .exe on Windows, usually empty on Unix.
	stem := strings.TrimSuffix(name, ext)

	join := func(suffix string) string { return filepath.Join(dir, stem+suffix+ext) }
	return Paths{
		Dir:     dir,
		Current: exe,
		New:     join(".new"),
		Sum:     join(".new") + ".sha256",
		Old:     join(".old"),
		Marker:  filepath.Join(dir, stem+".upgrade.json"),
	}, nil
}

// marker tracks replacement progress and triggers rollback if the new version cannot start.
type marker struct {
	From     string `json:"from"`     // Version before the upgrade.
	To       string `json:"to"`       // Target version.
	Attempts int    `json:"attempts"` // Startup attempts since replacement.
	StagedAt int64  `json:"staged_at"`
}

func readMarker(path string) (marker, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return marker{}, false
	}
	var m marker
	if json.Unmarshal(b, &m) != nil {
		return marker{}, false
	}
	return m, true
}

func writeMarker(path string, m marker) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// cleanStaged removes staging files after replacement, failed verification, or
// cancellation, so a leftover artex.new is not retried on the next startup.
func cleanStaged(p Paths) {
	_ = os.Remove(p.New)
	_ = os.Remove(p.Sum)
}

// CompareVersions returns -1/0/1 for a<b, a==b, or a>b.
// ok=false means at least one version is not comparable, such as dev or
// git-describe output like 0.3.7-2-gabc1234-dirty. Disable one-click updates then,
// rather than overwriting a development build and uncommitted changes with a release.
func CompareVersions(a, b string) (int, bool) {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := range 3 {
		if av[i] != bv[i] {
			if av[i] < bv[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// parseVersion parses v0.3.7 or 0.3.7 into a three-part integer version.
//
// Accept only a clean three-part version. Non-tag builds use git describe,
// such as 0.3.7-2-gabc1234, which must be incomparable rather than treated as
// 0.3.7; otherwise development builds could be misclassified or overwritten.
func parseVersion(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return [3]int{}, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// InDocker reports container execution. Replacement modifies the writable layer;
// recreating the container with docker compose restores the image's version.
// This is expected when pulling a new image, but the UI must explain it accurately.
func InDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	b, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd")
}
