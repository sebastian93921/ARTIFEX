package selfupdate

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"io"
	"net/http"
	"os"
	"path"
	"runtime"
	"strings"
	"time"
)

// sumsAsset is the release.yml checksum manifest covering all release ZIP files.
const sumsAsset = "SHA256SUMS"

// maxBinarySize prevents malformed ZIP files from exhausting disk space during extraction.
const maxBinarySize = 512 << 20 // 512 MiB

// Phase is the upgrade stage, used directly in the SSE phase field.
type Phase string

const (
	PhaseIdle     Phase = "idle"
	PhaseDownload Phase = "downloading"
	PhaseVerify   Phase = "verifying"
	PhaseExtract  Phase = "extracting"
	PhaseStaged   Phase = "staged"
	PhaseFailed   Phase = "failed"
)

// Progress reports to the caller/UI. pct is meaningful only during download
// (0-100); other phases use -1.
type Progress func(ph Phase, pct int, msg string)

// MessageProgress carries a localizable progress event without modifying raw arguments.
type MessageProgress func(ph Phase, pct int, msg locale.Message)

// Stage downloads and verifies the platform package, then stages artex.new.
//
// Use the full ZIP rather than a bare executable because SHA256SUMS covers ZIPs.
// This matches the release pipeline and its existing archives. The package also
// includes skills for potential future synchronization, costing only a few hundred KB.
//
// Returning successfully means staging is complete; the caller then shuts down with ExitRestart.
func Stage(ctx context.Context, c *http.Client, rel *Release, currentVersion string, prog Progress) error {
	return StageMessages(ctx, c, rel, currentVersion, func(ph Phase, pct int, msg locale.Message) {
		if prog != nil {
			prog(ph, pct, msg.In(locale.FromContext(ctx)))
		}
	})
}

// StageMessages retains built-in progress templates for per-subscriber rendering.
// Stage remains compatible with existing callers that consume plain text.
func StageMessages(ctx context.Context, c *http.Client, rel *Release, currentVersion string, prog MessageProgress) error {
	if prog == nil {
		prog = func(Phase, int, locale.Message) {}
	}
	p, err := ResolvePaths()
	if err != nil {
		return err
	}
	if err := checkWritable(p.Dir); err != nil {
		return err
	}

	name := AssetName(rel.TagName, runtime.GOOS, runtime.GOARCH)
	asset, ok := rel.FindAsset(name)
	if !ok {
		return locale.Errorf("This release has no package for %s/%s (missing %s)", runtime.GOOS, runtime.GOARCH, name)
	}

	prog(PhaseDownload, 0, locale.M("Fetching checksum manifest…"))
	sums, err := fetchSums(ctx, c, rel)
	if err != nil {
		return err
	}
	want, ok := sums[name]
	if !ok {
		return locale.Errorf("%s does not list %s; refusing to install an unverified executable", sumsAsset, name)
	}

	// Keep all temporary files in the target directory so the final rename is atomic
	// on one filesystem; cross-device rename fails and /tmp may be a separate mount.
	zipPath := p.New + ".zip.part"
	binPath := p.New + ".part"
	defer func() {
		_ = os.Remove(zipPath)
		_ = os.Remove(binPath)
	}()

	prog(PhaseDownload, 0, locale.M("Downloading %s (%s)…", name, humanSize(asset.Size)))
	got, err := download(ctx, c, asset, zipPath, prog)
	if err != nil {
		return err
	}

	prog(PhaseVerify, -1, locale.M("Verifying SHA256…"))
	if !strings.EqualFold(got, want) {
		return locale.Errorf("SHA256 mismatch: expected %s, got %s (download corrupted or tampered with)", short(want), short(got))
	}

	prog(PhaseExtract, -1, locale.M("Extracting and running smoke test…"))
	if err := extractBinary(zipPath, binPath); err != nil {
		return err
	}
	if err := smokeTest(binPath); err != nil {
		return locale.Errorf("New version cannot run on this system: %w", err)
	}

	// Store the staged executable's checksum separately for verification at restart,
	// catching modification or corruption between staging and replacement.
	binSum, err := fileSHA256(binPath)
	if err != nil {
		return locale.Errorf("Compute new executable checksum: %w", err)
	}
	if err := os.WriteFile(p.Sum, []byte(binSum), 0o644); err != nil {
		return locale.Errorf("Write checksum: %w", err)
	}
	if err := os.Rename(binPath, p.New); err != nil {
		_ = os.Remove(p.Sum)
		return locale.Errorf("Stage new version: %w", err)
	}

	if err := writeMarker(p.Marker, marker{
		From:     currentVersion,
		To:       strings.TrimPrefix(rel.TagName, "v"),
		StagedAt: time.Now().Unix(),
	}); err != nil {
		// The marker only controls automatic rollback; staging is ready, so do not abort the update.
		prog(PhaseStaged, -1, locale.M("Warning: could not write upgrade marker; this upgrade has no automatic rollback protection"))
	}

	prog(PhaseStaged, 100, locale.M("New version ready; restarting…"))
	return nil
}

// fetchSums downloads/parses SHA256SUMS into filename-to-hex-digest entries.
func fetchSums(ctx context.Context, c *http.Client, rel *Release) (map[string]string, error) {
	asset, ok := rel.FindAsset(sumsAsset)
	if !ok {
		return nil, locale.Errorf("Release is missing %s; integrity cannot be verified, refusing upgrade", sumsAsset)
	}
	body, err := get(ctx, c, asset.URL)
	if err != nil {
		return nil, locale.Errorf("Download %s: %w", sumsAsset, err)
	}
	defer body.Close()

	raw, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return nil, locale.Errorf("Read %s: %w", sumsAsset, err)
	}
	out := parseSums(string(raw))
	if len(out) == 0 {
		return nil, locale.Errorf("%s is empty or has an unrecognized format", sumsAsset)
	}
	return out, nil
}

// parseSums parses a sha256sum-style manifest into filename-to-hex-digest entries.
//
// Accept an entry only if its first field is a 64-character hexadecimal digest.
// Merely requiring two fields could accept arbitrary two-word prose as a checksum,
// polluting the map and potentially matching a real asset with an invalid digest.
func parseSums(raw string) map[string]string {
	out := map[string]string{}
	for line := range strings.Lines(raw) {
		// Format: <sha256>  <filename>. sha256sum uses two spaces; shasum binary mode
		// prefixes the filename with *.
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 || !isHexSHA256(fields[0]) {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == "" {
			continue
		}
		out[name] = strings.ToLower(fields[0])
	}
	return out
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// download writes an asset to dst, computing SHA256 and reporting Content-Length progress.
func download(ctx context.Context, c *http.Client, a Asset, dst string, prog MessageProgress) (string, error) {
	body, err := get(ctx, c, a.URL)
	if err != nil {
		return "", locale.Errorf("Download %s: %w", a.Name, err)
	}
	defer body.Close()

	f, err := os.Create(dst)
	if err != nil {
		return "", locale.Errorf("Create temporary file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	pw := &progressWriter{total: a.Size, prog: prog, name: a.Name, last: time.Now()}
	if _, err := io.Copy(io.MultiWriter(f, h, pw), body); err != nil {
		return "", locale.Errorf("Download interrupted: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", locale.Errorf("Write to disk failed: %w", err)
	}
	if a.Size > 0 && pw.written != a.Size {
		return "", locale.Errorf("Incomplete download: expected %d bytes, got %d bytes", a.Size, pw.written)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// get performs an allowlist-restricted GET and returns the response body.
func get(ctx context.Context, c *http.Client, rawURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if err := checkURL(req.URL); err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "artex-selfupdate")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// extractBinary extracts the ARTEX executable from a release package.
//
// The layout is artex-<version>-<os>-<arch>/artex. Match by basename
// rather than duplicating the version-dependent path, reducing naming fragility.
func extractBinary(zipPath, dst string) error {
	want := "artex"
	if runtime.GOOS == "windows" {
		want = "artex.exe"
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return locale.Errorf("Open release package: %w", err)
	}
	defer zr.Close()

	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() || !strings.EqualFold(path.Base(entry.Name), want) {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return locale.Errorf("Read %s: %w", entry.Name, err)
		}
		defer rc.Close()

		f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return locale.Errorf("Write new executable: %w", err)
		}
		defer f.Close()

		n, err := io.Copy(f, io.LimitReader(rc, maxBinarySize+1))
		if err != nil {
			return locale.Errorf("Extract %s: %w", entry.Name, err)
		}
		if n > maxBinarySize {
			return locale.Errorf("Executable in release package exceeds %s; refusing extraction", humanSize(maxBinarySize))
		}
		if n == 0 {
			return locale.Errorf("%s in the release package is empty", want)
		}
		return f.Sync()
	}
	return locale.Errorf("Release package does not contain %s", want)
}

// checkWritable verifies directory permissions before downloading. Otherwise a
// non-root/system-directory installation may fail only after downloading the entire package.
func checkWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".artex-update-probe-*")
	if err != nil {
		return locale.Errorf("Application directory %s is not writable; automatic update unavailable (check permissions or update manually): %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

// progressWriter counts bytes and throttles updates instead of emitting SSE for every 32KiB chunk.
type progressWriter struct {
	total   int64
	written int64
	name    string
	prog    MessageProgress
	last    time.Time
}

func (w *progressWriter) Write(b []byte) (int, error) {
	w.written += int64(len(b))
	if time.Since(w.last) < 300*time.Millisecond {
		return len(b), nil
	}
	w.last = time.Now()
	pct := -1
	if w.total > 0 {
		pct = int(w.written * 100 / w.total)
	}
	w.prog(PhaseDownload, pct, locale.M("Downloading %s / %s", humanSize(w.written), humanSize(w.total)))
	return len(b), nil
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

func short(sum string) string {
	if len(sum) > 12 {
		return sum[:12] + "…"
	}
	return sum
}
