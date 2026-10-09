package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"github.com/sebastian93921/artifex/locale"
	"net"
	"net/url"
	"strconv"
	"time"
)

// dingTalkChannel implements DingTalk custom bots. Bots allow 20 messages/minute and may silently drop
// excess messages even with HTTP 200, so enforce limits client-side. Security options are signing,
// keywords, or IP allowlists; signing is independent of message content, so this adapter supports
// signing and unsecured webhooks. Success and failure both use HTTP 200; inspect errcode to avoid
// false delivery receipts.
type dingTalkChannel struct{}

func (dingTalkChannel) Kind() string { return KindDingTalk }

func (dingTalkChannel) DefaultRatePerMin() int { return 20 }

// DingTalk webhook URLs contain access_token credentials, so mask the entire URL.
func (dingTalkChannel) SecretKeys() []string { return []string{"webhook", "secret"} }

// Changing the webhook destination requires explicitly supplying the signing-secret choice again.
func (dingTalkChannel) DestinationKeys() []string { return []string{"webhook"} }

func (dingTalkChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return locale.NewError("Webhook URL is required")
	}
	if err := validateHTTPURL(hook); err != nil {
		return locale.Errorf("Invalid webhook URL: %w", err)
	}
	return nil
}

// Send uses an ActionCard with a button for a single linked finding; otherwise it sends Markdown.
func (c dingTalkChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	m = m.withContextLanguage(ctx)
	hook := cfgString(cfg, "webhook")
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	endpoint, err := dingTalkSignedURL(hook, cfgString(cfg, "secret"), time.Now())
	if err != nil {
		return 0, Permanent(err)
	}

	title := markdownTitle(m)
	// DingTalk does not specify a clear Markdown byte limit; still bound the body to guard against
	// oversized evidence.
	text, kept := markdownBody(m, 20000)

	var payload any
	if !m.Batch && len(m.Items) == 1 && m.Items[0].DetailURL != "" {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          title,
				"text":           text,
				"btnOrientation": "0",
				"singleTitle":    locale.Text(locale.Resolve(m.Language), "View details"),
				"singleURL":      m.Items[0].DetailURL,
			},
		}
	} else {
		payload = map[string]any{
			"msgtype":  "markdown",
			"markdown": map[string]any{"title": title, "text": text},
		}
	}

	raw, err := doJSON(ctx, "POST", endpoint, nil, payload)
	if err != nil {
		return 0, err
	}
	// DingTalk embeds business failures in HTTP 200 responses.
	var res struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, locale.Errorf("Failed to parse DingTalk response: %w (%s)", err, snippet(raw))
	}
	if res.ErrCode != 0 {
		// 301000 means signature failure and 310000 means keyword mismatch. Both are configuration errors that
		// retries cannot repair.
		return 0, Permanent(locale.Errorf("DingTalk returned error %d: %s", res.ErrCode, res.ErrMsg))
	}
	return kept, nil
}

// dingTalkSignedURL adds timestamp and sign using the official algorithm: sign timestamp + newline +
// secret with HMAC-SHA256 keyed by secret, then base64 and URL-encode. The timestamp is milliseconds.
// An empty secret leaves the URL unchanged for unsigned bots.
func dingTalkSignedURL(hook, secret string, now time.Time) (string, error) {
	if secret == "" {
		return hook, nil
	}
	ts := strconv.FormatInt(now.UnixMilli(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	u, err := url.Parse(hook)
	if err != nil {
		// Do not expose url.Parse errors: they include the full URL and access_token.
		return "", locale.Errorf("Failed to parse webhook URL: %s", redactedTargetArgument(hook))
	}
	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// validateHTTPURL validates syntax, supported schemes, and literal local IPs. Errors must be redacted:
// url.Parse includes full URLs containing bot tokens, hook IDs, and query credentials, which could
// leak through API errors, stored last_error, logs, and delivery history. Check literal IPs here for
// immediate configuration feedback; hostnames are checked at dial time by blockInternalDial, which
// also prevents DNS rebinding. Restricting schemes avoids unexpected file/gopher behavior.
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return locale.Errorf("Cannot parse URL (%s)", redactedTargetArgument(raw))
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return locale.Errorf("Only http/https are supported; received %q", u.Scheme)
	}
	if u.Host == "" {
		return locale.NewError("Hostname is required")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && isBlockedDialIP(ip) && !allowLocalTargets() {
		return locale.Errorf("Delivery to local/link-local address %s is blocked (set %s=1 only if local delivery is required)", ip, AllowLocalTargetsEnv)
	}
	return nil
}
