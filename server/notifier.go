package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/notify"
)

// Global keys use the existing settings table; no new table is needed.
const (
	// settingNotifyEnabled is an emergency maintenance switch, on by default.
	// Configured channels, rather than this switch, determine whether notifications exist.
	settingNotifyEnabled = "notify_enabled"
	// settingNotifyPublicBaseURL supplies the externally accessible finding-detail URL,
	// such as https://artex.example.com. Empty omits the return-link button.
	// No existing external-base setting can be reused, so notifications own this key.
	settingNotifyPublicBaseURL = "notify_public_base_url"
	// settingNotifyDigestMinutes controls the digest period in minutes.
	settingNotifyDigestMinutes = "notify_digest_interval_min"
)

const (
	// notifyTick bounds realtime delivery latency to a three-second polling interval,
	// the main delay between persisted findings and messages arriving at a channel.
	notifyTick = 3 * time.Second
	// notifyLease must greatly exceed worst-case delivery time (the notification HTTP
	// client allows 15 seconds), preventing two dispatchers from sending the same
	// row concurrently after premature lease expiration.
	notifyLease = 3 * time.Minute
	// notifyFanOutPerTick bounds event expansion so enabling a channel does not turn
	// the entire historical backlog into delivery work in one tick.
	notifyFanOutPerTick = 200
	// notifyDefaultDigestMinutes is the default digest period.
	notifyDefaultDigestMinutes = 30
	// notifyUnlimitedBurstPerTick caps work even when a channel has no rate limit,
	// preventing thousands of findings on an unlimited channel from blocking
	// a single loop iteration for an excessive duration.
	notifyUnlimitedBurstPerTick = 50
	// notifyMaxSendsPerChannelPerTick limits one channel's sends per iteration.
	//
	// Derive it from the three-minute lease: too many serial sends can leave later
	// rows with expired leases before their delivery finishes. One process serializes
	// Run, but another process sharing the database could reclaim those rows,
	// send duplicates, increment attempts twice, and declare failures while the first
	// process is still sending them.
	//
	// Three minutes divided by a 30-second send timeout gives six with zero margin,
	// so choose five: at most 150 seconds, leaving 30 seconds of lease headroom.
	// TestNotifyTickBudgetFitsWithinLease enforces the relationship whenever
	// notifyLease, notifySendTimeout, or this count changes.
	notifyMaxSendsPerChannelPerTick = 5
	// notifySendTimeout bounds each delivery. Multiplied by the per-tick send count,
	// it must remain below notifyLease, as the lease-budget test verifies.
	notifySendTimeout = 30 * time.Second
)

// notifyBackoff is indexed by attempts already made.
// Its three total attempts, including the first, must match db.MaxNotifyAttempts.
var notifyBackoff = []time.Duration{
	time.Second,
	5 * time.Second,
	30 * time.Second,
}

// Notifier is the vulnerability notification delivery engine.
//
// It runs independently of Scheduler in its own goroutine, started by server.New.
// Its three-second realtime cadence differs from agent-trigger scheduling,
// and a blocked notification must not block agent triggers.
type Notifier struct {
	s  *Server
	pg *db.DB

	// mu protects buckets. Channel counts and contention are low, so one mutex
	// is sufficient without a more granular structure.
	mu      sync.Mutex
	buckets map[int64]*notifyBucket
}

// notifyBucket is a per-channel token bucket.
//
// A reset-per-minute counter permits boundary bursts: twenty messages at the end
// of one window and twenty at the start of the next looks like forty in one second.
// Constant-rate token replenishment avoids that burst pattern.
type notifyBucket struct {
	tokens   float64
	lastFill time.Time
}

func newNotifier(s *Server) *Notifier {
	return &Notifier{s: s, pg: s.m.pg, buckets: map[int64]*notifyBucket{}}
}

// Run loops until context cancellation; server.New starts it once.
func (n *Notifier) Run(ctx context.Context) {
	if n.pg == nil {
		return
	}
	t := time.NewTicker(notifyTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.step(ctx)
		}
	}
}

// step first fans out new events, then sends due deliveries.
//
// Failures are logged without terminating the loop; notification faults must not
// become process failures. Each independent tick naturally retries work.
func (n *Notifier) step(ctx context.Context) {
	if !n.enabled() {
		return
	}
	if _, _, err := n.pg.FanOutPendingEvents(ctx, notifyFanOutPerTick); err != nil {
		log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Event fan-out failed: %v"), err)
		return
	}
	channels, err := n.pg.ListNotificationChannels(ctx)
	if err != nil {
		log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Channel lookup failed: %v"), err)
		return
	}
	baseURL := n.publicBaseURL()
	for _, ch := range channels {
		if !ch.IsEnabled() {
			continue
		}
		// Tokens measure messages/HTTP requests, not findings. Realtime uses one message
		// per finding; a digest combines a whole finding batch into one message and
		// therefore consumes just one token.
		//
		// Both modes acquire tokens before claiming deliveries; reversing that order
		// would spend retry attempts on work subsequently blocked by rate limiting.
		now := time.Now()
		if ch.Mode == db.NotifyModeDigest {
			tokens, claimLimit := digestTickPlan()
			if n.takeTokens(ch.ID, ch.RatePerMin, tokens, now) <= 0 {
				continue
			}
			n.stepDigest(ctx, ch, claimLimit, baseURL)
			continue
		}
		allow := n.takeTokens(ch.ID, ch.RatePerMin, notifyMaxSendsPerChannelPerTick, now)
		if allow <= 0 {
			continue
		}
		n.stepRealtime(ctx, ch, allow, baseURL)
	}
}

// digestTickPlan returns token cost and maximum findings per batch.
//
// These have different units, which is why this decision has its own helper:
//
// tokens counts messages: one digest batch is one HTTP request, so always one.
// rate_per_min therefore limits digest messages per minute, not findings.
// claimLimit counts findings and is constrained by memory, independently of request budget.
//
// Previously, the lease-derived per-tick request budget was passed as batch size
// to make digest mode obey rate_per_min. At twenty messages per minute, a
// three-second tick replenished just one token, so each digest contained one
// finding, degenerating into realtime delivery with a digest heading.
// db.MaxDigestBatchSize was then unreachable.
//
// Existing end-to-end tests passed a large limit directly to stepDigest and
// missed the calculation in step. Keep the decision here, covered directly by
// TestDigestTickPlanDecouplesBatchSizeFromSendBudget.
func digestTickPlan() (tokens, claimLimit int) {
	return 1, db.MaxDigestBatchSize
}

// stepRealtime claims and sends one message per finding for this channel.
func (n *Notifier) stepRealtime(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	deliveries, err := n.pg.ClaimRealtimeDeliveries(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Realtime delivery claim failed channel=%d: %v"), ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(locale.Text(locale.FromContext(ctx), "Channel kind %q is not registered"), ch.Kind))
		return
	}
	for _, dl := range deliveries {
		msg, err := n.renderSingle(ctx, dl, baseURL)
		if err != nil {
			// Rendering failures are local-data failures; retries will not repair them.
			_ = n.pg.FailDeliveries(ctx, []int64{dl.ID}, notify.ErrorMessage(locale.FromContext(ctx), err))
			continue
		}
		n.send(ctx, channel, cfg, msg, []*db.NotificationDelivery{dl})
	}
}

// stepDigest combines due pending deliveries into one digest message.
func (n *Notifier) stepDigest(ctx context.Context, ch *db.NotificationChannel, allow int, baseURL string) {
	window := n.digestInterval()
	due, err := n.pg.DigestBatchDue(ctx, ch.ID, window)
	if err != nil {
		log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Digest batch check failed channel=%d: %v"), ch.ID, err)
		return
	}
	if !due {
		return
	}
	deliveries, err := n.pg.ClaimDigestBatch(ctx, ch.ID, allow, notifyLease)
	if err != nil {
		log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Digest batch claim failed channel=%d: %v"), ch.ID, err)
		return
	}
	if len(deliveries) == 0 {
		return
	}
	channel, cfg, ok := n.adapt(ch)
	if !ok {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), fmt.Sprintf(locale.Text(locale.FromContext(ctx), "Channel kind %q is not registered"), ch.Kind))
		return
	}
	msg, included, err := n.renderBatch(ctx, deliveries, baseURL, int(window.Minutes()))
	if err != nil {
		_ = n.pg.FailDeliveries(ctx, deliveryIDs(deliveries), notify.ErrorMessage(locale.FromContext(ctx), err))
		return
	}
	// Explicitly fail snapshots omitted from the message. Otherwise they are absent
	// from both included and failure lists, miss the sent-state update, and remain
	// sending until their leases expire and they are reclaimed indefinitely.
	if skipped := excludeDeliveries(deliveries, included); len(skipped) > 0 {
		reason := locale.Text(locale.FromContext(ctx), "Event snapshot cannot be parsed; this finding cannot be rendered as a message")
		if fErr := n.pg.FailDeliveries(ctx, deliveryIDs(skipped), reason); fErr != nil {
			log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Could not mark invalid-snapshot deliveries failed channel=%s ids=%v: %v"), ch.Kind, deliveryIDs(skipped), fErr)
		}
		log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Skipped %d deliveries with invalid snapshots channel=%d"), len(skipped), ch.ID)
	}
	// Only included deliveries go to send: included[i] corresponds exactly to msg.Items[i].
	// This maps a channel's first-K-delivered count onto the correct database rows.
	n.send(ctx, channel, cfg, msg, included)
}

// send delivers a message and transitions each delivery according to the result.
//
// A digest batch shares one send result: delivered or retried together. Retrying
// arbitrary individual members would break the semantics of the combined message.
//
// The exception is channel-length segmentation: if only the first K items fit,
// defer the rest to a later batch rather than marking them sent. Otherwise omitted
// findings would disappear from both the outgoing message and failure records.
func (n *Notifier) send(ctx context.Context, channel notify.Channel, cfg map[string]any, msg notify.Message, deliveries []*db.NotificationDelivery) {
	ctx = locale.WithLang(ctx, locale.Resolve(msg.Language))
	// Bound each send so one stalled channel cannot block the remaining channels indefinitely.
	sendCtx, cancel := context.WithTimeout(ctx, notifySendTimeout)
	defer cancel()
	delivered, err := channel.Send(sendCtx, cfg, msg)
	if err == nil && delivered > 0 {
		if delivered > len(deliveries) {
			// A delivered count above submitted count indicates a rendering bug. Log it
			// and treat all as delivered rather than corrupting row state.
			log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Channel reported %d delivered but only %d were submitted channel=%s; treating all as delivered"),
				delivered, len(deliveries), channel.Kind())
			delivered = len(deliveries)
		}
		sent, rest := deliveries[:delivered], deliveries[delivered:]
		if err := n.pg.MarkDeliveriesSent(ctx, deliveryIDs(sent)); err != nil {
			log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Could not mark deliveries sent channel=%s ids=%v: %v"), channel.Kind(), deliveryIDs(sent), err)
		}
		if len(rest) > 0 {
			// Length-limited remainder goes back to the queue for the next tick immediately.
			// Use DeferDeliveries, not RescheduleDeliveries: segmentation is not failure,
			// and must refund the optimistic attempt increment taken during claim.
			if err := n.pg.DeferDeliveries(ctx, deliveryIDs(rest),
				fmt.Sprintf(locale.Text(locale.FromContext(ctx), "Channel length limit reached; delivered the first %d items, remaining items deferred to the next batch"), delivered)); err != nil {
				log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Could not queue remaining segments channel=%s ids=%v: %v"), channel.Kind(), deliveryIDs(rest), err)
			}
		}
		return
	}
	if err == nil {
		// No error but no delivered count is a failure with backoff, avoiding a delivery
		// that is repeatedly claimed without ever reaching a terminal state.
		err = locale.Errorf("Channel did not report a delivered count (delivered=%d)", delivered)
	}

	// Determine failure disposition per row, not from the batch's maximum attempts.
	//
	// Using maxAttempts(deliveries) previously caused an older exhausted delivery
	// with attempts=2 to drag a fresh attempts=1 row in the same batch into failed,
	// permanently losing new findings before they received any retry.
	// Keep each row's retry allowance independent.
	permanent := notify.IsPermanent(err)
	var failIDs, exhaustedIDs []int64
	byDelay := map[time.Duration][]int64{}
	for _, dl := range deliveries {
		switch {
		case permanent:
			failIDs = append(failIDs, dl.ID)
		case dl.Attempts >= db.MaxNotifyAttempts:
			exhaustedIDs = append(exhaustedIDs, dl.ID)
		default:
			delay := notifyBackoff[min(dl.Attempts, len(notifyBackoff)-1)]
			byDelay[delay] = append(byDelay[delay], dl.ID)
		}
	}

	if len(failIDs) > 0 {
		if fErr := n.pg.FailDeliveries(ctx, failIDs, notify.ErrorMessage(locale.FromContext(ctx), err)); fErr != nil {
			log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Could not mark failed status channel=%s ids=%v: %v"), channel.Kind(), failIDs, fErr)
		}
	}
	if len(exhaustedIDs) > 0 {
		reason := fmt.Sprintf(locale.Text(locale.FromContext(ctx), "Still failed after %d attempts: %s"), db.MaxNotifyAttempts, err)
		if fErr := n.pg.FailDeliveries(ctx, exhaustedIDs, reason); fErr != nil {
			log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Could not mark failed status channel=%s ids=%v: %v"), channel.Kind(), exhaustedIDs, fErr)
		}
	}
	// Group reschedules by delay. Only three backoff tiers exist, avoiding hundreds
	// of individual UPDATE round trips for a large batch.
	for delay, group := range byDelay {
		if rErr := n.pg.RescheduleDeliveries(ctx, group, delay, notify.ErrorMessage(locale.FromContext(ctx), err)); rErr != nil {
			log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Delivery rescheduling failed channel=%s ids=%v: %v"), channel.Kind(), group, rErr)
		}
	}
	if len(failIDs)+len(exhaustedIDs) > 0 {
		log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Delivery failed channel=%d kind=%s permanent=%d exhausted=%d pending_retry=%d: %s"),
			deliveries[0].ChannelID, channel.Kind(), len(failIDs), len(exhaustedIDs), len(byDelay), err)
	}
}

// excludeDeliveries selects pointers in all but not keep, identifying omitted
// message items that need explicit failure handling rather than ambiguous state.
func excludeDeliveries(all, keep []*db.NotificationDelivery) []*db.NotificationDelivery {
	inKeep := make(map[*db.NotificationDelivery]bool, len(keep))
	for _, dl := range keep {
		inKeep[dl] = true
	}
	var out []*db.NotificationDelivery
	for _, dl := range all {
		if !inKeep[dl] {
			out = append(out, dl)
		}
	}
	return out
}

// adapt resolves the channel implementation and parses its configuration.
// ok=false means an unregistered kind: fail permanently rather than retry forever.
func (n *Notifier) adapt(ch *db.NotificationChannel) (notify.Channel, map[string]any, bool) {
	channel, ok := notify.Get(ch.Kind)
	if !ok {
		return nil, nil, false
	}
	var cfg map[string]any
	if len(ch.Config) > 0 {
		// On malformed configuration use an empty map so channel.Validate reports the
		// missing fields, which is more actionable than an opaque JSON parsing error.
		_ = json.Unmarshal(ch.Config, &cfg)
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	return channel, cfg, true
}

// renderSingle renders one finding notification.
func (n *Notifier) renderSingle(ctx context.Context, dl *db.NotificationDelivery, baseURL string) (notify.Message, error) {
	snap, err := parseSnapshot(dl)
	if err != nil {
		return notify.Message{}, err
	}
	item, err := n.itemFor(ctx, snap, baseURL)
	if err != nil {
		return notify.Message{}, err
	}
	return notify.Message{Items: []notify.Item{item}, HomeURL: baseURL, Language: taskLanguage(n.pg, strconv.FormatInt(snap.TaskID, 10))}, nil
}

// renderBatch parses each snapshot independently; one malformed item is skipped
// rather than preventing the whole digest from being rendered.
//
// included must correspond exactly, item by item, with msg.Items.
// The sender marks the first K deliveries sent according to the channel's count.
// Leaving a skipped snapshot in included would misalign indexes, marking bad
// rows sent and good rows unsent.
// The caller explicitly fails omitted items; see stepDigest.
func (n *Notifier) renderBatch(ctx context.Context, deliveries []*db.NotificationDelivery, baseURL string, windowMinutes int) (notify.Message, []*db.NotificationDelivery, error) {
	items := make([]notify.Item, 0, len(deliveries))
	included := make([]*db.NotificationDelivery, 0, len(deliveries))
	for _, dl := range deliveries {
		snap, err := parseSnapshot(dl)
		if err != nil {
			// Invalid snapshots enter neither the message nor included; the caller marks
			// them failed rather than silently treating them as delivered.
			log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Skipping invalid snapshot in digest delivery=%d: %v"), dl.ID, err)
			continue
		}
		item, err := n.itemFor(ctx, snap, baseURL)
		if err != nil {
			return notify.Message{}, nil, err
		}
		items = append(items, item)
		included = append(included, dl)
	}
	if len(items) == 0 {
		return notify.Message{}, nil, locale.Errorf("All %d deliveries in the digest batch had invalid snapshots", len(deliveries))
	}
	return notify.Message{
		Items:         items,
		Language:      locale.ServerDefault(),
		Batch:         true,
		WindowMinutes: windowMinutes,
		HomeURL:       baseURL,
	}, included, nil
}

// itemFor renders a snapshot, resolving asset names and the finding-detail link.
func (n *Notifier) itemFor(ctx context.Context, snap notify.Snapshot, baseURL string) (notify.Item, error) {
	assets, err := n.pg.NotificationAssetNames(ctx, snap.AssetIDs)
	if err != nil {
		// Asset-name lookup failure must not block notifications: a missing asset line
		// is preferable to losing the entire notification.
		log.Printf(locale.Text(locale.FromContext(ctx), "[notify] Asset-name lookup failed finding=%d: %v"), snap.FindingID, err)
	}
	item := notify.Item{
		FindingID:  snap.FindingID,
		Name:       snap.Name,
		VulnClass:  snap.VulnClass,
		Severity:   snap.Severity,
		Summary:    snap.Summary,
		Assets:     assets,
		FromStatus: snap.FromStatus,
		ToStatus:   snap.ToStatus,
	}
	if baseURL != "" {
		// The finding detail route is web/src/app/(main)/function/findings/detail/page.tsx;
		// it reads the finding ID from the id query parameter.
		item.DetailURL = fmt.Sprintf("%s/function/findings/detail?id=%d", baseURL, snap.FindingID)
	}
	return item, nil
}

// takeTokens takes at most want tokens and returns the actual count.
//
// One token is one message/HTTP request. Realtime requests its send count;
// digest requests one for the whole batch.
//
// Capacity is the per-minute limit, replenished continuously. A nonpositive rate
// returns a bounded generous allowance so unlimited backlog cannot stall one tick.
//
// want prevents draining tokens beyond the caller's per-tick budget. Otherwise
// unused tokens would disappear, burst capacity would never accumulate, and
// even an empty queue would consume the channel's allowance.
func (n *Notifier) takeTokens(channelID int64, ratePerMin, want int, now time.Time) int {
	if want <= 0 {
		return 0
	}
	if ratePerMin <= 0 {
		return min(want, notifyUnlimitedBurstPerTick)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	b := n.buckets[channelID]
	if b == nil {
		b = &notifyBucket{tokens: float64(ratePerMin), lastFill: now}
		n.buckets[channelID] = b
	}
	// Replenish using actual elapsed time at ratePerMin/60 tokens per second.
	if elapsed := now.Sub(b.lastFill).Seconds(); elapsed > 0 {
		b.tokens = minF(float64(ratePerMin), b.tokens+elapsed*float64(ratePerMin)/60)
		b.lastFill = now
	}
	// Add a tiny epsilon before flooring: floating-point replenishment may make
	// 0.5+0.5 slightly less than one and incorrectly yield zero whole tokens.
	// 1e-9 is far smaller than one token and does not permit meaningful overdraft.
	take := min(int(b.tokens+1e-9), want)
	if take <= 0 {
		return 0
	}
	b.tokens -= float64(take)
	return take
}

// enabled reads the global notification switch.
func (n *Notifier) enabled() bool {
	return n.pg.GetBool(settingNotifyEnabled, true)
}

// publicBaseURL reads the external return-link base and trims trailing slashes.
func (n *Notifier) publicBaseURL() string {
	v, ok, err := n.pg.GetSetting(settingNotifyPublicBaseURL)
	if err != nil || !ok {
		return ""
	}
	return trimTrailingSlash(v)
}

// digestInterval falls back to the default for missing or invalid settings.
func (n *Notifier) digestInterval() time.Duration {
	v, ok, err := n.pg.GetSetting(settingNotifyDigestMinutes)
	if err != nil || !ok {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	m := 0
	if _, err := fmt.Sscanf(v, "%d", &m); err != nil || m <= 0 {
		return time.Duration(notifyDefaultDigestMinutes) * time.Minute
	}
	return time.Duration(m) * time.Minute
}

// parseSnapshot decodes the event snapshot associated with a delivery.
func parseSnapshot(dl *db.NotificationDelivery) (notify.Snapshot, error) {
	var snap notify.Snapshot
	if len(dl.Snapshot) == 0 {
		return snap, locale.Errorf("Delivery %d has an empty event snapshot", dl.ID)
	}
	if err := json.Unmarshal(dl.Snapshot, &snap); err != nil {
		return snap, locale.Errorf("Parse event snapshot for delivery %d: %w", dl.ID, err)
	}
	if snap.Kind == "" {
		// The event row's kind is authoritative; the snapshot may come from an older version.
		snap.Kind = dl.EventKind
	}
	return snap, nil
}

func deliveryIDs(deliveries []*db.NotificationDelivery) []int64 {
	out := make([]int64, 0, len(deliveries))
	for _, dl := range deliveries {
		out = append(out, dl.ID)
	}
	return out
}

func trimTrailingSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
