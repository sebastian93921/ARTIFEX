package notify

import "github.com/sebastian93921/artifex/locale"

// Snapshot defines the notification_events.snapshot JSONB contract. Finding transactions write it;
// server delivery and filters read it. The notification domain owns these semantics while db only
// serializes. Duplicated finding fields preserve the conclusion at event time despite later title,
// severity, or status edits; querying current values could dangerously downplay an originally severe
// event. Fan-out/rendering also avoid joining findings, tasks, and assets.
type Snapshot struct {
	// Event kind: finding_created / finding_status_changed.
	Kind      string  `json:"kind"`
	FindingID int64   `json:"finding_id"`
	TaskID    int64   `json:"task_id"`
	VulnClass string  `json:"vulnclass"`
	Name      string  `json:"name"`
	Severity  string  `json:"severity"`
	Summary   string  `json:"summary"`
	AssetIDs  []int64 `json:"asset_ids"`
	// Populated only for finding_status_changed.
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// Item is one finding prepared for channel rendering.
type Item struct {
	FindingID int64
	Name      string
	VulnClass string
	Severity  string
	Summary   string
	// Assets contains resolved display names such as domains or IPs. The server populates them because
	// this package does not access the database.
	Assets []string
	// DetailURL links to the finding. An empty public_base_url omits the link.
	DetailURL string
	// Status-change fields; when present, render the old-to-new status transition.
	FromStatus string
	ToStatus   string
}

// IsStatusChange reports whether this item describes a status change.
func (i Item) IsStatusChange() bool { return i.FromStatus != "" || i.ToStatus != "" }

// Title prefers the user-supplied name, falls back to vulnclass, and finally uses a placeholder; it
// never returns an empty title.
func (i Item) Title(langs ...locale.Lang) string {
	if i.Name != "" {
		return i.Name
	}
	if i.VulnClass != "" {
		return i.VulnClass
	}
	return locale.Text(locale.First(langs), "(Unnamed finding)")
}

// Message contains the complete content of one channel delivery.
type Message struct {
	// Language controls built-in text only; an omitted value uses the server default.
	Language locale.Lang `json:"-"`
	// One item for an individual notification, or a full digest batch. Callers must supply at least one
	// item.
	Items []Item
	// Batch selects digest rendering with a different title, time window, and count.
	Batch bool
	// WindowMinutes is the configured digest interval, used only for batch wording. Pass it explicitly
	// instead of computing time.Since while rendering to keep output deterministic and testable.
	WindowMinutes int
	// HomeURL is the dashboard address from public_base_url; empty means no dashboard link.
	HomeURL string
}
