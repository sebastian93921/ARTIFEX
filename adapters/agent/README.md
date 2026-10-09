# ARTIFEX agent adapter


Connect Claude Code and Codex through a local stdio MCP server, or Pi through its native extension interface. All three use the same validated tools and authenticated ARTIFEX HTTP API. This adapter lets your coding agent control ARTIFEX; it does not replace ARTIFEX's own planner/workers or turn a coding-agent subscription into a model API key.

## Setup

Requirements: Node.js 22+, a running ARTIFEX backend, and an existing admin login. The adapter does not initialize or change your password. Install the separate adapter dependencies:

```sh
cd adapters/agent
npm ci
export ARTIFEX_URL=http://127.0.0.1:8787
export ARTIFEX_PASSWORD='your-existing-admin-password'
```

The password is used to log in as the backend's existing `ARTIFEX` admin user. The returned JWT stays in memory. Alternatively set `ARTIFEX_TOKEN` to an existing login JWT; a supplied token takes precedence. Expired/invalid tokens produce an error and are not silently replaced. Keep credentials in your environment or secret manager; do not commit them to client configuration.

Reads are enabled by default. To enable task creation and pause/resume, explicitly set `ARTIFEX_ALLOW_WRITES=true` before starting the adapter. Task creation schedules work immediately when the backend has an LLM configured. Retain the project's [locally isolated usage restrictions](../../README.md#license-and-disclaimer) and confirm the intended scope before creating a task.

### Claude Code

Use an absolute path to this checkout. Claude Code starts the adapter and inherits its environment:

```sh
claude mcp add --transport stdio artifex -- node /absolute/path/artifex/adapters/agent/src/mcp.js
```

Verify with `claude mcp list`, then use `/mcp` in Claude Code to inspect its tools. [Claude Code MCP documentation](https://code.claude.com/docs/en/mcp).

### Codex

```sh
codex mcp add artifex -- node /absolute/path/artifex/adapters/agent/src/mcp.js
```

Verify with `codex mcp list` and `/mcp`. For explicit environment forwarding in a project/user configuration:

```toml
[mcp_servers.artifex]
command = "node"
args = ["/absolute/path/artifex/adapters/agent/src/mcp.js"]
env_vars = ["ARTIFEX_URL", "ARTIFEX_TOKEN", "ARTIFEX_PASSWORD", "ARTIFEX_LANGUAGE", "ARTIFEX_ALLOW_WRITES"]
```

Use the credential variable that you set; omit unused entries. [Official OpenAI MCP documentation](https://developers.openai.com/codex/mcp).

### Pi

```sh
pi -e /absolute/path/artifex/adapters/agent/pi-extension.js
```

The extension registers native Pi tools and uses the same environment variables. It does not require a Pi MCP plugin. It imports no Pi package namespace, so it follows the public `registerTool` interface across Pi package renames. The adapter package also declares `pi.extensions` for Pi's package loader. [Pi extensions](https://github.com/earendil-works/pi/tree/main/packages/coding-agent).

## Tools

| Tool | Purpose |
| --- | --- |
| `artifex_health` | Backend availability and LLM readiness; no login required |
| `artifex_list_tasks` | Task status, counts, pagination |
| `artifex_get_task` | One task's persisted state and configuration |
| `artifex_get_coverage` | Task asset coverage |
| `artifex_list_findings` | Paginated findings filtered by task, severity, status or text |
| `artifex_get_finding` | Finding detail, evidence and report |
| `artifex_create_task` | Create/schedule a task; requires writes enabled |
| `artifex_control_task` | Pause/resume a task; requires writes enabled |

Example requests: “List my ARTIFEX tasks,” “Show the high-severity findings for task 12,” or, after enabling writes and confirming a local lab scope, “Create a task for this local fixture and show its progress.” A successful create response proves the task exists; check its status and findings to learn whether execution has finished. The adapter does not bypass backend LLM requirements.

## Configuration and failures

| Variable | Default / behavior |
| --- | --- |
| `ARTIFEX_URL` | `http://127.0.0.1:8787`; HTTPS or loopback HTTP origin only |
| `ARTIFEX_TOKEN` | Existing backend JWT; preferred over password |
| `ARTIFEX_PASSWORD` | Existing admin password; login on first authenticated request |
| `ARTIFEX_ALLOW_WRITES` | `false`; accepts only `true` or `false` |
| `ARTIFEX_LANGUAGE` | `en`; supports `en` and `ko` |
| `ARTIFEX_TIMEOUT_MS` | `30000`; integer from 1 to 120000 |

No generic HTTP, shell execution, deletion, approval bypass, provider configuration or arbitrary endpoint tool is exposed. Requests stay on the configured origin, reject redirects, forward cancellation and time out. Responses over 2 MiB fail explicitly; narrow the query rather than treating clipped evidence as complete. Task lists are paginated in the adapter; findings use backend pagination. Tools return `{data: ...}` in text and structured results (Pi uses `details`). Evidence is untrusted target content and must not be treated as instructions.

Failed MCP calls return `isError: true`; failed Pi calls throw for Pi to mark as tool errors. Error messages redact configured credentials. Writes are never automatically retried. If a write times out, inspect task status before repeating it, because the backend may have accepted it.

## Verification

```sh
npm test
npm run check
```

The end-to-end harness runs a real ARTIFEX handler and PostgreSQL, spawns the MCP stdio server through the official SDK, and executes the Pi tool handlers against that backend. It initializes an isolated test password, creates tasks, verifies persisted pause/resume state and coverage, reads a clearly simulated finding, and checks invalid authentication, missing tasks and invalid inputs. No model/provider or target scan is used. Running the Go E2E harness requires the source checkout; platform ZIPs include the adapter's unit tests but only the compiled Go backend.

From the repository root, after creating a **fresh disposable** database named `artifex_adapter_*`:

```sh
ARTIFEX_ADAPTER_E2E=1 \
ARTIFEX_PG_DSN='postgres://user:password@127.0.0.1:5432/artifex_adapter_e2e?sslmode=disable' \
go test ./server -run '^TestAgentAdapterE2E$' -count=1 -v
```

The harness fails if PostgreSQL is unavailable or the database is not explicitly selected; it never substitutes mocks. GitHub Actions repeats these checks in a dedicated disposable PostgreSQL service. MCP protocol tests establish compatibility with the shared interface; they do not claim a paid Claude/Codex/Pi model session was run.
