// Package agent wires real LLM-driven planner and work agents (on top of the
// agent-core SDK) to the PostgreSQL graphs. See the architecture design documentation.
// §4.3 (planner) and §4.4 (work agent).
//
// Provider configuration is read from the environment so the system runs with
// any Anthropic- or OpenAI-format endpoint. If no key is configured, FromEnv
// returns ok=false and the exploration engine stays idle (an LLM is required).
package agent

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Autumn-27/artex/locale"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/llmrec"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/compaction"
	"github.com/Autumn-27/norma/llm"
	acperm "github.com/Autumn-27/norma/permission"
	"github.com/Autumn-27/norma/transcript"
)

// Config describes the LLM backend resolved from the environment.
type Config struct {
	Format  llm.Format
	BaseURL string
	APIKey  string
	Model   string
	// Proxy routes all LLM requests through the given proxy URL (http/https/socks5,
	// optionally with user:pass@ credentials). Empty means direct — it does NOT
	// fall back to the standard *_PROXY environment variables.
	Proxy string
	// RatePerSecond / RatePerMinute cap the shared request rate across ALL agents
	// using the provider (0 = that window unlimited).
	RatePerSecond float64
	RatePerMinute float64
	// ContextWindowK is the model's context window in K tokens (user-configured),
	// used to size compaction thresholds. 0 = default; see CompactionWindow.
	ContextWindowK int
	// ThinkingType independently controls thinking.type:
	// Empty omits the field for compatibility; disabled explicitly turns it off;
	// enabled turns it on. It is independent of ReasoningEffort because some endpoints
	// expose only an effort parameter, so either setting may be used on its own.
	ThinkingType string
	// ReasoningEffort independently controls reasoning intensity:
	// Empty omits it; low/medium/high/xhigh/max select the corresponding effort.
	// OpenAI uses reasoning_effort; Anthropic uses output_config.effort.
	ReasoningEffort string
	// Stream controls profile SSE usage: true (default) streams, false sends
	// stream:false and gets complete JSON through Provider.Complete. Non-streaming
	// avoids broken gateway SSE (empty frames or lost reasoning deltas), at the cost
	// of live progress/token counts. Maps to agentcore.Options.NonStreaming = !Stream.
	Stream bool
	// MaxTokens limits output tokens per reply. Zero omits it and uses server defaults.
	// Unlike ContextWindowK, the local total-capacity/compaction threshold setting,
	// this value is sent with each request through agentcore.Options.MaxTokens.
	MaxTokens int
	// MaxTokensField chooses the request field for MaxTokens in openai format:
	// Empty uses max_tokens; max_completion_tokens selects the newer field.
	// OpenAI reasoning models reject max_tokens with unsupported_parameter, while
	// many compatible gateways require it. Let users choose per endpoint; do not infer.
	MaxTokensField string
	// Nonempty SessionHeaderKey adds a custom HTTP header to each LLM request,
	// carrying the current session ID (conv-<id>, exp<x>-worker-i<intent>, etc.;
	// see WorkerSessionID). Some gateways use it for prompt caching/sticky routing.
	// Empty omits it. transcript.WithSessionID attaches the value to context, and
	// RoundTripper reads it, so a shared provider can send different session values.
	SessionHeaderKey string
	// MaxConcurrent caps in-flight requests to this provider (llmpool.Limiter).
	// 0 = unlimited. The server applies it per profile (or from
	// ARTEX_LLM_MAX_CONCURRENT for env configs) so a busy task cannot occupy
	// more than the endpoint's own concurrency budget.
	MaxConcurrent int
	// Retry holds resolved profile -> global -> built-in retry parameters, resolved
	// by the server. See RetryConfig for layers; zero values use built-in defaults.
	Retry RetryConfig
}

// RetryConfig travels with an LLM profile. Attempts have consistent semantics:
// zero uses defaults, negative disables retries, positive overrides the count.
// An interval of zero retains exponential backoff; positive selects a fixed delay.
type RetryConfig struct {
	// ConnectAttempts/ConnectInterval cover pre-stream connection reset/timeout/429/5xx,
	// mapping to llm.Config.MaxRetries/RetryInterval: default 3, exponential 0.5s to 8s.
	ConnectAttempts int
	ConnectInterval time.Duration
	// EmptyAttempts/EmptyInterval cover completed responses without content blocks
	// in openai format, mapping to EmptyResponseRetries/EmptyResponseInterval.
	// Defaults: two retries using the same exponential ladder.
	EmptyAttempts int
	EmptyInterval time.Duration
	// StreamAttempts/StreamInterval add a same-provider safe-window retry layer above
	// the SDK for broken streams/overload/in-stream 429 only before any output is delivered.
	// server/task_llm.go consumes it; defaults are two retries, exponential 0.5s to 4s.
	StreamAttempts int
	StreamInterval time.Duration
}

// compaction window resolution bounds (in K tokens). Below the floor the
// threshold math (window − summary reserve − buffer) would go non-positive and
// compaction would fire every turn; above the cap it would never fire.
const (
	defaultWindowK = 200  // unset → assume a 200K window (Claude default)
	minWindowK     = 32   // floor so effectiveWindow stays comfortably positive
	maxWindowK     = 1000 // cap at 1M tokens (user request)
)

// CompactionWindow returns the model context window in TOKENS for compaction
// thresholds, resolved from the user-configured size (ContextWindowK). 0/unset →
// a 200K default; otherwise clamped to [32K, 1M] so compaction stays effective.
func (c Config) CompactionWindow() int {
	k := c.ContextWindowK
	if k <= 0 {
		k = defaultWindowK
	}
	if k < minWindowK {
		k = minWindowK
	}
	if k > maxWindowK {
		k = maxWindowK
	}
	return k * 1000
}

// compactionConfig builds the agent-core compaction config for a context window
// in tokens. agentcore.NewSession wires the summarizer (same provider) when this
// is set on Options.Compaction.
func compactionConfig(windowTokens int) *compaction.Config {
	if windowTokens <= 0 {
		windowTokens = defaultWindowK * 1000
	}
	return &compaction.Config{ContextWindow: windowTokens}
}

// FromEnv resolves the LLM provider config:
//
//	ARTEX_LLM_PROVIDER = anthropic|openai|openai-responses (default: inferred from
//	                     keys; with only a base URL set, openai is assumed for
//	                     local OpenAI-compatible runtimes)
//	ARTEX_LLM_MODEL    = model id        (default: per provider)
//	ARTEX_LLM_BASE_URL = endpoint        (optional; custom/local endpoints make the
//	                     API key optional — Ollama, vLLM, LM Studio, llama.cpp
//	                     typically need none)
//	ARTEX_LLM_PROXY    = proxy URL        (optional; http/https/socks5)
//	ARTEX_LLM_MAX_CONCURRENT = max in-flight requests (optional; 0/unset = unlimited;
//	                     queue excess calls — useful when the endpoint serves a
//	                     fixed number of concurrent sessions, e.g. a shared vLLM)
//	ANTHROPIC_API_KEY / OPENAI_API_KEY         = credentials
func FromEnv() (Config, bool) {
	prov := os.Getenv("ARTEX_LLM_PROVIDER")
	anthKey := os.Getenv("ANTHROPIC_API_KEY")
	oaiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := strings.TrimSpace(os.Getenv("ARTEX_LLM_BASE_URL"))

	if prov == "" {
		switch {
		case anthKey != "":
			prov = "anthropic"
		case oaiKey != "":
			prov = "openai"
		case baseURL != "":
			// Custom endpoint without a key: assume a local OpenAI-compatible
			// runtime rather than leaving the engine idle.
			prov = "openai"
		default:
			return Config{}, false
		}
	}

	c := Config{
		BaseURL: baseURL,
		Model:   os.Getenv("ARTEX_LLM_MODEL"),
		Proxy:   strings.TrimSpace(os.Getenv("ARTEX_LLM_PROXY")),
		// Streaming defaults on; ARTEX_LLM_STREAM=false/0/off explicitly disables it.
		Stream: !isFalsy(os.Getenv("ARTEX_LLM_STREAM")),
	}
	// Optional in-flight request cap; invalid values mean unlimited.
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ARTEX_LLM_MAX_CONCURRENT"))); err == nil && n > 0 {
		c.MaxConcurrent = n
	}
	switch prov {
	case "openai":
		c.Format = llm.FormatOpenAI
		c.APIKey = oaiKey
		if c.Model == "" {
			c.Model = "gpt-4o"
		}
	case "openai-responses":
		c.Format = llm.FormatOpenAIResponses
		c.APIKey = oaiKey
		if c.Model == "" {
			c.Model = "gpt-5"
		}
	default:
		c.Format = llm.FormatAnthropic
		c.APIKey = anthKey
		if c.Model == "" {
			c.Model = "claude-opus-4-8"
		}
	}
	// A custom base URL (Ollama, vLLM, LM Studio, llama.cpp, corporate gateways)
	// may run without any API key; cloud providers still need one.
	if c.APIKey == "" && c.BaseURL == "" {
		return Config{}, false
	}
	return c, true
}

// ConfigFrom builds a Config from UI-provided strings (provider defaults to
// anthropic; model defaults per provider). Inputs are trimmed and the base URL
// is normalized to the API base the provider expects (the provider appends the
// endpoint path itself), so a full endpoint URL is tolerated.
func ConfigFrom(provider, model, baseURL, apiKey, proxy string) Config {
	c := Config{
		Model:   strings.TrimSpace(model),
		BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:  strings.TrimSpace(apiKey),
		Proxy:   strings.TrimSpace(proxy),
		Stream:  true, // Streaming defaults on; callers override from the profile.
	}
	switch strings.TrimSpace(provider) {
	case "openai":
		c.Format = llm.FormatOpenAI
		// provider appends "/chat/completions"; tolerate a full endpoint URL.
		c.BaseURL = strings.TrimRight(strings.TrimSuffix(c.BaseURL, "/chat/completions"), "/")
		if c.Model == "" {
			c.Model = "gpt-4o"
		}
	case "openai-responses":
		c.Format = llm.FormatOpenAIResponses
		// provider appends "/responses"; tolerate a full endpoint URL.
		c.BaseURL = strings.TrimRight(strings.TrimSuffix(c.BaseURL, "/responses"), "/")
		if c.Model == "" {
			c.Model = "gpt-5"
		}
	default:
		c.Format = llm.FormatAnthropic
		// provider appends "/v1/messages".
		c.BaseURL = strings.TrimRight(strings.TrimSuffix(c.BaseURL, "/v1/messages"), "/")
		if c.Model == "" {
			c.Model = "claude-opus-4-8"
		}
	}
	return c
}

// isFalsy reports whether an env-var string explicitly requests "off". Empty or
// unrecognized → false (so an unset var keeps the streaming default).
func isFalsy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "false", "off", "no":
		return true
	}
	return false
}

// Provider returns the short provider name ("anthropic"/"openai").
func (c Config) Provider() string {
	switch c.Format {
	case llm.FormatOpenAI:
		return "openai"
	case llm.FormatOpenAIResponses:
		return "openai-responses"
	}
	return "anthropic"
}

// NewProvider builds an llm.Provider from the config. When a rate is set, the
// limiter lives on the single provider instance — so planner + all workers +
// main agent (which share this provider) are bounded by one shared rate limit.
func (c Config) NewProvider() (llm.Provider, error) {
	client, err := quotaAwareHTTPClient(c.Proxy, c.SessionHeaderKey)
	if err != nil {
		return nil, err
	}
	lc := llm.Config{
		Format:     c.Format,
		BaseURL:    c.BaseURL,
		APIKey:     c.APIKey,
		Model:      c.Model,
		HTTPClient: client,
	}
	// Pass thinking.type and effort independently; empty omits each field.
	// Either, both, or neither may be sent.
	lc.ThinkingType = c.ThinkingType
	lc.ReasoningEffort = c.ReasoningEffort
	// Choose the output-limit field name (empty means max_tokens). Its value comes
	// from agentcore.Options.MaxTokens per round; the provider only chooses the key.
	lc.MaxTokensField = c.MaxTokensField
	// Pass SDK retry semantics unchanged: counts 0=default/negative=off; interval 0=exponential/positive=fixed.
	lc.MaxRetries = c.Retry.ConnectAttempts
	lc.RetryInterval = c.Retry.ConnectInterval
	lc.EmptyResponseRetries = c.Retry.EmptyAttempts
	lc.EmptyResponseInterval = c.Retry.EmptyInterval
	if c.RatePerSecond > 0 || c.RatePerMinute > 0 {
		lc.RateLimit = &llm.RateLimit{PerSecond: c.RatePerSecond, PerMinute: c.RatePerMinute}
	}
	provider, err := llm.NewProvider(lc)
	if err != nil {
		return nil, err
	}
	if c.Model == "glm-5.3" {
		return glm53Provider{Provider: provider}, nil
	}
	return provider, nil
}

// IsQuotaExhaustedMessage deliberately recognizes only explicit balance,
// billing, credit, or quota-exhaustion signals. Generic 429/rate-limit text,
// authentication failures, network errors, and server failures are excluded.
var nonFailoverHTTPStatus = regexp.MustCompile(`(?:status(?:\s+code)?|http(?:\s+status)?)\s*[=:]?\s*(?:401|403|5\d\d)\b`)
var transientQuotaLimit = regexp.MustCompile(`(?i)(?:\b(?:rpm|tpm|rpd|qps)\b|quota[_\s-]*metric|rate[_\s-]*limit|too many requests|(?:requests?|tokens?)\s+(?:per|/)\s*(?:second|minute)|(?:per|/)\s*(?:second|minute)\s+(?:requests?|tokens?)|generate[_\s-]*requests[_\s-]*per[_\s-]*(?:minute|second)|tokens?[_\s-]*per[_\s-]*(?:minute|second))`)

func IsQuotaExhaustedMessage(message string) bool {
	message = strings.ToLower(message)
	// Authentication/authorization and provider-side 5xx failures never rotate,
	// even when a gateway happens to echo a quota-looking phrase in the body.
	if nonFailoverHTTPStatus.MatchString(message) {
		return false
	}
	// Provider APIs frequently describe an ordinary rate limit as "quota
	// exceeded", especially Google-style responses containing a quota metric.
	// These limits recover with time and must stay on the current provider.
	if transientQuotaLimit.MatchString(message) {
		return false
	}
	markers := []string{
		"insufficient_quota", "quota_exceeded", "quota exceeded", "quota exhausted",
		"exceeded your current quota", "billing_hard_limit_reached",
		"billing hard limit", "billing_not_active", "credit balance", "insufficient credit",
		"insufficient balance", "balance is too low", "payment required", "status 402",
		"余额不足", "额度不足", "额度已用尽", "欠费",
	}
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}
	// gRPC RESOURCE_EXHAUSTED is overloaded for both account quota and ordinary
	// request-rate limiting. Preserve it as an explicit exhaustion signal only
	// when the same error does not identify a transient rate limit.
	return strings.Contains(message, "resource_exhausted") &&
		!strings.Contains(message, "rate limit") &&
		!strings.Contains(message, "too many requests")
}

// quotaAwareTransport preserves Norma's normal retry behavior except for a 429
// whose body explicitly says the account quota/balance is exhausted. Norma's
// retry loop treats every 429 as transient; normalizing only that response to
// 402 lets a task router fail over immediately while retaining the original
// response body for provider-specific classification and audit logs.
type quotaAwareTransport struct {
	base http.RoundTripper
	// sessionHeaderKey, when non-empty, is the HTTP header name each request
	// carries; its value is the session id read from the request context. Empty
	// disables it. See Config.SessionHeaderKey.
	sessionHeaderKey string
}

func (t quotaAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Custom session-id header: name is user-configured, value is THIS run's
	// session id (norma stashes it on the context via transcript.WithSessionID).
	// Stable across a session's turns and distinct across sessions — exactly what
	// a session-keyed prompt cache wants. Skipped when no session id is present.
	if t.sessionHeaderKey != "" {
		if sid := transcript.SessionIDFrom(req.Context()); sid != "" {
			req.Header.Set(t.sessionHeaderKey, sid)
		}
	}
	// When LLM recording is on, the Recorder puts a Capture on the context so the
	// raw wire bodies can be persisted. This is the only layer that still sees
	// them: norma builds the request body internally and decodes the SSE response
	// before either reaches the recorder.
	capt := llmrec.CaptureFrom(req.Context())
	capt.SetRequest(requestBodySnapshot(req))

	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	// Tee rather than read: a 200 is an SSE stream that must keep streaming. The
	// 429 branch below reads through this wrapper, so its body lands in the
	// capture before being replaced.
	resp.Body = capt.TeeResponse(resp.StatusCode, resp.Body)

	if resp.StatusCode != http.StatusTooManyRequests {
		return resp, nil
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	if readErr != nil {
		return resp, nil
	}
	if IsQuotaExhaustedMessage(string(body)) {
		resp.StatusCode = http.StatusPaymentRequired
		resp.Status = "402 Payment Required"
	}
	return resp, nil
}

// requestBodySnapshot copies an outgoing request body without consuming it.
// norma builds every model request from a *bytes.Reader, so net/http populates
// GetBody and the copy has no effect on what gets sent.
func requestBodySnapshot(req *http.Request) string {
	if req.GetBody == nil {
		return ""
	}
	rc, err := req.GetBody()
	if err != nil {
		return ""
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		return ""
	}
	return string(b)
}

func quotaAwareHTTPClient(proxy, sessionHeaderKey string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	proxy = strings.TrimSpace(proxy)
	if proxy == "" {
		transport.Proxy = nil // Empty means direct; do not fall back to HTTP_PROXY/HTTPS_PROXY environment variables.
	} else {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, locale.Errorf("llm: invalid proxy %q: %w", proxy, err)
		}
		switch proxyURL.Scheme {
		case "http", "https", "socks5":
		case "":
			return nil, locale.Errorf("llm: proxy %q missing scheme (use http://, https:// or socks5://)", proxy)
		default:
			return nil, locale.Errorf("llm: unsupported proxy scheme %q (use http, https or socks5)", proxyURL.Scheme)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{Transport: quotaAwareTransport{base: transport, sessionHeaderKey: strings.TrimSpace(sessionHeaderKey)}}, nil
}

// logTestConnection prints the raw HTTP status code(s) and response body of a
// connection test to the server log, leaving a diagnosable trail when users click Test:
// exactly what the gateway returned — 401 bodies, quota text, empty frames — not
// just the collapsed ok/err the UI shows. Bodies are clipped to keep a chatty
// SSE stream from flooding the log.
func logTestConnection(c Config, capt *llmrec.Capture) {
	attempts := capt.Attempts()
	if len(attempts) == 0 {
		log.Printf(locale.Text(locale.ServerDefault(), "[llm-test] %s / %s @ %s — no HTTP request sent (configuration or connection failed)"),
			c.Provider(), c.Model, c.BaseURL)
		return
	}
	for i, a := range attempts {
		log.Printf(locale.Text(locale.ServerDefault(), "[llm-test] %s / %s @ %s — attempt %d/%d HTTP %d\nResponse body: %s"),
			c.Provider(), c.Model, c.BaseURL, i+1, len(attempts), a.Status, clipBody(a.Body))
	}
}

// clipBody trims a wire body for logging. 4K is plenty to show an error JSON or
// the head of an SSE stream while bounding a runaway response.
func clipBody(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return locale.Text(locale.ServerDefault(), "(empty)")
	}
	const max = 4096
	if len(s) > max {
		return s[:max] + fmt.Sprintf(locale.Text(locale.ServerDefault(), "…(truncated, %d bytes total)"), len(s))
	}
	return s
}

// TestConnection makes a minimal real completion to verify the provider/model/
// endpoint/key actually work. Returns the round-trip latency and the model's
// reply text.
func TestConnection(ctx context.Context, c Config) (time.Duration, string, error) {
	prov, err := c.NewProvider()
	if err != nil {
		return 0, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Capture raw wire status/body for connection diagnostics; decoding into norma
	// StreamEvents otherwise loses those details. quotaAwareTransport finds this
	// Capture in context and records every HTTP attempt's status and response body.
	ctx, capt := llmrec.NewCapture(ctx)
	defer logTestConnection(c, capt)
	// Connection tests bypass the agentcore session loop, which normally attaches
	// session IDs. Endpoints configured with SessionHeaderKey may require one,
	// such as opencode zen's x-opencode-session header (400 MissingSessionID if absent).
	// Attach a one-time random ID so tests use the same header path as real chat.
	// Endpoints without SessionHeaderKey ignore it, with no side effects.
	ctx = transcript.WithSessionID(ctx, "conntest-"+transcript.NewSessionID())
	start := time.Now()
	// Give MaxTokens enough room: reasoning models can spend thousands of tokens
	// before answering even a simple ping. A tiny budget stops during reasoning,
	// yielding finish=length and confusing interruption/resume indications despite
	// transport success. A sufficient budget lets the model emit OK and stop cleanly.
	// Keep EscalateMaxTokens false to avoid wasteful retry/resume loops after truncation.
	reply, err := agentcore.Run(ctx, agentcore.Options{
		Provider:       prov,
		SystemPrompt:   []string{locale.Text(locale.FromContext(ctx), "This is a connection test. Output exactly the two characters OK, with no reasoning, explanation, or other content.")},
		PermissionMode: acperm.ModeBypass,
		MaxTurns:       1,
		MaxTokens:      8192,
		NonStreaming:   !c.Stream, // Test using this profile's actual streaming/non-streaming mode.
	}, "ping")
	lat := time.Since(start)
	if err != nil {
		return lat, "", err
	}
	// err==nil alone is insufficient: a successful request may still return no text
	// due to reasoning exhaustion, safety filtering, or a compatibility layer dropping
	// content. Treat missing visible text as failure, matching unusable chat behavior.
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return lat, "", locale.Errorf("Model returned no content (request succeeded, but no text was returned)")
	}
	return lat, reply, nil
}
