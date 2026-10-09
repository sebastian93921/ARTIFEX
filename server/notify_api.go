package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"net/http"
	"strings"
	"time"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/notify"
)

// Notification HTTP endpoints are all behind requireAuth in Handler, matching
// other management endpoints.

// notifyChannelDTO is the public channel representation.
//
// Config is masked: credential values begin with notify.MaskedPrefix.
// Returning a masked value unchanged means preserve the stored secret,
// as implemented by notify.MergeConfig.
type notifyChannelDTO struct {
	ID         int64          `json:"id"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"`
	Enabled    bool           `json:"enabled"`
	Mode       string         `json:"mode"`
	Config     map[string]any `json:"config"`
	Filter     notify.Filter  `json:"filter"`
	RatePerMin int            `json:"rate_per_min"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	// SecretKeys tells the frontend which fields need password inputs and unchanged-value hints.
	// Each channel declares them; the frontend does not hard-code channel-specific secret rules.
	SecretKeys []string `json:"secret_keys"`
}

// notifyDeliveryDTO is the public delivery-history representation.
type notifyDeliveryDTO struct {
	ID          int64      `json:"id"`
	FindingID   int64      `json:"finding_id,string"`
	EventKind   string     `json:"event_kind"`
	ChannelID   int64      `json:"channel_id"`
	ChannelName string     `json:"channel_name"`
	ChannelKind string     `json:"channel_kind"`
	State       string     `json:"state"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error"`
	BatchID     *int64     `json:"batch_id,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
	NextAttempt time.Time  `json:"next_attempt_at"`
	// Title/summary lets history identify the notification without expanding it.
	Title    string `json:"title"`
	Severity string `json:"severity"`
}

func toNotifyChannelDTO(ch *db.NotificationChannel) notifyChannelDTO {
	var cfg map[string]any
	if len(ch.Config) > 0 {
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	secrets := []string{}
	if c, ok := notify.Get(ch.Kind); ok {
		secrets = c.SecretKeys()
	}
	return notifyChannelDTO{
		ID:         ch.ID,
		Name:       ch.Name,
		Kind:       ch.Kind,
		Enabled:    ch.IsEnabled(),
		Mode:       ch.Mode,
		Config:     notify.MaskConfig(ch.Kind, cfg),
		Filter:     notify.ParseFilter(ch.Filter),
		RatePerMin: ch.RatePerMin,
		CreatedAt:  ch.CreatedAt,
		UpdatedAt:  ch.UpdatedAt,
		SecretKeys: secrets,
	}
}

func toNotifyDeliveryDTO(dl *db.NotificationDelivery) notifyDeliveryDTO {
	snap, _ := parseSnapshot(dl)
	dto := notifyDeliveryDTO{
		ID:          dl.ID,
		FindingID:   dl.FindingID,
		EventKind:   dl.EventKind,
		ChannelID:   dl.ChannelID,
		ChannelName: dl.ChannelName,
		ChannelKind: dl.ChannelKind,
		State:       dl.State,
		Attempts:    dl.Attempts,
		LastError:   dl.LastError,
		BatchID:     dl.BatchID,
		CreatedAt:   dl.CreatedAt,
		SentAt:      dl.SentAt,
		NextAttempt: dl.NextAttemptAt,
		Severity:    snap.Severity,
	}
	if snap.Name != "" {
		dto.Title = snap.Name
	} else {
		dto.Title = snap.VulnClass
	}
	return dto
}

// notifyMeta returns static metadata and global settings together, avoiding
// multiple requests merely to render a selector.
func (s *Server) notifyMeta(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	kinds := make([]map[string]any, 0, len(notify.Kinds()))
	for _, k := range notify.Kinds() {
		ch, _ := notify.Get(k)
		kinds = append(kinds, map[string]any{
			"kind":                 k,
			"default_rate_per_min": ch.DefaultRatePerMin(),
			"secret_keys":          ch.SecretKeys(),
		})
	}
	baseURL, _, _ := pg.GetSetting(settingNotifyPublicBaseURL)
	digest, _, _ := pg.GetSetting(settingNotifyDigestMinutes)
	stats, err := pg.NotificationStatsSnapshot(r.Context())
	if err != nil {
		writeNotifyError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"kinds":               kinds,
		"enabled":             pg.GetBool(settingNotifyEnabled, true),
		"public_base_url":     baseURL,
		"digest_interval_min": digest,
		"defaults": map[string]any{
			"digest_interval_min": notifyDefaultDigestMinutes,
		},
		"stats": stats,
	})
}

func (s *Server) notifyListChannels(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	channels, err := pg.ListNotificationChannels(r.Context())
	if err != nil {
		writeNotifyError(w, 500, err)
		return
	}
	out := make([]notifyChannelDTO, 0, len(channels))
	for _, ch := range channels {
		out = append(out, toNotifyChannelDTO(ch))
	}
	writeJSON(w, 200, map[string]any{"channels": out})
}

// notifyChannelRequest is the create/update request body.
//
// Pointer fields distinguish omission from an explicit zero value; PATCH-like
// updates must preserve stored values when a field is omitted.
type notifyChannelRequest struct {
	Name       *string        `json:"name"`
	Kind       *string        `json:"kind"`
	Enabled    *bool          `json:"enabled"`
	Mode       *string        `json:"mode"`
	Config     map[string]any `json:"config"`
	Filter     *notify.Filter `json:"filter"`
	RatePerMin *int           `json:"rate_per_min"`
}

func (s *Server) notifyCreateChannel(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req notifyChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Request body is not valid JSON: ")+err.Error())
		return
	}
	if req.Kind == nil || !notify.ValidKind(*req.Kind) {
		writeErr(w, 400, fmt.Sprintf(locale.Text(responseLanguage(w), "Invalid channel kind; choose: %s"), strings.Join(notify.Kinds(), " / ")))
		return
	}
	name := ""
	if req.Name != nil {
		name = strings.TrimSpace(*req.Name)
	}
	if name == "" {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Channel name is required"))
		return
	}
	channel, _ := notify.Get(*req.Kind)
	if err := channel.Validate(req.Config); err != nil {
		writeNotifyError(w, 400, err)
		return
	}
	ch := &db.NotificationChannel{
		Name:       name,
		Kind:       *req.Kind,
		Enabled:    req.Enabled,
		Mode:       db.NotifyModeRealtime,
		RatePerMin: channel.DefaultRatePerMin(),
	}
	if req.Mode != nil {
		if !db.ValidNotifyMode(*req.Mode) {
			writeErr(w, 400, locale.Text(responseLanguage(w), "Invalid delivery mode; choose realtime or digest"))
			return
		}
		ch.Mode = *req.Mode
	}
	if req.RatePerMin != nil {
		// Honor explicit values, including zero, which validly means unlimited.
		if *req.RatePerMin < 0 {
			writeErr(w, 400, locale.Text(responseLanguage(w), "Rate limit cannot be negative"))
			return
		}
		ch.RatePerMin = *req.RatePerMin
	}
	// Apply channel defaults only when the field is omitted. Resolve this here,
	// where omission can be distinguished from explicit zero, not in the database.
	// Treating zero as missing there would make unlimited-rate configuration impossible.
	if req.RatePerMin == nil {
		ch.RatePerMin = channel.DefaultRatePerMin()
	}
	if req.Filter != nil {
		// Validate constrained filters such as min_severity at write time. A typo must
		// fail validation rather than silently disabling filtering and sending everything.
		if err := req.Filter.Validate(); err != nil {
			writeNotifyError(w, 400, err)
			return
		}
		raw, _ := json.Marshal(req.Filter)
		ch.Filter = raw
	}
	rawCfg, _ := json.Marshal(req.Config)
	ch.Config = rawCfg

	id, err := pg.SaveNotificationChannel(r.Context(), ch)
	if err != nil {
		writeNotifyError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) notifyUpdateChannel(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Invalid channel ID"))
		return
	}
	current, err := pg.NotificationChannelByID(r.Context(), id)
	if err != nil {
		notifyChannelLookupErr(w, err)
		return
	}
	var req notifyChannelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Request body is not valid JSON: ")+err.Error())
		return
	}

	// Changing kind replaces its credential configuration; never merge another kind's old fields.
	kind := current.Kind
	if req.Kind != nil {
		if !notify.ValidKind(*req.Kind) {
			writeErr(w, 400, fmt.Sprintf(locale.Text(responseLanguage(w), "Invalid channel kind; choose: %s"), strings.Join(notify.Kinds(), " / ")))
			return
		}
		kind = *req.Kind
	}
	channel, _ := notify.Get(kind)

	var stored map[string]any
	if kind == current.Kind {
		if len(current.Config) > 0 {
			_ = json.Unmarshal(current.Config, &stored)
		}
	}
	if stored == nil {
		stored = map[string]any{}
	}
	// PrepareConfigUpdate requires an explicit credential decision when destination changes,
	// preventing a URL-only edit from forwarding stored credentials to a new destination.
	merged, err := notify.PrepareConfigUpdate(kind, stored, req.Config)
	if err != nil {
		writeNotifyError(w, 400, err)
		return
	}
	if err := channel.Validate(merged); err != nil {
		writeNotifyError(w, 400, err)
		return
	}
	rawCfg, _ := json.Marshal(merged)

	ch := &db.NotificationChannel{
		ID:         id,
		Name:       current.Name,
		Kind:       kind,
		Enabled:    current.Enabled,
		Mode:       current.Mode,
		Config:     rawCfg,
		Filter:     current.Filter,
		RatePerMin: current.RatePerMin,
	}
	if req.Name != nil {
		if ch.Name = strings.TrimSpace(*req.Name); ch.Name == "" {
			writeErr(w, 400, locale.Text(responseLanguage(w), "Channel name cannot be empty"))
			return
		}
	}
	if req.Enabled != nil {
		ch.Enabled = req.Enabled
	}
	if req.Mode != nil {
		if !db.ValidNotifyMode(*req.Mode) {
			writeErr(w, 400, locale.Text(responseLanguage(w), "Invalid delivery mode; choose realtime or digest"))
			return
		}
		ch.Mode = *req.Mode
	}
	if req.RatePerMin != nil {
		if *req.RatePerMin < 0 {
			writeErr(w, 400, locale.Text(responseLanguage(w), "Rate limit cannot be negative"))
			return
		}
		ch.RatePerMin = *req.RatePerMin
	}
	if req.Filter != nil {
		if err := req.Filter.Validate(); err != nil {
			writeNotifyError(w, 400, err)
			return
		}
		raw, _ := json.Marshal(req.Filter)
		ch.Filter = raw
	}

	// SetNotificationChannelEnabled also marks pending deliveries skipped when disabling,
	// preventing a stale backlog from being sent when the channel is re-enabled.
	// SaveNotificationChannel alone would not perform that transition.
	enabledChanged := ch.Enabled != nil && current.Enabled != nil && *ch.Enabled != *current.Enabled
	if enabledChanged {
		// Persist configuration with the old enabled value, then toggle separately.
		// This endpoint owns updates to both fields, preventing a premature skip transition.
		prev := ch.Enabled
		ch.Enabled = current.Enabled
		if _, err := pg.SaveNotificationChannel(r.Context(), ch); err != nil {
			writeNotifyError(w, 500, err)
			return
		}
		if err := pg.SetNotificationChannelEnabled(r.Context(), id, *prev); err != nil {
			writeNotifyError(w, 500, err)
			return
		}
		writeJSON(w, 200, map[string]any{"id": id})
		return
	}
	if _, err := pg.SaveNotificationChannel(r.Context(), ch); err != nil {
		writeNotifyError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"id": id})
}

func (s *Server) notifyDeleteChannel(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Invalid channel ID"))
		return
	}
	if err := pg.DeleteNotificationChannel(r.Context(), id); err != nil {
		notifyChannelLookupErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// notifyTestChannel sends one test message using the currently saved configuration.
//
// Call Send directly rather than queuing: users need immediate configuration
// feedback without opening delivery history to discover whether the test succeeded.
// This endpoint is synchronous, bounded by the notification client's 15-second timeout.
func (s *Server) notifyTestChannel(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Invalid channel ID"))
		return
	}
	ch, err := pg.NotificationChannelByID(r.Context(), id)
	if err != nil {
		notifyChannelLookupErr(w, err)
		return
	}
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		writeErr(w, 400, fmt.Sprintf(locale.Text(responseLanguage(w), "Channel kind %q is not registered"), ch.Kind))
		return
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if err := channel.Validate(cfg); err != nil {
		writeNotifyError(w, 400, err)
		return
	}
	msg := notifyTestMessage(s.notifierBaseURL(pg), locale.FromRequest(r))
	start := time.Now()
	// A test contains one item, so the delivered count is unnecessary; single-item
	// truncation handles channel limits without segmentation.
	if _, err := channel.Send(r.Context(), cfg, msg); err != nil {
		// Preserve the channel's actual error detail so users can diagnose their configuration.
		writeNotifyError(w, 502, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"ok":         true,
		"latency_ms": time.Since(start).Milliseconds(),
	})
}

// notifyTestMessage uses unmistakably synthetic content so recipients cannot
// mistake the test for a real vulnerability.
func notifyTestMessage(baseURL string, langs ...locale.Lang) notify.Message {
	return notify.Message{
		Language: locale.First(langs),
		Items: []notify.Item{{
			FindingID: 0,
			Name:      locale.Text(locale.First(langs), "Test message · Channel configured successfully"),
			VulnClass: locale.Text(locale.First(langs), "Connectivity test"),
			Severity:  "low",
			Summary:   locale.Text(locale.First(langs), "This is an ARTIFEX notification-channel test. Receiving it confirms the channel configuration works."),
			Assets:    []string{"artifex.example.com"},
			DetailURL: baseURL,
		}},
		HomeURL: baseURL,
	}
}

// notifierBaseURL reads the external return-link base URL.
func (s *Server) notifierBaseURL(pg *db.DB) string {
	v, _, _ := pg.GetSetting(settingNotifyPublicBaseURL)
	return trimTrailingSlash(v)
}

func (s *Server) notifyListDeliveries(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	f := db.NotificationDeliveryFilter{
		State:     r.URL.Query().Get("state"),
		EventKind: r.URL.Query().Get("event_kind"),
	}
	if v := r.URL.Query().Get("channel_id"); v != "" {
		f.ChannelID = int64(atoiDefault(v, 0))
	}
	page := queryInt(r, "page", 1)
	pageSize := queryInt(r, "page_size", 50)
	items, total, err := pg.ListNotificationDeliveries(r.Context(), f, page, pageSize)
	if err != nil {
		writeNotifyError(w, 500, err)
		return
	}
	out := make([]notifyDeliveryDTO, 0, len(items))
	for _, dl := range items {
		out = append(out, toNotifyDeliveryDTO(dl))
	}
	writeJSON(w, 200, map[string]any{"deliveries": out, "total": total, "page": page, "page_size": pageSize})
}

func (s *Server) notifyRetryDelivery(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, locale.Text(responseLanguage(w), "Invalid delivery ID"))
		return
	}
	if err := pg.RetryNotificationDelivery(r.Context(), id); err != nil {
		writeNotifyError(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// notifyChannelLookupErr maps missing channels to 404 and other failures to 500.
func notifyChannelLookupErr(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrNotificationChannelNotFound) {
		writeErr(w, 404, locale.Text(responseLanguage(w), "Notification channel not found"))
		return
	}
	writeNotifyError(w, 500, err)
}

// writeNotifyError preserves typed delivery failures until their request-language boundary.
func writeNotifyError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]any{"error": notify.ErrorMessage(responseLanguage(w), err)})
}
