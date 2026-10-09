package locale

// Add only missing shared messages; domain-specific catalogs remain authoritative.
func init() {
	if !Has("action must be pause|resume") {
		Register("action must be pause|resume", "action must be pause|resume")
	}
	if !Has("action must be pause|resume|cancel") {
		Register("action must be pause|resume|cancel", "action must be pause|resume|cancel")
	}
	if !Has("agent not found") {
		Register("agent not found", "agent not found")
	}
	if !Has("api_key required") {
		Register("api_key required", "api_key required")
	}
	if !Has("asset is not associated with this task") {
		Register("asset is not associated with this task", "asset is not associated with this task")
	}
	if !Has("bad asset id") {
		Register("bad asset id", "bad asset id")
	}
	if !Has("bad constraint id") {
		Register("bad constraint id", "bad constraint id")
	}
	if !Has("bad conversation id") {
		Register("bad conversation id", "bad conversation id")
	}
	if !Has("bad finding id") {
		Register("bad finding id", "bad finding id")
	}
	if !Has("bad goal id") {
		Register("bad goal id", "bad goal id")
	}
	if !Has("bad id") {
		Register("bad id", "bad id")
	}
	if !Has("bad intent id") {
		Register("bad intent id", "bad intent id")
	}
	if !Has("bad json") {
		Register("bad json", "bad json")
	}
	if !Has("bad seq") {
		Register("bad seq", "bad seq")
	}
	if !Has("bad session") {
		Register("bad session", "bad session")
	}
	if !Has("bad task category id") {
		Register("bad task category id", "bad task category id")
	}
	if !Has("bad task template id") {
		Register("bad task template id", "bad task template id")
	}
	if !Has("bad trigger id") {
		Register("bad trigger id", "bad trigger id")
	}
	if !Has("category_id is required") {
		Register("category_id is required", "category_id is required")
	}
	if !Has("context task not found") {
		Register("context task not found", "context task not found")
	}
	if !Has("conversation not found") {
		Register("conversation not found", "conversation not found")
	}
	if !Has("description is required") {
		Register("description is required", "description is required")
	}
	if !Has("description must be at most %d characters") {
		Register("description must be at most %d characters", "description must be at most %d characters")
	}
	if !Has("file not found") {
		Register("file not found", "file not found")
	}
	if !Has("finding not available in task context") {
		Register("finding not available in task context", "finding not available in task context")
	}
	if !Has("finding not found") {
		Register("finding not found", "finding not found")
	}
	if !Has("finding origin task is no longer available") {
		Register("finding origin task is no longer available", "finding origin task is no longer available")
	}
	if !Has("finding origin task or node is no longer available") {
		Register("finding origin task or node is no longer available", "finding origin task or node is no longer available")
	}
	if !Has("invalid JSON") {
		Register("invalid JSON", "invalid JSON")
	}
	if !Has("invalid binding id") {
		Register("invalid binding id", "invalid binding id")
	}
	if !Has("invalid body") {
		Register("invalid body", "invalid body")
	}
	if !Has("invalid finding id") {
		Register("invalid finding id", "invalid finding id")
	}
	if !Has("invalid scope id") {
		Register("invalid scope id", "invalid scope id")
	}
	if !Has("invalid skill name") {
		Register("invalid skill name", "invalid skill name")
	}
	if !Has("missing hash") {
		Register("missing hash", "missing hash")
	}
	if !Has("missing host") {
		Register("missing host", "missing host")
	}
	if !Has("missing hosts") {
		Register("missing hosts", "missing hosts")
	}
	if !Has("missing id") {
		Register("missing id", "missing id")
	}
	if !Has("missing task") {
		Register("missing task", "missing task")
	}
	if !Has("no findings selected") {
		Register("no findings selected", "no findings selected")
	}
	if !Has("no task") {
		Register("no task", "no task")
	}
	if !Has("node is not an intent") {
		Register("node is not an intent", "node is not an intent")
	}
	if !Has("nothing to update: provide status/severity/name/vulnclass") {
		Register("nothing to update: provide status/severity/name/vulnclass", "nothing to update: provide status/severity/name/vulnclass")
	}
	if !Has("saved provider is unavailable") {
		Register("saved provider is unavailable", "saved provider is unavailable")
	}
	if !Has("scope row not found") {
		Register("scope row not found", "scope row not found")
	}
	if !Has("session not found") {
		Register("session not found", "session not found")
	}
	if !Has("side question not found") {
		Register("side question not found", "side question not found")
	}
	if !Has("skill already exists") {
		Register("skill already exists", "skill already exists")
	}
	if !Has("skill name must be 1-64 lowercase alphanumeric/hyphen characters, not starting/ending/doubling hyphens") {
		Register("skill name must be 1-64 lowercase alphanumeric/hyphen characters, not starting/ending/doubling hyphens", "skill name must be 1-64 lowercase alphanumeric/hyphen characters, not starting/ending/doubling hyphens")
	}
	if !Has("skill not found") {
		Register("skill not found", "skill not found")
	}
	if !Has("streaming unavailable") {
		Register("streaming unavailable", "streaming unavailable")
	}
	if !Has("streaming unsupported") {
		Register("streaming unsupported", "streaming unsupported")
	}
	if !Has("task is being deleted") {
		Register("task is being deleted", "task is being deleted")
	}
	if !Has("traffic disabled") {
		Register("traffic disabled", "traffic disabled")
	}
	if !Has("bad json: ") {
		Register("bad json: ", "bad json: ")
	}
	if !Has("provider init failed: ") {
		Register("provider init failed: ", "provider init failed: ")
	}
	if !Has("persist provider failed: ") {
		Register("persist provider failed: ", "persist provider failed: ")
	}
	if !Has("bad finding id: ") {
		Register("bad finding id: ", "bad finding id: ")
	}
	if !Has("bad scope: ") {
		Register("bad scope: ", "bad scope: ")
	}
	if !Has("bad format: ") {
		Register("bad format: ", "bad format: ")
	}
	if !Has("bad status: ") {
		Register("bad status: ", "bad status: ")
	}
	if !Has("bad severity: ") {
		Register("bad severity: ", "bad severity: ")
	}
	if !Has("invalid JSON: ") {
		Register("invalid JSON: ", "invalid JSON: ")
	}
	if !Has("db: ") {
		Register("db: ", "db: ")
	}
	if !Has("empty body from model") {
		Register("empty body from model", "empty body from model")
	}
	if !Has("invalid evidence hash") {
		Register("invalid evidence hash", "invalid evidence hash")
	}
	if !Has("restore destination already exists: %s") {
		Register("restore destination already exists: %s", "restore destination already exists: %s")
	}
	if !Has("inspect restore destination %s: %w") {
		Register("inspect restore destination %s: %w", "inspect restore destination %s: %w")
	}
	if !Has("restore %s: %w") {
		Register("restore %s: %w", "restore %s: %w")
	}
	if !Has("remove traffic stage: %w") {
		Register("remove traffic stage: %w", "remove traffic stage: %w")
	}
	if !Has("archive and task ids must be positive") {
		Register("archive and task ids must be positive", "archive and task ids must be positive")
	}
	if !Has("unsupported traffic archive version %d") {
		Register("unsupported traffic archive version %d", "unsupported traffic archive version %d")
	}
	if !Has("invalid archived traffic blob %q") {
		Register("invalid archived traffic blob %q", "invalid archived traffic blob %q")
	}
	if !Has("traffic blob checksum mismatch: %s") {
		Register("traffic blob checksum mismatch: %s", "traffic blob checksum mismatch: %s")
	}
	if !Has("llm: invalid proxy %q: %w") {
		Register("llm: invalid proxy %q: %w", "llm: invalid proxy %q: %w")
	}
	if !Has("llm: proxy %q missing scheme (use http://, https:// or socks5://)") {
		Register("llm: proxy %q missing scheme (use http://, https:// or socks5://)", "llm: proxy %q missing scheme (use http://, https:// or socks5://)")
	}
	if !Has("llm: unsupported proxy scheme %q (use http, https or socks5)") {
		Register("llm: unsupported proxy scheme %q (use http, https or socks5)", "llm: unsupported proxy scheme %q (use http, https or socks5)")
	}
	if !Has("invalid HTTP header name %q") {
		Register("invalid HTTP header name %q", "invalid HTTP header name %q")
	}
	if !Has("mcp sse http %d: %s") {
		Register("mcp sse http %d: %s", "mcp sse http %d: %s")
	}
	if !Has("mcp sse endpoint: %w") {
		Register("mcp sse endpoint: %w", "mcp sse endpoint: %w")
	}
	if !Has("mcp sse endpoint URL: %w") {
		Register("mcp sse endpoint URL: %w", "mcp sse endpoint URL: %w")
	}
	if !Has("bad MCP response: %w") {
		Register("bad MCP response: %w", "bad MCP response: %w")
	}
	if !Has("mcp tool %q error: %s") {
		Register("mcp tool %q error: %s", "mcp tool %q error: %s")
	}
	if !Has("mcp: empty response for %s") {
		Register("mcp: empty response for %s", "mcp: empty response for %s")
	}
	if !Has("mcp http %d: %s") {
		Register("mcp http %d: %s", "mcp http %d: %s")
	}
	if !Has("mcp: decode json response: %w") {
		Register("mcp: decode json response: %w", "mcp: decode json response: %w")
	}
	if !Has("mcp sse stream: %w") {
		Register("mcp sse stream: %w", "mcp sse stream: %w")
	}
	if !Has("mcp: no matching response in event stream") {
		Register("mcp: no matching response in event stream", "mcp: no matching response in event stream")
	}
}
