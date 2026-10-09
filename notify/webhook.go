package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/sebastian93921/artifex/locale"
	"net/http"
	"strings"
	"text/template"
	"time"
)

// webhookChannel supports custom URLs, methods, headers, and JSON templates, covering Slack,
// Mattermost, Discord, and private receivers without separate adapters.
type webhookChannel struct{}

func (webhookChannel) Kind() string { return KindWebhook }

// Generic webhooks have no platform-wide rate limit; zero leaves operators to configure the receiver's
// capacity.
func (webhookChannel) DefaultRatePerMin() int { return 0 }

// Mask URLs and headers because both commonly contain authentication tokens. Editing a header requires
// resubmitting the entire header object; masks mean preserve. This deliberate inconvenience prevents
// credential exposure to browsers.
func (webhookChannel) SecretKeys() []string { return []string{"url", "headers"} }

// Changing url requires an explicit headers choice; otherwise stored Authorization headers would be
// sent to the new destination, bypassing masking.
func (webhookChannel) DestinationKeys() []string { return []string{"url"} }

// webhookDefaultTemplate supplies straightforward JSON for receivers that simply ingest and store
// notification objects.
const webhookDefaultTemplate = `{
  "title": {{json .Title}},
  "batch": {{.Batch}},
  "count": {{.Count}},
  "items": [
{{- range $i, $it := .Items}}
{{- if $i}},{{end}}
    {
      "finding_id": {{$it.FindingID}},
      "name": {{json $it.Name}},
      "vulnclass": {{json $it.VulnClass}},
      "severity": {{json $it.Severity}},
      "summary": {{json $it.Summary}},
      "assets": {{json $it.Assets}},
      "detail_url": {{json $it.DetailURL}}
    }
{{- end}}
  ]
}`

// webhookTemplateData is the context exposed to user templates.
type webhookTemplateData struct {
	Title   string
	Batch   bool
	Count   int
	Items   []webhookItem
	HomeURL string
	// SentAt records this delivery time in RFC3339 format for receivers.
	SentAt string
}

type webhookItem struct {
	FindingID     int64
	Name          string
	VulnClass     string
	Severity      string
	SeverityLabel string
	Summary       string
	Assets        []string
	DetailURL     string
	FromStatus    string
	ToStatus      string
	// StatusLabel describes a status transition; it is empty for other events.
	StatusLabel string
}

func (webhookChannel) Validate(cfg map[string]any) error {
	raw := cfgString(cfg, "url")
	if raw == "" {
		return locale.NewError("Destination URL is required")
	}
	if err := validateHTTPURL(raw); err != nil {
		return locale.Errorf("Invalid destination URL: %w", err)
	}
	if m := strings.ToUpper(cfgString(cfg, "method")); m != "" && m != http.MethodGet && m != http.MethodPost && m != http.MethodPut && m != http.MethodPatch {
		return locale.Errorf("Unsupported method %s (use GET/POST/PUT/PATCH)", m)
	}
	if tpl := cfgString(cfg, "body_template"); tpl != "" {
		if _, err := parseWebhookTemplate(tpl); err != nil {
			return locale.Errorf("Invalid request body template syntax: %w", err)
		}
	}
	return nil
}

func (c webhookChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	m = m.withContextLanguage(ctx)
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	method := strings.ToUpper(cfgString(cfg, "method"))
	if method == "" {
		method = http.MethodPost
	}

	// GET sends no body. Encoding content into queries is outside template scope and GET semantics; use
	// GET only for trigger-on-request hooks.
	var payload any
	if method != http.MethodGet {
		body, err := renderWebhookBody(cfgString(cfg, "body_template"), m)
		if err != nil {
			return 0, Permanent(err)
		}
		// Send rendered JSON as json.RawMessage to avoid wrapping the user's structure in a doubly escaped
		// JSON string.
		if !json.Valid([]byte(body)) {
			return 0, Permanent(locale.NewError("Rendered request body template is not valid JSON"))
		}
		payload = json.RawMessage(body)
	}

	headers := cfgMap(cfg, "headers")
	if ct := cfgString(cfg, "content_type"); ct != "" {
		// Apply the explicit setting after custom headers so it takes precedence.
		if headers == nil {
			headers = map[string]string{}
		}
		headers["Content-Type"] = ct
	}
	if _, err := doJSON(ctx, method, cfgString(cfg, "url"), headers, payload); err != nil {
		return 0, err
	}
	// Generic webhooks do not truncate user-controlled bodies, so the entire batch counts as delivered.
	return len(m.Items), nil
}

// renderWebhookBody renders the user template or default request body.
func renderWebhookBody(tpl string, m Message) (string, error) {
	if strings.TrimSpace(tpl) == "" {
		tpl = webhookDefaultTemplate
	}
	t, err := parseWebhookTemplate(tpl)
	if err != nil {
		return "", locale.Errorf("Invalid request body template syntax: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, newWebhookTemplateData(m)); err != nil {
		return "", locale.Errorf("Failed to render request body template: %w", err)
	}
	return buf.String(), nil
}

// parseWebhookTemplate uses missingkey=zero for missing map entries. The actual context is a struct;
// empty/nil Items must remain safe to range over.
func parseWebhookTemplate(tpl string) (*template.Template, error) {
	return template.New("body").Funcs(webhookTemplateFuncs).Option("missingkey=zero").Parse(tpl)
}

// webhookTemplateFuncs defines helpers available to templates.
var webhookTemplateFuncs = template.FuncMap{
	// json serializes arbitrary values safely. It is essential: direct title interpolation produces
	// invalid JSON when findings contain quotes or newlines, causing receiver parse errors that obscure
	// the underlying title issue.
	"json": func(v any) (string, error) {
		raw, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	},
	// jsons escapes a JSON fragment for embedding inside another JSON string value.
	"jsons": func(v any) (string, error) {
		raw, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		quoted, err := json.Marshal(string(raw))
		if err != nil {
			return "", err
		}
		// Remove outer quotes so callers control whether to include them.
		return string(quoted[1 : len(quoted)-1]), nil
	},
}

func newWebhookTemplateData(m Message) webhookTemplateData {
	d := webhookTemplateData{
		Title:   markdownTitle(m),
		Batch:   m.Batch,
		Count:   len(m.Items),
		HomeURL: m.HomeURL,
		SentAt:  time.Now().Format(time.RFC3339),
		Items:   make([]webhookItem, 0, len(m.Items)),
	}
	for _, it := range m.Items {
		wi := webhookItem{
			FindingID:     it.FindingID,
			Name:          it.Name,
			VulnClass:     it.VulnClass,
			Severity:      it.Severity,
			SeverityLabel: SeverityLabel(it.Severity, locale.Resolve(m.Language)),
			Summary:       it.Summary,
			Assets:        append([]string{}, it.Assets...),
			DetailURL:     it.DetailURL,
			FromStatus:    it.FromStatus,
			ToStatus:      it.ToStatus,
		}
		if it.IsStatusChange() {
			wi.StatusLabel = StatusLabel(it.FromStatus, locale.Resolve(m.Language)) + " → " + StatusLabel(it.ToStatus, locale.Resolve(m.Language))
		}
		d.Items = append(d.Items, wi)
	}
	return d
}
