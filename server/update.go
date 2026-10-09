package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/Autumn-27/artex/selfupdate"
)

// HTTP endpoints for one-click updates. Download, verification, and replacement
// live in selfupdate; this layer owns auth, mutual exclusion, progress, and exit signaling.
//
// The process does not restart itself. After staging it exits with ExitRestart;
// start.sh/start.bat, or the Docker entrypoint supervisor, relaunches it.

// restartCh closes once an upgrade is ready or rollback completes; main exits with ExitRestart.
var (
	restartOnce sync.Once
	restartCh   = make(chan struct{})
)

// RestartRequested closes when main should exit and let the supervisor restart it.
func RestartRequested() <-chan struct{} { return restartCh }

func requestRestart() { restartOnce.Do(func() { close(restartCh) }) }

// bootState stores this startup's Bootstrap outcome: pending upgrade, rollback,
// or discarded staging. main supplies it so the UI receives an accurate result.
var (
	bootStateMu sync.Mutex
	bootState   selfupdate.State
)

// SetBootUpdateState is called once by main at startup.
func SetBootUpdateState(st selfupdate.State) {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	bootState = st
}

func bootUpdateState() selfupdate.State {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	return bootState
}

// releaseCache caches GitHub's latest-release query.
//
// The top-bar update notice checks on each full page load. Unauthenticated GitHub
// access allows 60 requests/hour/IP; multiple tabs and reloads would quickly
// exhaust it without caching. Explicit update checks may bypass the cache with force.
type releaseCache struct {
	mu  sync.Mutex
	rel *selfupdate.Release
	err error
	at  time.Time
	// fetch is a test injection point; nil performs the real GitHub query.
	fetch func(context.Context, *http.Client) (*selfupdate.Release, error)
}

const (
	releaseTTL = 30 * time.Minute
	// Cache errors briefly so unreachable GitHub does not delay every page load;
	// keep the TTL short to recover quickly when connectivity returns.
	releaseErrTTL = 2 * time.Minute
	// Query timeout: NewClient's 30-minute timeout is for package downloads, not version checks.
	releaseTimeout = 20 * time.Second
)

var relCache = &releaseCache{}

// get returns the latest Release, avoiding network access on a cache hit.
//
// Hold the lock while fetching so concurrent callers share one query rather
// than issuing a burst of GitHub requests as multiple tabs load.
func (c *releaseCache) get(ctx context.Context, client *http.Client, force bool) (*selfupdate.Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !force {
		ttl := releaseTTL
		if c.err != nil {
			ttl = releaseErrTTL
		}
		if !c.at.IsZero() && time.Since(c.at) < ttl {
			return c.rel, c.err
		}
	}

	fetch := c.fetch
	if fetch == nil {
		fetch = selfupdate.FetchLatest
	}
	ctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()
	rel, err := fetch(ctx, client)
	// Caller cancellation (such as closing a tab) is not a GitHub failure. Do not
	// cache it, or the next visitor would receive an unrelated cancellation error.
	if err != nil && ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
		return c.rel, err
	}
	c.rel, c.err, c.at = rel, err, time.Now()
	return rel, err
}

// updateProgress is one progress event sent to the frontend.
type updateProgress struct {
	message locale.Message
	cause   error
	Phase   selfupdate.Phase `json:"phase"`
	Percent int              `json:"percent"` // Meaningful only while downloading; otherwise -1.
	Message string           `json:"message"`
	Version string           `json:"version,omitempty"`
	Error   string           `json:"error,omitempty"`
}

// updateHub retains an upgrade's progress and broadcasts it to SSE subscribers.
//
// running also provides exclusion: concurrent update/apply requests receive 409,
// preventing two goroutines from writing the same artex.new file.
type updateHub struct {
	mu      sync.Mutex
	running bool
	cur     updateProgress
	subs    map[chan updateProgress]struct{}
}

var updHub = &updateHub{
	cur:  updateProgress{Phase: selfupdate.PhaseIdle, Percent: -1},
	subs: map[chan updateProgress]struct{}{},
}

// begin claims update ownership, returning false if an update is already running.
func (h *updateHub) begin(version string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return false
	}
	h.running = true
	h.cur = updateProgress{Phase: selfupdate.PhaseDownload, Percent: 0, Message: "Preparing…", message: locale.M("Preparing…"), Version: version}
	h.fanout(h.cur)
	return true
}

// finish ends an update. A nil error means staging succeeded and restart is pending.
func (h *updateHub) finish(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	if err != nil {
		h.cur = updateProgress{Phase: selfupdate.PhaseFailed, Percent: -1, Message: "Update failed", message: locale.M("Update failed"), cause: err, Error: err.Error(), Version: h.cur.Version}
	} else {
		h.cur = updateProgress{Phase: selfupdate.PhaseStaged, Percent: 100, Message: "New version ready; restarting…", message: locale.M("New version ready; restarting…"), Version: h.cur.Version}
	}
	h.fanout(h.cur)
}

func (h *updateHub) publish(ph selfupdate.Phase, pct int, msg string) {
	h.publishMessage(ph, pct, locale.M(msg))
}

func (h *updateHub) publishMessage(ph selfupdate.Phase, pct int, msg locale.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cur = updateProgress{Phase: ph, Percent: pct, Message: msg.In(locale.En), message: msg, Version: h.cur.Version}
	h.fanout(h.cur)
}

// fanout requires h.mu. Subscriber channels are buffered; drop events when full.
// Progress is transient: a stalled SSE client must never block the upgrade itself.
func (h *updateHub) fanout(p updateProgress) {
	for ch := range h.subs {
		select {
		case ch <- p:
		default:
		}
	}
}

func (h *updateHub) snapshot() (updateProgress, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur, h.running
}

func (h *updateHub) subscribe() (<-chan updateProgress, func()) {
	ch := make(chan updateProgress, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// updateCheck queries GitHub's latest stable release and compares it with the running version.
//
// The frontend may also contact api.github.com (CORS allows it), but this endpoint
// is authoritative: the backend must reach GitHub to download updates. A browser
// may have connectivity or a proxy that the server lacks; reporting that here
// prevents offering an update that cannot be downloaded.
func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	current := BuildVersion
	mode := "binary"
	if selfupdate.InDocker() {
		mode = "docker"
	}
	boot := bootUpdateState()
	out := map[string]any{
		"current":     current,
		"mode":        mode,
		"os":          runtime.GOOS,
		"arch":        runtime.GOARCH,
		"has_backup":  selfupdate.HasBackup(),
		"repo":        selfupdate.Repo,
		"boot_notice": boot.Detail,
		"rolled_back": boot.RolledBack,
	}

	// Top-bar checks use cache; explicit checks pass force=1 to fetch fresh data.
	force := r.URL.Query().Get("force") != ""
	client := selfupdate.NewClient(s.m.GlobalProxy())
	rel, err := relCache.get(r.Context(), client, force)
	if err != nil {
		out["error"] = locale.ErrorMessage(locale.FromRequest(r), err)
		writeJSON(w, 200, out)
		return
	}

	latest := rel.TagName
	out["latest"] = latest
	out["notes"] = rel.Body
	out["html_url"] = rel.HTMLURL
	if !rel.PublishedAt.IsZero() {
		out["published_at"] = rel.PublishedAt.Format(time.RFC3339)
	}

	asset := selfupdate.AssetName(latest, runtime.GOOS, runtime.GOARCH)
	out["asset"] = asset
	if a, ok := rel.FindAsset(asset); ok {
		out["asset_available"] = true
		out["size"] = a.Size
	} else {
		out["asset_available"] = false
	}

	cmp, comparable := selfupdate.CompareVersions(current, latest)
	out["comparable"] = comparable
	out["has_update"] = comparable && cmp < 0
	if !comparable {
		// Development builds (dev or suffixed git-describe versions) are incomparable.
		// Disable updates to prevent a release overwriting the locally tested executable.
		out["reason"] = locale.Text(locale.FromRequest(r), "Current version %q is not a stable release; one-click updates are disabled", current)
	}
	writeJSON(w, 200, out)
}

// updateApply downloads/stages a version, then exits for supervisor-driven restart.
//
// Return 202 immediately and work in a goroutine. Full downloads may take minutes;
// tying them to a request risks reverse-proxy timeouts. Progress uses /api/update/stream.
func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	current := BuildVersion

	// Use cache to install exactly the version the user saw and confirmed.
	client := selfupdate.NewClient(s.m.GlobalProxy())
	rel, err := relCache.get(r.Context(), client, false)
	if err != nil {
		writeError(w, 502, err)
		return
	}
	cmp, comparable := selfupdate.CompareVersions(current, rel.TagName)
	if !comparable {
		writeErr(w, 400, locale.Text(locale.FromRequest(r), "Current version %q is not a stable release; one-click updates are disabled", current))
		return
	}
	if cmp >= 0 {
		writeErr(w, 400, locale.Text(locale.FromRequest(r), "Already running the latest version %s", current))
		return
	}
	if !updHub.begin(rel.TagName) {
		writeErr(w, 409, "An update is already in progress")
		return
	}

	go func() {
		// Use the server context: the HTTP request ends after its response, which would
		// immediately cancel a download tied to the request context.
		err := selfupdate.StageMessages(s.ctx, client, rel, current, func(ph selfupdate.Phase, pct int, msg locale.Message) {
			updHub.publishMessage(ph, pct, msg)
		})
		updHub.finish(err)
		if err != nil {
			log.Printf("[update] Update failed: %v", err)
			return
		}
		log.Printf("[update] %s -> %s staged; exiting to complete replacement", current, rel.TagName)
		// Allow the final progress event to reach clients before requesting exit.
		time.Sleep(1500 * time.Millisecond)
		requestRestart()
	}()

	writeJSON(w, 202, map[string]any{"ok": true, "target": rel.TagName})
}

// updateRollback restores the previous executable backed up as artex.old.
func (s *Server) updateRollback(w http.ResponseWriter, r *http.Request) {
	if _, running := updHub.snapshot(); running {
		writeErr(w, 409, "Cannot roll back while an update is in progress")
		return
	}
	if err := selfupdate.Rollback(); err != nil {
		writeError(w, 400, err)
		return
	}
	log.Printf("[update] Manually rolled back to the previous version; exiting to complete the switch")
	writeJSON(w, 202, map[string]any{"ok": true})
	go func() {
		time.Sleep(500 * time.Millisecond)
		requestRestart()
	}()
}

// updateStream sends update progress over SSE.
func (s *Server) updateStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := updHub.subscribe()
	defer unsub()

	send := func(p updateProgress) {
		p = p.inLanguage(locale.FromRequest(r))
		b, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	// Send current state immediately so a refreshed page sees the ongoing upgrade.
	cur, _ := updHub.snapshot()
	send(cur)

	ctx := r.Context()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-ch:
			if !ok {
				return
			}
			send(p)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// inLanguage renders a copy, so concurrent SSE subscribers cannot change each other's locale.
func (p updateProgress) inLanguage(lang locale.Lang) updateProgress {
	if p.message.Template != "" {
		p.Message = p.message.In(lang)
	}
	if p.cause != nil {
		p.Error = locale.ErrorMessage(lang, p.cause)
	}
	return p
}
