package notify

import (
	"context"
	"encoding/json"
	"github.com/sebastian93921/artifex/locale"
)

// weComMarkdownLimit is the hard byte limit, not a character limit. It is the tightest of the six
// channels and motivates TruncateBytes.
const weComMarkdownLimit = 4096

// weComChannel implements WeCom group bots. The URL key is the entire credential; signing is
// unsupported. Markdown exceeding 4096 bytes is rejected rather than truncated, so clients must
// truncate multibyte content. Enforce the twenty-message/minute limit client-side too.
type weComChannel struct{}

func (weComChannel) Kind() string { return KindWeCom }

func (weComChannel) DefaultRatePerMin() int { return 20 }

// WeCom's only credential is its webhook URL key; no separate signing fields require masking.
func (weComChannel) SecretKeys() []string { return []string{"webhook"} }

// The webhook is both destination and credential, so changing it cannot retain a separate old secret.
func (weComChannel) DestinationKeys() []string { return []string{"webhook"} }

func (weComChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return locale.NewError("Webhook URL is required")
	}
	if err := validateHTTPURL(hook); err != nil {
		return locale.Errorf("Invalid webhook URL: %w", err)
	}
	return nil
}

func (c weComChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	m = m.withContextLanguage(ctx)
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	// Digests can easily exceed 4096 bytes. Truncate client-side to deliver the entries that fit instead
	// of having the platform reject the whole batch.
	content, kept := markdownBody(m, weComMarkdownLimit)
	payload := map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]any{"content": content},
	}
	raw, err := doJSON(ctx, "POST", cfgString(cfg, "webhook"), nil, payload)
	if err != nil {
		return 0, err
	}
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, locale.Errorf("Failed to parse WeCom response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 45009 means rate limiting and is retryable after the window advances. It indicates an overly
		// aggressive rate_per_min; lower the configured rate instead of relying on backoff alone.
		if res.ErrCode == 45009 {
			return 0, locale.Errorf("WeCom rate limit %d: %s", res.ErrCode, res.ErrMsg)
		}
		// 93000 means an invalid webhook key, a permanent failure that retries cannot repair.
		return 0, Permanent(locale.Errorf("WeCom returned error %d: %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}
