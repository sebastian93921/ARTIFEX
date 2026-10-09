package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// allowLocalTargets defaults to blocking loopback/link-local destinations, including local
// administration ports and cloud metadata at 169.254.169.254. A compromised/shared administrator
// session could read response snippets through last_error and delivery history, creating a partial-
// read SSRF primitive. Local SMTP relays are legitimate deployments, so ARTIFEX_NOTIFY_ALLOW_LOCAL=1
// explicitly opts in. Exporting AllowLocalTargetsEnv also lets package/server tests enable loopback
// fake receivers.
const AllowLocalTargetsEnv = "ARTIFEX_NOTIFY_ALLOW_LOCAL"

func allowLocalTargets() bool {
	v := strings.TrimSpace(os.Getenv(AllowLocalTargetsEnv))
	return v == "1" || strings.EqualFold(v, "true")
}

// isBlockedDialIP blocks loopback, link-local (including metadata), unspecified, and multicast
// addresses. RFC1918 networks remain allowed for legitimate internal Mattermost and SMTP deployments;
// this intentionally balances sensitive-target protection with deployability.
func isBlockedDialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// Normalize IPv4-mapped IPv6 such as ::ffff:127.0.0.1 before checking to prevent bypasses.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// blockInternalDial is the transport dialer's Control hook and checks the actual connection
// destination. Save-time validation alone misses DNS rebinding and redirects; same-host redirects
// remain possible even though cross-host redirects are forbidden.
func blockInternalDial(_, address string, _ syscall.RawConn) error {
	if allowLocalTargets() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return locale.Errorf("Cannot resolve destination %q", host)
	}
	if isBlockedDialIP(ip) {
		return locale.Errorf("Delivery to local/link-local address %s is blocked (set %s=1 only if local delivery is required)", ip, AllowLocalTargetsEnv)
	}
	return nil
}

// notifyTransport clones the default transport and adds only the dial guard, retaining pooling,
// HTTP/2, timeout, and proxy tuning.
var notifyTransport = func() *http.Transport {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Transport{}
	}
	clone := t.Clone()
	clone.DialContext = (&net.Dialer{Timeout: 10 * time.Second, Control: blockInternalDial}).DialContext
	return clone
}()

// httpClient is shared by all channels. It deliberately avoids the server's target-testing
// GlobalProxy, often an unstable tunnel, so notification availability is independent of target-network
// failures. Use a 15-second timeout. Reject cross-host redirects because credentials reside in query
// strings or paths; fixed delivery endpoints should not change hosts. Same-host normalization
// redirects remain allowed.
var httpClient = &http.Client{
	Timeout:   15 * time.Second,
	Transport: notifyTransport,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return locale.NewError("Too many redirects")
		}
		if len(via) > 0 && req.URL.Host != via[0].URL.Host {
			return locale.Errorf("Cross-host redirect blocked (%s → %s)", via[0].URL.Host, req.URL.Host)
		}
		return nil
	},
}

// respBodyLimit bounds abnormal responses; delivery history needs only the code and a short
// explanation.
const respBodyLimit = 8 << 10

// doJSON sends a request and returns a bounded response. Nil payload means an empty body; custom
// headers are attached verbatim. Network failures and HTTP 5xx/408/429 remain retryable; other 4xx
// failures are permanent to avoid repeated futile requests.
func doJSON(ctx context.Context, method, url string, headers map[string]string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			// Serialization failures are local configuration/type errors and cannot improve through retries.
			return nil, Permanent(locale.Errorf("Failed to build request body: %w", err))
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		// Invalid URLs are permanent configuration errors. Do not expose url.Parse's full credential-bearing
		// address.
		return nil, Permanent(locale.Errorf("Invalid request URL: %s", redactedTargetArgument(url)))
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		// Connection refusal, DNS failures, and timeouts normally warrant backoff retries. Redact transport
		// errors first: http.Client.Do wraps full credential-bearing URLs in *url.Error, which would otherwise
		// leak into stored last_error, delivery history (bypassing masks), logs, and test-send 502 responses.
		return nil, locale.Errorf("Request failed: %s", redactedTransportArgument(err))
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, respBodyLimit))
	if readErr != nil {
		return nil, locale.Errorf("Failed to read response: %w", readErr)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	// Retry 429 rate limits and 408 timeouts; other 4xx responses indicate configuration or permission
	// failures.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
		return nil, locale.Errorf("Remote rate limit or timeout (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if resp.StatusCode >= 500 {
		return nil, locale.Errorf("Remote service error (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	return nil, Permanent(locale.Errorf("Remote server rejected the request (HTTP %d): %s", resp.StatusCode, snippet(raw)))
}

// snippet collapses responses into short single-line error text so whitespace and long bodies do not
// break delivery-history layout.
func snippet(raw []byte) string {
	return OneLine(string(raw), 200)
}

// redactRequestTarget exposes only scheme://host/… and uses a fixed placeholder when parsing fails.
// Credential placement varies: DingTalk/WeCom query parameters, Feishu hook-path suffixes, and
// Telegram bot-path segments. Keeping more requires fragile adapter-specific exceptions; host alone
// supports DNS, connection, and certificate diagnosis while masked configuration suffixes identify the
// bot.
func redactRequestTarget(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(Unparseable URL)"
	}
	return u.Scheme + "://" + u.Host + "/…"
}

// redactTransportError unwraps *url.Error.Err instead of calling Error(), which embeds the URL.
// Structured unwrapping is safer than matching every URL-encoded or escaped representation afterward.
func redactTransportError(err error) string {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		host := ""
		if u, parseErr := url.Parse(uerr.URL); parseErr == nil {
			host = u.Host
		}
		if uerr.Err != nil {
			return fmt.Sprintf("%s %s: %s", uerr.Op, host, uerr.Err)
		}
		return fmt.Sprintf("%s %s: unknown error", uerr.Op, host)
	}
	// Unstructured errors, including redirect-policy errors, can also contain URLs and need redaction.
	return redactURLsInText(err.Error())
}

// redactURLsInText redacts http(s) addresses in unstructured redirect or third-party errors.
// Whitespace and quotes delimit URLs because those characters cannot appear literally in them.
func redactURLsInText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		rest := s[i:]
		if strings.HasPrefix(rest, "http://") || strings.HasPrefix(rest, "https://") {
			end := len(rest)
			if j := strings.IndexAny(rest, " \t\n\"'"); j >= 0 {
				end = j
			}
			b.WriteString(redactRequestTarget(rest[:end]))
			i += end
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
