package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"log"
	"strings"
	"time"

	"github.com/sebastian93921/artifex/notify"
)

// IM notification channel configuration and event layer. Delivery claiming and transitions are in
// db/notification_delivery.go。
//
// Preserve these two invariants when changing this file:
//
//  1. The finding-write transaction (RecordFindingTx) only calls InsertNotificationEventTx for one blind insert;
//     it reads no notification tables and evaluates no filters. Adding reads here could let
//     malformed user filters contaminate or abort the finding-write transaction.
//  2. Filter matching never errors: malformed configuration matches (see notify.Match). Prefer extra
//     notifications to missed notifications.

// ErrNotificationChannelNotFound indicates a missing channel.
var ErrNotificationChannelNotFound = locale.NewError("Notification channel not found")

// Delivery states.
const (
	NotifyStatePending = "pending" // Pending.
	NotifyStateSending = "sending" // Claimed by a dispatcher with an unexpired lease.
	NotifyStateSent    = "sent"    // Delivered.
	NotifyStateFailed  = "failed"  // Retries exhausted or permanent failure; manual retry is possible.
	NotifyStateSkipped = "skipped" // Channel disabled; no further sends.
)

// Notification modes.
const (
	NotifyModeRealtime = "realtime"
	NotifyModeDigest   = "digest"
)

// ValidNotifyMode allowlists notification modes, like findings.status: no DB CHECK,
// allowing future extension.
func ValidNotifyMode(m string) bool {
	return m == NotifyModeRealtime || m == NotifyModeDigest
}

// NotificationChannel configures one channel instance. Config and Filter retain raw JSON;
// notify parses them, while the DB layer does not interpret their fields.
type NotificationChannel struct {
	ID     int64           `json:"id"`
	Name   string          `json:"name"`
	Kind   string          `json:"kind"`
	Mode   string          `json:"mode"`
	Config json.RawMessage `json:"config"`
	Filter json.RawMessage `json:"filter"`
	// Enabled is a pointer to distinguish an omitted field from explicit false;
	// frontend toggles submit only changed fields.
	Enabled    *bool     `json:"enabled,omitempty"`
	RatePerMin int       `json:"rate_per_min"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// IsEnabled treats nil Enabled (not loaded) as enabled.
func (c *NotificationChannel) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

// NotificationEvent represents an event fact.
type NotificationEvent struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	FindingID int64           `json:"finding_id"`
	Snapshot  json.RawMessage `json:"snapshot"`
	CreatedAt time.Time       `json:"created_at"`
}

const notificationChannelCols = `id, name, kind, enabled, config, mode, filter, rate_per_min, created_at, updated_at`

func scanNotificationChannel(sc interface{ Scan(...any) error }) (*NotificationChannel, error) {
	var c NotificationChannel
	var enabled bool
	if err := sc.Scan(&c.ID, &c.Name, &c.Kind, &enabled, &c.Config, &c.Mode, &c.Filter, &c.RatePerMin, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	c.Enabled = &enabled
	return &c, nil
}

// ListNotificationChannels returns all channels, enabled first, then by ID.
// SQL sorting gives the UI and dispatcher the same stable order.
func (d *DB) ListNotificationChannels(ctx context.Context) ([]*NotificationChannel, error) {
	rows, err := d.QueryContext(ctx, `SELECT `+notificationChannelCols+` FROM notification_channels
ORDER BY enabled DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*NotificationChannel{}
	for rows.Next() {
		c, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// NotificationChannelByID loads one channel.
func (d *DB) NotificationChannelByID(ctx context.Context, id int64) (*NotificationChannel, error) {
	row := d.QueryRowContext(ctx, `SELECT `+notificationChannelCols+` FROM notification_channels WHERE id=$1`, id)
	c, err := scanNotificationChannel(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotificationChannelNotFound
	}
	return c, err
}

// SaveNotificationChannel creates or updates a channel.
//
// Updates overwrite only explicitly supplied non-nil/nonempty fields, allowing partial
// drawer forms without resubmitting undisplayed config fields, which could otherwise
// overwrite real secrets with masked values.
func (d *DB) SaveNotificationChannel(ctx context.Context, c *NotificationChannel) (int64, error) {
	if c.Mode == "" {
		c.Mode = NotifyModeRealtime
	}
	// Preserve 0 exactly: it is a valid unlimited-rate setting.
	//
	// The former RatePerMin <= 0 defaulting logic intended to supply a safe omitted-field default
	// but also consumed explicit zero. Documentation, UI hints, and takeTokens interpreted zero
	// as unlimited, while this layer silently changed it to 20 (DingTalk/WeCom/Telegram)
	// or 100 (Feishu), leaving users unknowingly limited despite requesting unlimited throughput.
	//
	// Only callers can distinguish omission from explicit zero in the request body,
	// so server-side notifyCreateChannel supplies defaults for omitted fields.
	if c.RatePerMin < 0 {
		return 0, locale.NewError("Rate limit must not be negative")
	}
	if c.Config == nil {
		c.Config = json.RawMessage(`{}`)
	}
	if c.Filter == nil {
		c.Filter = json.RawMessage(`{}`)
	}
	enabled := c.IsEnabled()

	if c.ID == 0 {
		var id int64
		err := d.QueryRowContext(ctx, `INSERT INTO notification_channels(name,kind,enabled,config,mode,filter,rate_per_min)
VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
			c.Name, c.Kind, enabled, string(c.Config), c.Mode, string(c.Filter), c.RatePerMin).Scan(&id)
		return id, err
	}
	res, err := d.ExecContext(ctx, `UPDATE notification_channels
SET name=$2, kind=$3, enabled=$4, config=$5, mode=$6, filter=$7, rate_per_min=$8
WHERE id=$1`,
		c.ID, c.Name, c.Kind, enabled, string(c.Config), c.Mode, string(c.Filter), c.RatePerMin)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotificationChannelNotFound
	}
	return c.ID, nil
}

// SetNotificationChannelEnabled toggles a channel.
//
// Disabling a channel marks unsent deliveries skipped, avoiding a burst of stale findings
// when re-enabled that could be mistaken for newly discovered vulnerabilities.
func (d *DB) SetNotificationChannelEnabled(ctx context.Context, id int64, enabled bool) error {
	return d.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE notification_channels SET enabled=$2 WHERE id=$1`, id, enabled)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotificationChannelNotFound
		}
		if !enabled {
			if _, err := tx.ExecContext(ctx, `UPDATE notification_deliveries SET state=$2, last_error=$3
WHERE channel_id=$1 AND state IN ($4,$5)`,
				id, NotifyStateSkipped, "Channel disabled", NotifyStatePending, NotifyStateSending); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteNotificationChannel deletes a channel and cascades its delivery history through the foreign key;
// without channel configuration that history cannot be interpreted.
func (d *DB) DeleteNotificationChannel(ctx context.Context, id int64) error {
	res, err := d.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotificationChannelNotFound
	}
	return nil
}

// RecordNotificationEventTx makes a best-effort event write inside the caller's transaction.
//
// This is the sole notification operation on the finding-write path: one INSERT, no table reads,
// channel knowledge, or filtering. Successful commit atomically persists the finding and queued event,
// leaving no post-commit enqueue window in which the message can be lost.
//
// Two intentional design choices:
//
//  1. SAVEPOINT is required because any PostgreSQL statement error aborts the entire transaction;
//     all later statements, including COMMIT, fail. Merely ignoring an INSERT error
//     cannot allow the caller to commit unless a savepoint isolates that statement's failure.
//     Without a savepoint, rolling back the entire transaction is the only option.
//
//  2. Full rollback is wrong here: notifications are a convenience, but finding persistence is core.
//     A notification-table issue (missing migration or transient disk failure) must not prevent saving
//     a critical finding. Isolate and log the error, then return false so the finding can commit, sacrificing this notification.
//     The bool return is intentional: callers must not treat it as an error that invalidates finding persistence.
func RecordNotificationEventTx(ctx context.Context, tx *sql.Tx, kind string, findingID int64, snap notify.Snapshot) bool {
	raw, err := json.Marshal(snap)
	if err != nil {
		log.Print(locale.Text(locale.ServerDefault(), "[notify] Serialize notification event failed finding=%d: %v", findingID, err))
		return false
	}
	if _, err := tx.ExecContext(ctx, `SAVEPOINT notify_event`); err != nil {
		log.Print(locale.Text(locale.ServerDefault(), "[notify] Create savepoint failed finding=%d: %v", findingID, err))
		return false
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO notification_events(kind,finding_id,snapshot) VALUES($1,$2,$3)`,
		kind, findingID, string(raw)); err != nil {
		log.Print(locale.Text(locale.ServerDefault(), "[notify] Write notification event failed finding=%d (finding record unaffected): %v", findingID, err))
		// Roll back to the savepoint to recover the transaction from the aborted state.
		if _, rbErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT notify_event`); rbErr != nil {
			log.Print(locale.Text(locale.ServerDefault(), "[notify] Roll back to savepoint failed finding=%d: %v", findingID, rbErr))
		}
		return false
	}
	// Release the savepoint to avoid accumulating unused savepoints in long transactions.
	_, _ = tx.ExecContext(ctx, `RELEASE SAVEPOINT notify_event`)
	return true
}

// AddNotificationEvent is the standalone transaction variant of InsertNotificationEventTx for callers
// without an existing transaction, such as sending a channel test message without a real finding.
func (d *DB) AddNotificationEvent(ctx context.Context, kind string, findingID int64, snap notify.Snapshot) (int64, error) {
	raw, err := json.Marshal(snap)
	if err != nil {
		return 0, locale.Errorf("Serialize notification event snapshot: %w", err)
	}
	var id int64
	err = d.QueryRowContext(ctx, `INSERT INTO notification_events(kind,finding_id,snapshot) VALUES($1,$2,$3) RETURNING id`,
		kind, findingID, string(raw)).Scan(&id)
	return id, err
}

// FanOutPendingEvents expands undispatched finding events into deliveries for currently enabled channels,
// returning the number of processed events and newly created deliveries.
//
// The whole round is transactional. FOR UPDATE SKIP LOCKED claims different rows across processes,
// using the same approach as the archive queue (see
// completeNextArchiveJob in db/task_archives.go).
//
// Match filters in Go rather than SQL: channel filters are JSONB with optional fields, and expressing
// six matching combinations in SQL would be hard to maintain. Only a few channels are manually configured,
// so loading them all and comparing in memory is faster and easier to test.
//
// Events matching no channel still become fanned_out, or they would stay pending forever
// and be rescanned on every tick.
func (d *DB) FanOutPendingEvents(ctx context.Context, limit int) (eventCount, deliveryCount int, err error) {
	if limit <= 0 {
		limit = 200
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback() //nolint:errcheck // No-op after a successful commit.

	channels, err := listEnabledNotificationChannelsTx(ctx, tx)
	if err != nil {
		return 0, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, kind, finding_id, snapshot FROM notification_events
WHERE NOT fanned_out ORDER BY id FOR UPDATE SKIP LOCKED LIMIT $1`, limit)
	if err != nil {
		return 0, 0, err
	}
	var (
		events      []NotificationEvent
		parsedSnaps []notify.Snapshot
	)
	for rows.Next() {
		var ev NotificationEvent
		if err := rows.Scan(&ev.ID, &ev.Kind, &ev.FindingID, &ev.Snapshot); err != nil {
			rows.Close()
			return 0, 0, err
		}
		var snap notify.Snapshot
		// Snapshots are written by us and should parse. A parse failure does not stop dispatching,
		// but empty fields make filtered channels skip that event. Prefer missing one notification
		// to letting a corrupt row block the entire queue.
		_ = json.Unmarshal(ev.Snapshot, &snap)
		// The row's kind is authoritative; the snapshot copy is for rendering and may come from an older version.
		snap.Kind = ev.Kind
		events = append(events, ev)
		parsedSnaps = append(parsedSnaps, snap)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(events) == 0 {
		return 0, 0, tx.Commit()
	}

	type pending struct {
		eventID   int64
		channelID int64
	}
	var toInsert []pending
	for i, snap := range parsedSnaps {
		for _, ch := range channels {
			if !notify.Match(notify.ParseFilter(ch.Filter), snap) {
				continue
			}
			toInsert = append(toInsert, pending{eventID: events[i].ID, channelID: ch.ID})
		}
	}
	if len(toInsert) > 0 {
		var (
			vals []string
			args []any
		)
		for _, p := range toInsert {
			vals = append(vals, fmt.Sprintf("($%d,$%d)", len(args)+1, len(args)+2))
			args = append(args, p.eventID, p.channelID)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO notification_deliveries(event_id,channel_id) VALUES `+strings.Join(vals, ","), args...); err != nil {
			return 0, 0, err
		}
	}

	// Mark this round's events dispatched, including those matching no channel (see function comment).
	ids := make([]string, 0, len(events))
	markArgs := make([]any, 0, len(events))
	for _, ev := range events {
		markArgs = append(markArgs, ev.ID)
		ids = append(ids, fmt.Sprintf("$%d", len(markArgs)))
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_events SET fanned_out=true WHERE id IN (`+strings.Join(ids, ",")+`)`, markArgs...); err != nil {
		return 0, 0, err
	}
	return len(events), len(toInsert), tx.Commit()
}

// listEnabledNotificationChannelsTx loads the few enabled channels transactionally without
// pagination or caching; caching would add uncertainty about when configuration edits take effect.
func listEnabledNotificationChannelsTx(ctx context.Context, tx *sql.Tx) ([]*NotificationChannel, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, name, kind, config, mode, filter, rate_per_min
FROM notification_channels WHERE enabled ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*NotificationChannel{}
	for rows.Next() {
		var c NotificationChannel
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.Config, &c.Mode, &c.Filter, &c.RatePerMin); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// NotificationAssetNames resolves asset IDs to short notification display names.
//
// Preserve input order and omit missing IDs, so output may be shorter. Stable ordering
// ensures repeated deliveries of the same finding retain the same asset order;
// reordered assets after a retry could otherwise be mistaken for changed assets.
func (d *DB) NotificationAssetNames(ctx context.Context, ids []int64) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph, args := placeholders(1, ids)
	rows, err := d.QueryContext(ctx, `SELECT id, type, domain, ip, url, app_name, bundle_id FROM assets WHERE id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	labels := map[int64]string{}
	for rows.Next() {
		var (
			id                int64
			typ               string
			domain, ip, url   sql.NullString
			appName, bundleID sql.NullString
		)
		if err := rows.Scan(&id, &typ, &domain, &ip, &url, &appName, &bundleID); err != nil {
			return nil, err
		}
		labels[id] = assetDisplayName(typ, domain.String, ip.String, url.String, appName.String, bundleID.String)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if label, ok := labels[id]; ok && label != "" {
			out = append(out, label)
		}
	}
	return out, nil
}

// assetDisplayName selects the most identifiable label for an asset type.
// Return empty if unavailable and leave presentation to the caller rather than inventing
// placeholders such as "Asset #42" that readers might mistake for real domains.
func assetDisplayName(typ, domain, ip, url, appName, bundleID string) string {
	pick := func(vals ...string) string {
		for _, v := range vals {
			if strings.TrimSpace(v) != "" {
				return v
			}
		}
		return ""
	}
	switch typ {
	case "root_domain", "subdomain":
		return domain
	case "ip":
		return ip
	case "app":
		return pick(appName, bundleID)
	case "service", "endpoint":
		return pick(url, domain, ip)
	default:
		return pick(domain, ip, url, appName)
	}
}

// SetFindingStatusWithNotify changes finding status and registers a status-change
// notification event in the same transaction.
//
// Return the previous status as from, existence as found, and event registration success as notified.
//
// Three intentional behaviors:
//   - Unchanged status creates no event. Repeated frontend submissions and idempotent automation
//     must not generate notification noise.
//   - Missing finding returns found=false without writes; the caller converts this to 404.
//   - Event-registration failure does not affect the status update (see savepoints in RecordNotificationEventTx),
//     so notified=false can still mean the status changed successfully and must not become a caller error.
func (d *DB) SetFindingStatusWithNotify(ctx context.Context, id int64, status string) (from string, found bool, notified bool, err error) {
	err = d.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		var txErr error
		from, found, _, notified, txErr = SetFindingStatusTx(ctx, tx, id, status)
		return txErr
	})
	return from, found, notified, err
}

// SetFindingStatusTx updates status and registers its notification inside the caller's transaction.
//
// This transaction-level helper gives all status-changing paths identical semantics. Previously only
// patchFinding used the notifying variant, while a "fixed" retest in finding_retests
// directly executed UPDATE findings SET status=..., so channels configured with
// on_status_change missed these transitions. The UI changed silently and operators
// had to open the platform to discover them.
//
// Return previous status, existence, actual status change, and event-registration success as
// from/found/changed/notified. Registration failure does not block status updates; see RecordNotificationEventTx.
func SetFindingStatusTx(ctx context.Context, tx *sql.Tx, id int64, status string) (from string, found bool, changed bool, notified bool, err error) {
	var (
		vulnclass, name, severity, summary string
		taskID                             sql.NullInt64
		assetIDs                           []byte
	)
	scanErr := tx.QueryRowContext(ctx, `SELECT vulnclass, name, severity, summary, task_id, asset_ids, status
FROM findings WHERE id=$1 FOR UPDATE`, id).
		Scan(&vulnclass, &name, &severity, &summary, &taskID, &assetIDs, &from)
	if scanErr == sql.ErrNoRows {
		return "", false, false, false, nil
	}
	if scanErr != nil {
		return "", false, false, false, scanErr
	}
	found = true
	if from == status {
		// Do not register events for unchanged status: repeated submissions and idempotent replays
		// must not create notification noise.
		return from, true, false, false, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE findings SET status=$2 WHERE id=$1`, id, status); err != nil {
		return from, true, false, false, err
	}
	var assets []int64
	_ = json.Unmarshal(assetIDs, &assets)
	notified = RecordNotificationEventTx(ctx, tx, notify.EventFindingStatusChanged, id, notify.Snapshot{
		Kind:       notify.EventFindingStatusChanged,
		FindingID:  id,
		TaskID:     taskID.Int64,
		VulnClass:  vulnclass,
		Name:       name,
		Severity:   severity,
		Summary:    summary,
		AssetIDs:   assets,
		FromStatus: from,
		ToStatus:   status,
	})
	return from, true, true, notified, nil
}

// NotificationStats holds the notification page's overview counts.
type NotificationStats struct {
	Channels     int   `json:"channels"`
	ChannelsOn   int   `json:"channels_on"`
	Pending      int   `json:"pending"`
	Failed       int   `json:"failed"`
	SentToday    int   `json:"sent_today"`
	BacklogAgeMS int64 `json:"backlog_age_ms"` // Age of the oldest pending delivery in milliseconds.
}

// NotificationStatsSnapshot summarizes notification-system health.
// BacklogAgeMS indicates stalled delivery more directly than pending count:
// three queued items could have waited three seconds or three hours.
func (d *DB) NotificationStatsSnapshot(ctx context.Context) (*NotificationStats, error) {
	var s NotificationStats
	if err := d.QueryRowContext(ctx, `SELECT
    (SELECT count(*) FROM notification_channels),
    (SELECT count(*) FROM notification_channels WHERE enabled),
    (SELECT count(*) FROM notification_deliveries WHERE state IN ($1,$2)),
    (SELECT count(*) FROM notification_deliveries WHERE state=$3),
    (SELECT count(*) FROM notification_deliveries WHERE state=$4 AND sent_at >= date_trunc('day', now())),
    COALESCE((SELECT EXTRACT(EPOCH FROM (now() - min(created_at))) * 1000 FROM notification_deliveries WHERE state=$1), 0)::bigint`,
		NotifyStatePending, NotifyStateSending, NotifyStateFailed, NotifyStateSent).
		Scan(&s.Channels, &s.ChannelsOn, &s.Pending, &s.Failed, &s.SentToday, &s.BacklogAgeMS); err != nil {
		return nil, err
	}
	return &s, nil
}
