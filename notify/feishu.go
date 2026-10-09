package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"strconv"
	"time"
)

// feishuChannel implements Feishu/Lark custom bots using interactive cards. Its signature algorithm
// differs from DingTalk (see feishuSign). Nonzero code values signal failures even with HTTP 200.
// Header color templates map severity for quick recognition.
type feishuChannel struct{}

func (feishuChannel) Kind() string { return KindFeishu }

// Feishu custom bots are documented at about 5 requests/second with a 100/minute cap.
func (feishuChannel) DefaultRatePerMin() int { return 100 }

// The webhook URL's final segment identifies the bot and is a credential.
func (feishuChannel) SecretKeys() []string { return []string{"webhook", "secret"} }

// Changing the webhook URL requires an explicit signing-secret choice for the new destination.
func (feishuChannel) DestinationKeys() []string { return []string{"webhook"} }

func (feishuChannel) Validate(cfg map[string]any) error {
	hook := cfgString(cfg, "webhook")
	if hook == "" {
		return locale.NewError("Webhook URL is required")
	}
	if err := validateHTTPURL(hook); err != nil {
		return locale.Errorf("Invalid webhook URL: %w", err)
	}
	return nil
}

func (c feishuChannel) Send(ctx context.Context, cfg map[string]any, m Message) (int, error) {
	m = m.withContextLanguage(ctx)
	if err := c.Validate(cfg); err != nil {
		return 0, Permanent(err)
	}
	card, kept := feishuCard(m)
	payload := map[string]any{
		"msg_type": "interactive",
		"card":     card,
	}
	// Signature fields are siblings of message fields and appear only when a secret is configured.
	if secret := cfgString(cfg, "secret"); secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		payload["timestamp"] = ts
		payload["sign"] = feishuSign(ts, secret)
	}
	raw, err := doJSON(ctx, "POST", cfgString(cfg, "webhook"), nil, payload)
	if err != nil {
		return 0, err
	}
	var res struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		// Support alternate field names used by some Feishu webhook versions.
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return 0, locale.Errorf("Failed to parse Feishu response: %w (%s)", err, snippet(raw))
	}
	if res.Code != 0 {
		return 0, Permanent(locale.Errorf("Feishu returned error %d: %s", res.Code, res.Msg))
	}
	if res.StatusCode != 0 {
		return 0, Permanent(locale.Errorf("Feishu returned error %d: %s", res.StatusCode, res.StatusMessage))
	}
	return kept, nil
}

// feishuSign follows the official example: hmac.new(string_to_sign.encode(), digestmod=sha256). The
// key is timestamp + newline + secret, and the message is empty. Using secret as key and stringToSign
// as message is DingTalk's opposite algorithm and causes Feishu signature failure 19021.
func feishuSign(timestamp, secret string) string {
	stringToSign := timestamp + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// feishuSeverityTemplate maps severity to header colors. Unknown values use grey rather than low-
// severity blue.
func feishuSeverityTemplate(severity string) string {
	switch severity {
	case "critical":
		return "red"
	case "high":
		return "orange"
	case "medium":
		return "yellow"
	case "low":
		return "blue"
	default:
		return "grey"
	}
}

// feishuMaxCardBytes is a conservative card limit below the platform cap, allowing for JSON envelope
// overhead; oversized cards are rejected entirely.
const feishuMaxCardBytes = 24000

// feishuCard returns the interactive card and the number of included items. As with markdownBody, only
// included items may receive delivery receipts.
func feishuCard(m Message) (map[string]any, int) {
	elements := []any{}
	kept := 0
	if m.Batch {
		// Pack complete items before building the header so its remaining-item count reflects actual capacity.
		kept = packItemCount(m.Items, feishuMaxCardBytes, markdownReservedBytes, "", byteSize, func(it Item, idx int) string {
			return feishuBatchLine(it, idx+1, locale.Resolve(m.Language))
		})
		items := m.Items[:kept]
		elements = append(elements, feishuMarkdownDiv(markdownBatchIntro(m, items, len(m.Items))))
		for i, it := range items {
			elements = append(elements, feishuMarkdownDiv(feishuBatchLine(it, i+1, locale.Resolve(m.Language))))
		}
		if m.HomeURL != "" {
			elements = append(elements, feishuButton(locale.Text(locale.Resolve(m.Language), "View all in ARTEX"), m.HomeURL))
		}
	} else if len(m.Items) > 0 {
		kept = 1
		it := m.Items[0]
		elements = append(elements, feishuMarkdownDiv(feishuItemLines(it, locale.Resolve(m.Language))))
		if it.DetailURL != "" {
			elements = append(elements, feishuButton(locale.Text(locale.Resolve(m.Language), "View details"), it.DetailURL))
		}
	}

	card := map[string]any{
		"config":   map[string]any{"wide_screen_mode": true},
		"header":   map[string]any{"title": map[string]any{"tag": "plain_text", "content": markdownTitle(m)}},
		"elements": elements,
	}
	if len(m.Items) > 0 {
		card["header"].(map[string]any)["template"] = feishuSeverityTemplate(m.Items[0].Severity)
	}
	return card, kept
}

func feishuMarkdownDiv(content string) map[string]any {
	return map[string]any{"tag": "div", "text": map[string]any{"tag": "lark_md", "content": content}}
}

func feishuButton(label, url string) map[string]any {
	return map[string]any{
		"tag": "action",
		"actions": []any{map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "lark_md", "content": label},
			"url":  url,
			"type": "primary",
		}},
	}
}

// feishuItemLines renders lark_md. Like Markdown, it parses links and emphasis, so normalize and
// escape every external field through markdownText to prevent injected clickable links.
func feishuItemLines(it Item, langs ...locale.Lang) string {
	out := fmt.Sprintf("**%s · %s**", SeverityLabel(it.Severity, locale.First(langs)), markdownText(it.Title(locale.First(langs)), 0))
	if it.IsStatusChange() {
		out += fmt.Sprintf(locale.Text(locale.First(langs), "\n**Status change**: %s → %s"),
			markdownText(StatusLabel(it.FromStatus, locale.First(langs)), 0), markdownText(StatusLabel(it.ToStatus, locale.First(langs)), 0))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title(locale.First(langs)) {
		out += fmt.Sprintf(locale.Text(locale.First(langs), "\n**Type**: %s"), markdownText(it.VulnClass, 0))
	}
	if a := assetLine(it.Assets, maxAssetsShown, locale.First(langs)); a != "" {
		out += fmt.Sprintf(locale.Text(locale.First(langs), "\n**Assets**: %s"), markdownText(a, 0))
	}
	if it.Summary != "" {
		if s := markdownText(it.Summary, maxSummaryRunes); s != "" {
			out += fmt.Sprintf(locale.Text(locale.First(langs), "\n**Summary**: %s"), s)
		}
	}
	return out
}

// feishuBatchLine renders one digest card entry.
func feishuBatchLine(it Item, index int, langs ...locale.Lang) string {
	line := fmt.Sprintf("**%d. %s · %s**", index, SeverityLabel(it.Severity, locale.First(langs)), markdownText(it.Title(locale.First(langs)), 0))
	if a := assetLine(it.Assets, maxAssetsShown, locale.First(langs)); a != "" {
		line += " — " + markdownText(a, 0)
	}
	return line
}
