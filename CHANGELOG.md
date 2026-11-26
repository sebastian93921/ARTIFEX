# Upstream ARTEX changelog

This is the translated upstream ARTEX history from baseline `160fe13`, preserving its original dates, contributors and technical history. These entries are not ARTEX releases. ARTEX's 2026-10-02 modifications are recorded separately in [provenance](docs/PROVENANCE.md). Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## Latest changes

### Task concurrency and LLM pool
#### Added
- Added `max_active_tasks` (System settings): engine-level task admission under a configurable cap. Paused tasks no longer consume a slot and newly created tasks are admitted correctly. This replaces the `task_concurrency_enabled` / `task_concurrency_limit` pair with one knob.
- Added per-profile LLM request-concurrency caps and slot leases that reserve a profile slot for a whole intent run, so concurrent intents cannot oversubscribe a profile.
- Added a per-profile `max_timeout_seconds` setting; compaction slot-wait windows scale with the profile budget.

### Task integration and API keys
#### Added
- Added scoped task API keys with Settings key management: per-key labels, optional expiry, tasks:read and tasks:write scopes, one-time secret display, and revocation without stopping running tasks.
- Added idempotent task integration APIs: request-ID replay protection, canonical-host admission that deduplicates concurrent submissions of the same target, and tombstones retained after task deletion.

### Task retention and housekeeping
#### Added
- Added retention settings (Tasks → Retention settings): archive completed tasks after a configurable age (default 7 days) and permanently delete archives after a separate residence period (default 30 days), with a daily scheduler disabled by default until enabled by an administrator.
- Archive/export and restore are path-confined with pinned `os.Root`, preserving evidence, anti-replay tombstones and dependent-task barriers. Deletion is fail-closed on archive or blob-cleanup errors; running, queued, paused and unresolved tasks are excluded.
- The report endpoint now hydrates task metrics so list summaries and the report dialog agree on finding counts.

### Scan resume
#### Added
- Added resume for terminal timeout/failed tasks (`POST /api/tasks/{id}/resume` and a row action on the tasks page). Unfinished running intents return to open, completed facts/findings/evidence are preserved, and a fresh timeout budget is stamped for both timeout and failed runs. Cancelled, archived and non-terminal tasks are rejected; repeated resumes conflict cleanly.

### Skills
#### Added
- Added the ct-subs skill: certificate-transparency subdomain enumeration using certspotter with crt.sh fallback and backoff.

### Security and compatibility
#### Changed
- Disabled MPTCP by default via `godebug multipathtcp=0` in go.mod (CVE-2026-98432 mitigation; the actual fix is the host kernel patch).

#### Fixed
- Registered previously missing archive/deletion locale messages so the catalog-coverage test passes.

#### Tests
- Made resume tests hermetic across environments: resumed workers are drained before TempDir cleanup so CI (which has an LLM profile configured) no longer races transcript writes against directory removal.

## [Unreleased]

<<<<<<< HEAD
### Upstream ports

#### Fixed

- Ported three upstream changes from the final ARTEX tree (v0.3.15, via
  kutukam/autonomous-pentest-agent): the auth init/status fail-open fix
  (a DB read error no longer lets an unauthenticated request reach
  /setup and overwrite the admin password; password length is now also
  validated server-side), the SSE same-origin default for
  reverse-proxy deployments, and per-call token metering for the
  intercept judge channel (worker=judge) with a usage card on the
  intercept settings page. Ported UI strings are English per this
  tree's English-only interface.

## [Unreleased]

### Branding

#### Changed

- Rebranded the fork from ARTEX to ARTIFEX: Go module `github.com/sebastian93921/artifex`, entry
  point `./cmd/artifex`, `ARTIFEX_*` env/config keys, `artifex_locale` cookie, `artifex` database
  defaults, `ARTIFEX` login username default, executable/archive names and Docker Compose names.
  Selfupdate now resolves releases from `sebastian93921/ARTIFEX`. Upstream attribution to
  [Autumn-27/ARTEX](https://github.com/Autumn-27/ARTEX) at commit `160fe13` is retained in
  [docs/PROVENANCE.md](docs/PROVENANCE.md). Existing databases keep their configured users; the
  `artex` DB name/role must be renamed when upgrading a deployed instance.
- Removed the internal integration client's name from the public tree: renamed
  `docs/task-integration.md` (was the client-specific contract doc) and its
  cross-repo transport test/env var, and neutralised the retention confirmation
  strings in English and Chinese. The HTTP contract and behaviour are unchanged.
=======
### Added

- **Wildcard domains in asset scope**: a scope line such as `*.example.com`, the way bug bounty programs write a domain and its subdomains, is accepted in company and task asset scope and stored as the `example.com` domain rule, which already covers its subdomains. A URL form (`https://*.example.com/`) works too. The wildcard is accepted only on a registrable domain: `*.api.example.com`, `*.co.uk` and `*.<IP>` are still rejected, because no rule covers only a subdomain's subdomains (#519).
- **Stream viewer direction filter** (follow-up to #380): the Traffic detail's Stream tab can show all messages, client-to-server messages or server-to-client messages. Pagination and refresh keep the selected direction, changing the filter resets the list, and opening another exchange resets the direction to all. Recording-limit warnings remain visible but are excluded from the displayed message counts.
- **Security response headers on every response**: `X-Content-Type-Options: nosniff`, a same-origin referrer policy, `X-Frame-Options: SAMEORIGIN` and a `frame-ancestors 'self'` CSP are now set by one wrapper around the whole handler tree, so the JSON API carries them too; previously only the console route did. Console documents additionally get `Permissions-Policy` and COOP/CORP. `Strict-Transport-Security` is sent only when the request is known to be HTTPS: directly over TLS, or via `X-Forwarded-Proto: https` from a peer in `ARTEX_TRUSTED_PROXIES`, so a direct client cannot claim HTTPS on its own. The README reverse-proxy recipe now starts with an 80 to 443 redirect.
- **Global company switcher**: the sidebar header carries a company picker (search, an explicit "all companies", the asset count). It persists per browser and narrows these views on the server: the findings list, grouped view, asset tree and stat cards (`GET /api/exploration/findings`, `/groups`, `/asset-tree`, `/stats` take `company`), the dashboard findings panels (same list endpoint), the task list (`GET /api/tasks?company=`), the traffic list (`GET /api/traffic?company=`, the hosts the company owns) and the asset tabs (`GET /api/assets?company_id=`, plain and DSL path; the company clause is ANDed outside the compiled DSL). A finding belongs to a company through its live assets. A finding with no live asset belongs to a company only when its task is scoped to that company (`task_scope` kind `company`); otherwise it shows under "all companies" only. An unknown or non-numeric company id returns nothing (#425).
- **Go vulnerability scanning in CI**: every push and pull request runs the official `govulncheck` action against packages reachable from `./...`, using the Go version declared by `go.mod`. Run the same check locally with `./scripts/verify.sh --security`.

### Fixed

- **Asset insert applies the JSON body cap**: `POST /api/assets` decodes through the shared JSON limit, including a `multipart/` request that the upload middleware leaves uncapped. A body over the cap is rejected before the database is touched.
- **Missing Docker buildx is reported before the build**: without the buildx plugin, Compose only warns and falls back to the legacy builder, which fails on the Dockerfile's first `FROM --platform=$BUILDPLATFORM` with `failed to parse platform`. `install.sh` (all-Docker) and `update.sh` (Docker update) now check `docker buildx version` first and stop with an install hint, and the automatic Docker install on apt-based distros not covered by get.docker.com installs the buildx package as well. The README Docker section names buildx as a requirement.
- **A goal URL followed by punctuation is still seeded**: a goal such as `Assess http://juiceshop:3000.` no longer logs `could not parse a target host/IP` and skips the site. Trailing punctuation, including full-width `。` and a closing bracket that has no matching opener, is trimmed from the URL before it is parsed, so `http://[::1]` is left alone. The scheme the goal names is kept and a final period does not end up in the host.
- **Task timeout triggers fire for every task**: `on_task_timeout` used the task id as a watermark, but tasks time out in deadline order. Once a newer task timed out first, an older task that timed out later was never fired for. Fired timeouts are now kept in a persisted set, like met goals; on first start the set is seeded with the tasks already timed out, so nothing replays.
- **A named pipe in the task workspace no longer hangs the archive worker**: packing opened every non-directory entry, and opening a FIFO blocks until a writer appears, so the single archive worker stopped there and every later archive, restore and delete job waited behind it. A socket failed the archive instead, and a device was packed but refused on restore. The packer now skips every entry that is neither a directory nor a regular file, as it already did for symlinks.
- **Task form does not keep test-account secrets on screen**: after a task is created the credential rows are cleared, and the password field is masked.
- **LLM connection-test logs stay one line**: the response body written by a connection test has CR and LF replaced with spaces before it is logged, so the body cannot start a second log record.
- **Benchmark judge validation**: grading and the judge's falsification battery require one verdict per ground-truth ID, actual boolean decisions and gates, and matches to the supplied scanner findings. Invalid grading responses use the existing three-attempt budget and abort without writing scores, rather than counting duplicate IDs or truthy strings such as `"false"`.
- **Live log subscribers can no longer crash the process**: a client disconnect closes its log channel. The sink now delivers a line while it still holds the subscriber lock, so that close cannot land in the middle of the send.
- **Notification delivery ignores HTTP_PROXY**: the dial guard checks the address it connects to. An environment proxy made that address the proxy, so a link-local target could still be reached. Notification HTTP now connects directly.
- **Captured traffic blobs are owner-only**: HTTP and raw-stream bodies spilled out of the database are written `0600`, and their bucket directory is `0700`. An existing blob is tightened when it is seen again.
- **Asset URLs drop userinfo when parsing fails**: a URL `url.Parse` rejects is no longer stored unchanged. The `scheme://userinfo@` prefix is removed first, so a password in that prefix does not land in the asset row.
- **Hook `==` keeps the fraction of a float**: comparing a float with an integer truncated the float first, so `1.5 == 1` and `-0.5 == 0` were true. A number decoded by `json.Decode` is a float, so a hook testing `m["amount"] == 0` matched `{"amount":0.5}`. A float on either side is now compared as a float.
- **Hook `range` counts against the memory budget**: ranging over a body, slice, string, or map charges the iteration before the loop. A body larger than the hook budget now stops with the memory limit instead of allocating one element at a time with no check.
- **Chat upload name search stops**: choosing `name`, `name-1`, `name-2`, … now returns an error when the filesystem rejects the path or after 10000 collisions, instead of looping while `Stat` keeps failing.
- **Uploads do not follow a symlink**: chat and workspace uploads `Lstat` the destination and refuse to write when it is already a symlink. A dangling link is no longer treated as a free name.
- **Hardening follow-up for the upload and capture fixes**: existing spill blob directories and files are tightened on startup, hook `range` charges 32 bytes per pair, and uploads open with `O_NOFOLLOW` on Unix. The Windows upload path keeps the `Lstat` check.
- **Docker Compose database passwords with URL-special characters**: `docker-compose.yml` no longer pastes `POSTGRES_PASSWORD` into `ARTEX_PG_DSN`. The server builds the URL from `ARTEX_PG_HOST`, `ARTEX_PG_PORT`, `ARTEX_PG_USER`, `ARTEX_PG_PASSWORD`, `ARTEX_PG_DBNAME` and `ARTEX_PG_SSLMODE`, percent-encoding the user and password, so `/`, `?`, `%`, `@`, `:` and spaces connect. An explicit `ARTEX_PG_DSN` still wins and is not rewritten. `ARTEX_PG_PASSWORD` is removed from the environment with the other startup secrets (#450).

## [0.7.0] - 2026-11-21

Agent-driven traffic capture (SOCKS5, transparent TCP and raw UDP), operator tampering hooks with their admin panel, the agentic injection and proof hardening from the #124 audit, LLM context telemetry and the worker cache-prefix split, capture-proxy rewrite rules, HAR import and export, and a large security batch across the interception gate, sessions, logs, the workspace file API and the model endpoint probe.

### Added

- **Traffic > Hooks admin panel (P4.2 PR2)**: the Traffic page gains an operator-only Hooks tab for the constrained Go tampering hooks: list, create (always created disabled), edit, delete, toggle, version history with rollback, and a dry-run against a captured exchange or a synthetic input. A dry-run shows the mutated request or response, the fields the hook changed, and the per-line trace, with masked fields shown as their keyed HMAC. Binary and compressed bodies are flagged and skipped, bodies over 1 MiB are flagged, and the UI never renders hook or exchange text as HTML (#435).
- **Concurrent multi-LLM worker rotation** (#270): a task can rotate several bound LLM profiles across its workers, with per-lane exhaustion tracked and shown on the task controls.
- **Encrypted S3 archive storage** (#285, #318): an optional S3 backend stores task archives encrypted, connects with the existing archive lifecycle, and lets an interrupted archive job be recovered on the next start.
- **Turkish and Simplified Chinese UI, optional Neon theme** (#346, #413, #401): the web console gains a translation layer with a Turkish and a Simplified Chinese locale, and an opt-in Neon theme preset in Settings.
- **Readiness probe** (#363): `GET /api/ready` checks PostgreSQL and reports readiness separately from the health route.
- **Operator console browser protections** (#349): the embedded console restricts framing to the same origin and keeps referrer information on the same origin.
- **Target noise governor**: one shared egress governor paces ARTEX's own HTTP requests (WebFetch, the http custom tools and the enrich probe) against a noise profile (`off` by default, `quiet`, `balanced`, `aggressive`): a per-host token bucket with jitter, per-host and global in-flight caps, 429/503 backoff, GET deduplication with a short cache, and a configurable target User-Agent (`ARTEX_TARGET_USER_AGENT`). Off is a strict passthrough (#205).
- **Low-and-slow prompt block**: when a noise profile is set, the planner, worker, main agent and chat prompts carry the per-host limits of the profile and the exact rate or delay flags for nmap, nuclei, httpx, katana, ffuf, dalfox and sqlmap; with the profile off the prompts are unchanged (#212).
- **Noise profile at runtime**: System settings has a **Target noise** card. It sets the profile (`off`, `quiet`, `balanced`, `aggressive`) through the `noise_profile` setting, which takes precedence over `ARTEX_NOISE_PROFILE`. The change applies to the next WebFetch, http custom tool and enrich request, and to the next agent prompt, without a restart. The card also shows the per-host counters, which `GET /api/settings/noise` returns (#233).
- **Bash requests paced through the capture proxy**: with traffic capture on, the Bash proxy URL carries a marker userinfo. The recording proxy paces each request of a marked connection by the noise profile's per-host rate and jitter, and counts it, before it goes upstream. This applies inside CONNECT tunnels too. WebFetch, the http custom tools and enrich keep the unmarked URL and are not paced twice. The proxy now drops `Proxy-Authorization` from every request it forwards or records. This is best effort: only clients that honour the proxy are covered, with capture on (#234).
- **Hierarchical budgets**, off by default: a task budget and a per-intent budget take the minimum of their own budget and the parent's remaining budget above the wrap-up reserve, planner calls count inside the task, and reaching the reserve forces a wrap-up (#206).
- **Coverage ledger**: the worker records what it examined, ruled out or left open for each surface and risk class with `record_coverage`; gaps, unexamined classes and completeness appear in the planner context, `GET /api/tasks/{id}/coverage`, the coverage card and the Markdown report (#207).
- **Closed-question judge**: the LLM fallback judge answers eight fixed questions that the code owns and the code takes the decision; every failure now leads to ask instead of allow, and the previous behaviour stays available as `llm_judge_mode = freeform` (#208).
- **argv interception rules**: a new `match_type = argv` matches the parsed prefix of a Bash command exactly, and a pending Bash approval can create such a rule in one click; a code-owned denylist refuses programs where a prefix would allow arbitrary code (#210, #211).
- **Scanner output ingestion**: `ingest_scan_output` parses nmap XML, nuclei JSONL, httpx JSONL and ffuf JSON, and writes assets and facts deterministically, so the model never reads the raw output (#214, #215).
- **bbot ingestion**: a typed parser splits bbot JSONL events by `scope_distance`, only in-scope events go through `insert_assets`, and `ingest_bbot` reads the file line by line with a 64 KiB cap (#199, #200).
- **Tool RAG**, off by default: when the bound tool list of an agent is large, only the subset relevant to the current intent is injected, as a projection that never adds or edits a tool (`ARTEX_TOOL_RAG`) (#204).
- **Fact provenance**: `record_fact` takes a closed `source` enum (default `inference`) and every fact carries the server-set `origin`, both shown in `recent_facts`, `list_facts` and `expand_digest` (#194).
- **Per-message nonce envelope**: agent- and target-derived fields reach the planner and worker user messages inside a code-owned data envelope with a random nonce per message; operator input stays outside it (#195).
- **Intercept risk score and batch approval**: a deterministic risk score (low, medium, high) is shown on each pending approval, and up to 100 pending decisions can be settled in one call, each through the existing single-decision path (#232).
- **`ask_operator` target warning**: a request whose question names a host or URL outside the goal targets is tagged as containing target-derived text (#197).
- **Opt-in update banner**: `ARTEX_UPDATE_CHECK` makes the backend check the latest GitHub release at most once per 24 hours and the UI show a banner; off by default (#202).
- **Release provenance**: release archives carry GitHub build provenance attestations and a CycloneDX SBOM (`gh attestation verify <zip> --repo BaaRai/ARTEX`); release notes are the matching `CHANGELOG.md` section (#219, #201).
- **Opt-in constrained Go tampering hooks — backend, API & trace (P4.2 PR1)**: opt-in behind `ARTEX_TAMPER_HOOKS_ENABLED=1` (off by default; flag off loads zero hooks and keeps the no-hook baseline unchanged). Adds a small, deterministic, zero-dependency Go-subset evaluator (`traffic/hookvm`, parsed via `go/parser` with sorted map iteration, hard 100k step / 25 ms per-hook / 100 ms per-phase / 4 MiB memory bounds, and AST rejection of FS/network/exec/env/time/rand/goroutine/channel/defer/goto/global-var/pointer/switch constructs). Operator-only hooks (never an agent tool) are stored sealed at rest in `traffic_tamper_hooks` and `traffic_tamper_hook_versions` (migration `0011_traffic_tamper_hooks.sql`), managed via `/api/traffic/hooks*` (CRUD, toggle, version history, rollback, dry-run), prefiltered with `traffic/rewrite` matchers, executed after declarative rewrite rules per phase (fail-open only in 0.7; binary/compressed/>1 MiB bodies skipped; tainted sensitive fields stay HMAC-masked), and traced in SQLite `exchange_rewrites` (`source_kind`, `hook_id`, `hook_version`). See `docs/traffic-tamper-hooks.md`.
- **Opt-in SOCKS5 UDP ASSOCIATE & raw UDP recording (P4.1c)**: when traffic capture is enabled, setting `ARTEX_SOCKS5_UDP_ENABLED=1` enables RFC 1928 `UDP ASSOCIATE` (`0x03`, IPv4 first) on the SOCKS5 listener (off by default: with flags off no listener starts; with SOCKS5 TCP on and UDP off, `UDP ASSOCIATE` returns `0x07` and no UDP socket is opened). Enforces per-datagram destination policy before forwarding using the existing target gate (`intercept.Interceptor.ResolveUDP4` / `intercept.PinnedUDP4Resolver`) and egress governor (`egress.Do`), dropping denied datagrams without passthrough; bounds associations (`UDPMaxDatagrams`, `UDPMaxBytes`, `UDPIdleTimeout`) and tears them down with the TCP control connection; drops `FRAG != 0` with no fragmentation reassembly; and records each forwarded datagram individually in SQLite `raw_udp_datagrams` with capped previews and atomic `_blobs` refs without writing HTTP `exchanges` rows or PG migrations. Transparent UDP (`IP_RECVORIGDSTADDR`) is deferred to C2. See `docs/traffic-socks5-udp.md`.
- **Opt-in Linux transparent TCP inbound & raw TCP recording (P4.1b)**: on Linux when traffic capture is enabled, setting `ARTEX_TRANSPARENT_TCP_ENABLED=1` (loopback `127.0.0.1:1081` by default) or `ARTEX_TRANSPARENT_TCP_ADDR=<ipv4:port>` starts an IPv4 transparent TCP listener fed by operator-managed `iptables`/`nftables` `REDIRECT` rules (off by default; ARTEX never touches the host firewall; root/`CAP_NET_ADMIN` is required only for the operator's redirect rules; IPv6 is not covered). Recovers original IPv4 destinations via `SO_ORIGINAL_DST` with self-redirect loop protection, reuses the P4.1a target gate (`intercept.Interceptor.DialSOCKS5`), egress governor (`egress.Do`), half-close relay bounds, and SQLite `raw_tcp_streams` store (atomic `_blobs` spill, no HTTP `exchanges` row, no PostgreSQL migration). See `docs/traffic-transparent.md`.
- **Opt-in SOCKS5 inbound proxy & raw TCP recording (P4.1a)**: when traffic capture is enabled, setting `ARTEX_SOCKS5_ENABLED=1` (loopback `127.0.0.1:1080` by default) or `ARTEX_SOCKS5_ADDR=<addr>` starts an RFC 1928 SOCKS5 `CONNECT` listener alongside the HTTP recording proxy (off by default; HTTP proxy and agent proxy env vars are unchanged; listener does not start if traffic recording is disabled). Accepts loopback no-auth and RFC 1929 username/password without verifying credentials (carrying `artex-pace` in the username; binding to a non-loopback address therefore exposes an unauthenticated proxy, though the target gate and governor still apply), rejects `BIND`/`UDP ASSOCIATE` with `0x07`, enforces the existing target gate (`intercept.Interceptor.DialSOCKS5` / `intercept.PinnedDialer`) and egress governor (`egress.Do`) before dialing, relays half-close-aware TCP with concurrency/byte/idle/handshake bounds, and records one row per connection in SQLite `raw_tcp_streams` with capped directional previews and atomic (temp+rename) `_blobs` refs without writing HTTP `exchanges` rows. See `docs/traffic-socks5.md`.
- **Opt-in worker cache-prefix split (P3.5 Phase C)**: setting `ARTEX_CACHE_PREFIX=1` (off by default) splits the `worker` system prompt at `DynamicBoundary = 1` into a sibling-shareable static prefix (`body + untrustedContentTail + workerTrafficBlock`) and a per-intent dynamic suffix (`workerArtifactSpec(runDir)` through `<available-deferred-tools>`) without reordering sections or altering the visible joined prompt text. Planner, mainagent, chat, and goals are unchanged; custom worker prompts referencing `{{.Now}}` remain volatile in the static segment. See `docs/context-telemetry.md`.
- **Opt-in LLM context size telemetry (P3.5 Phase A)**: setting `ARTEX_CONTEXT_TELEMETRY=1` (off by default) records per-call request section byte sizes (`system_static`/`system_dynamic` split at `DynamicBoundary`, serialized `tools`, `messages` by role, `tool_result` subset, and `last_message_bytes`), tool/message counts, and token/cache usage in the new `llm_context` table (migration `0009_llm_context.sql`), with zero prompt/behavior change and zero content stored, independent of `llm_record`. `GET /api/llm/context/summary` aggregates input percentiles (`p50`/`p90`/`p99`), pro-rata section shares over real `input_tokens`, `cache_read/input` ratios by `(worker, model)`, and top tasks. See `docs/context-telemetry.md`.
- **HAR 1.2 traffic import and export**: `POST /api/traffic/import/har` imports raw HAR 1.2 JSON captures (up to 32 MiB / 2,000 entries / 32 MiB decoded bodies) with deterministic `har-<sha256>` IDs (and `_artexExchangeId` preservation for ARTEX exports so distinct same-second exchanges round-trip without collapsing), base64 decoding, `postData.params` and `cookies` array fallbacks, URL userinfo stripping, blob spilling above 256 KiB, and full-text indexing, without applying capture rewrite rules (`exchange_rewrites` stays empty); per-entry errors are reported alongside `imported`/`skipped`/`failed` counts. `GET /api/traffic/export/har` exports filtered traffic (reusing the `GET /api/traffic` filters) as a compliant HAR 1.2 document (with explicit unknown placeholders for unrecorded timings and header sizes) in stable `(ts, id)` order, encoding UTF-8 bodies as text and binary bodies as base64, capped at 1,000 entries / 20 MiB raw headers+bodies (31 MiB JSON backstop, guaranteed under the 32 MiB import limit) with explicit truncation reporting via `X-ARTEX-HAR-*` headers and `_truncated`/`_truncatedReason`/`_totalMatched` fields. The Traffic page adds **Import HAR** and **Export HAR** controls. See `docs/traffic-har.md`.
- **Capture-proxy rewrite rules, storage and API**: `GET`, `POST`, `PUT` and `DELETE /api/traffic/rewrite-rules` and `POST /api/traffic/rewrite-rules/{id}/toggle` manage rules that match a request or a response (method, host, URL, headers, body, status) and describe a rewrite (method, path and query, headers, body, status). Rules are validated on save, limited to 100 of which 50 enabled, created disabled, and stored in the new table `traffic_rewrite_rules` (migration `0005_traffic_rewrite_rules.sql`) with `matchers` and `actions` encrypted at rest. The value of an action marked `sensitive` is never returned, and an update sent without it keeps the stored one. The server compiles the enabled rules at startup and after every change and hands the snapshot to the proxy. The proxy applies them from the next entry. See `docs/traffic-rewrite-rules.md`.
- **Capture-proxy rewrite rules applied**: the capture proxy applies enabled rules to each HTTP request before it is sent to the target and to each response before it reaches the client. Each action that ran is recorded in the new table `exchange_rewrites` in the same transaction as the exchange, `traffic_get` shows a `REWRITTEN` line, and `traffic_search` never finds a value a rule wrote. SSE status and headers are rewritten at the start of the stream. **WebSocket handshakes are not rewritten**: the capture library runs its hook after the upgrade, so the status and headers cannot be changed, and frames pass unchanged. **Responses of 5 MiB or more** are forwarded unchanged and not recorded in full, because the library does not call the response hook for them. Snapshots of original requests are capped at 1024 entries and 64 MiB. Replays (`POST /api/traffic/replay`) bypass capture rewrite rules by default via an internal `X-ARTEX-Replay-Nonce` header stripped unconditionally by the proxy, and apply the rules when opted in with `apply_rules: true` (the **Apply capture rewrite rules** checkbox in the Replay panel). `GET /api/traffic` exposes the proxy's `rewrite` counters (`large_responses`, `capacity_refusals`, `tombstone_drops`, `conversions`, `sweeps`), flags each exchange row with `rewritten`, and filters with `rewritten=all|rewritten|original` (also accepting `true`/`false`), surfaced with a filter control and row badge on the Traffic page. Traffic archives export version 2 with per-exchange `rewritten` and sorted distinct applied `rewrite_rule_ids` while remaining deterministic and backward-compatible with version 1 archives. The rule-management web UI comes later.
- **Bulk finding status** (#298): select findings in the findings table and apply one disposition to all of them, up to 500 per call, in one transaction; each row still goes through the single-decision notification semantics and the change is mirrored onto its exploration node (#289).
- **Stream message direction filter** (#379): `GET /api/traffic/stream` and the `traffic_messages` agent tool take an optional `direction` (`c2s` or `s2c`) that lists only one side of a WebSocket or SSE conversation. `total` and `offset` apply to the filtered list, each row keeps its `seq` in the whole stream, and the `meta` row (messages dropped past the recorder limits) stays in a filtered page. Any other value is a 400 (API) or a tool error.
- **Traffic replay** (#266): replay a recorded request from the traffic detail with its method, URL, headers and stored body; replay is disabled when only the body preview is loaded.
- **Finding proof strength in reports** (#268): the Markdown report and the export show each finding's proof strength next to its verification status.
- **Opt-in Keenable web search backend** (#295): the search tool gains Keenable alongside Brave and Tavily; the key is stored encrypted and can be cleared independently.
- **Durable activity outbox** (#300): activity rows that cannot be stored are written to the new `activity_outbox` table (migration `0006_activity_outbox.sql`) and replayed once on the next start. The server stops admitting new work before the drain, and a shutdown cancel leaves an in-flight intent claim running so the next start can reset it to open instead of recording it as done or blocked.
- **LLM model search** (#284): the System → LLM model picker can search the model list, and the API-key field opts out of password-manager autofill.
- **Finding report state** (#308): findings carry a `report_state` (`none`, `submitted`, `accepted`, `rejected`, `duplicate`) for what was sent to a bug bounty program, separate from the triage status; the bulk update accepts both fields, and restoring an older archive fills `none` (migration `0007_finding_report_state.sql`).

### Changed

- **`record_fact` is facts-only** (#410, #282): the tool publishes and accepts facts only; asset writes go through `insert_assets`.
- **Web dependency lockfile** (#438): `source-map-js` 1.2.4 and `baseline-browser-mapping` 2.11.39, past their denial-of-service advisories; `npm audit --omit=dev` is clean.
- **Build and supply-chain hardening** (#408, #416, #419, #420): tool and Node digests are pinned instead of fetching their checksums, `npm ci` runs with `--ignore-scripts` in the build, install and update stages, the web build disables Next.js telemetry, and the api-recon skill lockfile points at the public npm registry.
- **Password changes revoke sessions**: a password change stores a monotonic `auth.password_version` in the same transaction as the hash. Tokens carry that version, so a token from an older version is rejected, including when two changes land in the same second. `reset-password.sh` writes the same counter and requires an ARTEX restart, because the process reads the version at startup. A failed write leaves the previous session valid. Log, activity, and side-question streams re-check the token and close after the version moves. If the version cannot be read at startup, sessions are rejected until the next login reads it.
- **Task budgets survive a restart**: planner, worker, judge, and compactor rows in `llm_usage` are counted back into `ARTEX_TASK_BUDGET_*`. A restored task already inside the wrap-up reserve only has that reserve left, so the run wraps up instead of starting another worker. The per-worker estimate is not restored and starts from the defaults (50,000 tokens and 10 turns) until a worker finishes in this process. Chat, goal decomposition, and side questions stay outside the cap.
- **Untrusted-output boundary is now on by default**: Norma frames tool results for the worker, planner, chat and main agent unless `ARTEX_UNTRUSTED_BOUNDARY` is set to `0`, `false`, `off` or `no`. It was opt-in via `ARTEX_UNTRUSTED_BOUNDARY=1` in 0.6.0 (#166); the default flips here (#203).
- **Versioned migrations**: the `db` package applies numbered `db/migrations/NNNN_slug.sql` files once, in order, under the schema advisory lock, and records a SHA-256 checksum in `schema_migrations`; missing, renamed, changed or out-of-order migrations fail closed (#226).
- **Coverage graph performance**: the coverage graph batches its connector lookups (three queries instead of one per root and per company) and adds the partial index `idx_ts_icp`; on a task of about 3,000 assets the p50 dropped from 126 ms to 27 ms and the queries per call from 221 to 3 (#230).
- **Request body bounds** (#305): JSON request bodies decode at most 32 MiB, every non-multipart body is capped, and an oversized body answers 413 instead of a generic bad request. `ReadTimeout` bounds how long headers and the body may take to arrive without cutting a handler that is already running.
- **Chat attachments up to 512 MiB** (#252): the client, server, compose file and reverse-proxy sample agree on a 512 MiB combined multipart limit.
- **MITM passthrough TTL** (#306): a host that cannot be intercepted is tunnelled for one minute, then interception is tried again; an expired mark counts as intercepted.
- **Norma v0.4.11** (#294).
- **CI**: the suite also runs on the latest Go release as an advisory job, the rewrite-rule engine runs under the race detector, and the gzip/compression fixtures no longer depend on the local Go version (#279, #278).
- **Repository hygiene** (#275): `docs/` is no longer ignored, and contributing documents the 7-open-pull-request cap (#307).

### Fixed

- **Compose private model endpoints** (#406): `ARTEX_LLM_PRIVATE_ENDPOINTS` is passed to the artex container, so the private endpoint allow-list works in the Compose deployment.
- **Bash exit 127 in transcripts** (#398): the session transcript explains a 127 exit as a missing command and shows the matching hint.
- **LLM usage status on early stream exit**: when a consumer stops reading a stream before `message_stop`, the recorder now stores the `llm_usage` row as `error` at that point, whether or not the context was cancelled, instead of inferring the status from `ctx.Err()` in a deferred call. `TestSideUsageRecordedOnceOnConsumerCancellation` runs again (follow-up to #9).
- **RSI lesson trust**: the planner overview carries each finding's triage status, and `prove_goal` refuses a `false_positive`, `duplicate`, or `ignored` finding. A lesson with no company is visible only to tasks that also have no company. Retracted evidence stays on the row with `withdrawn_at` set, extraction runs when a disposition changes or a retest completes, and it still withdraws evidence after a task gains a second company. The rendered lesson block is capped in characters, not bytes.
- **Workspace and skill symlink confinement**: workspace file operations, the skill file APIs and the skill loader resolve symlinks in every path component and refuse a target outside their root, so a link planted in the workspace or a skill directory cannot expose another file.
- **Target gate behind proxies**: a target approval is name-only whenever a proxy is in the path, including the recording proxy, the global egress proxy and a custom tool's own proxy, so the gate never claims an IP binding the proxy does not enforce.
- **Remote MCP response bound**: Streamable HTTP JSON-RPC bodies and SSE data payloads are refused above 8 MiB, so a remote MCP server cannot force an unbounded decode.
- **LLM response capture bound**: each recorded provider attempt keeps at most 1 MiB, and a call keeps at most 8 attempts, so a large or retrying stream cannot grow the recorder without bound. The live stream is unchanged.
- **Intercept audit ring**: `preToolUse` records allow only after intercept and the target gate decide, so a blocked call no longer leaves a leftover allow row in `Guard.Audit()`.
- **DSN password redaction**: `config.Redact` now masks libpq `password=` keywords and `?password=` query values as well as URL userinfo, so a ping-failure log cannot print the database password.
- **Asset DSL nesting**: `ParseDSL` refuses queries longer than 4 KiB or nested more than 32 parentheses deep, so a crafted `dsl` cannot grow the goroutine stack without bound.
- **Retry-After overflow**: a 429/503 `Retry-After` of huge seconds no longer wraps `time.Duration` into a tiny delay. Values above `MaxDelay` are not retried.
- **Conversation message pages**: `GET /api/conversations/{id}/messages` clamps `limit` (200 for history, 1000 for `?since=`), so a huge query cannot load the whole transcript in one response.
- **Findings CSV formula cells**: `FindingsCSV` prefixes name, class, summary, task, and other text fields that start with `=`, `+`, `-`, `@`, tab, or CR so Excel/LibreOffice do not evaluate them as formulas when the export is opened.
- **Asset service links**: the Services tab only uses an asset URL as `href` when it is `http:` or `https:`. `javascript:`, `data:`, and other schemes render as text.
- **Chat session directories**: `ChatAgent.Chat` accepts only `[A-Za-z0-9_-]` session ids (at most 64 bytes) before joining them under `<workDir>/sessions`, so `..` and path separators cannot escape that directory.
- **Change-password rate limit**: `POST /api/auth/change-password` uses the same limiter as login, keyed by client IP and by the session token, so a stolen session cannot guess the current password without backoff.
- **Ingested traffic headers**: `RecordExternal` rejects a method that is not an HTTP token and header names or values that contain CR, LF, or NUL, matching Replay, so ingested exchanges cannot inject extra header lines into stored evidence.
- **Browser MCP starts offline** (#329): the seeded `browser` MCP ran `npx @playwright/mcp`, which contacts the npm registry on every start even with the package installed globally, and Playwright MCP defaulted to a Chrome channel the image does not ship. It now runs the image's `playwright-mcp` binary with `--headless --browser chromium`. Existing databases that still have a stock `npx @playwright/mcp` row (including the common `-y` form) or a `playwright-mcp` row with no `--browser` are rewritten once; any other command or package is left alone. The image also installs the Chromium build `@playwright/mcp` and `@playwright/cli` expect, which is not the same build `playwright install` puts in the cache.
- **CREATE DATABASE identifiers**: `ensureDatabase` now builds the statement with `pgx.Identifier`, which quotes the DSN database name and doubles any embedded quote, so the name cannot close the identifier. No database name PostgreSQL accepts is refused, so existing installs with a hyphen, a dot or non-ASCII characters keep starting.
- **ask_operator image_path**: the screenshot path must resolve inside the run directory of the agent that asks (`<data>/tasks/<id>/i<N>` for a worker, `<data>/tasks/<id>/main` for the main agent). A relative path resolves from that directory and an absolute path must be inside it; `..` segments and symlinks that leave it are refused, so the tool cannot copy an arbitrary server image into the operator transcript. The copy is staged in `<data>/tasks/<id>/uploads`.
- **ask_operator image_path, behavior change**: a worker that passed `i<N>/shot.png` relative to the task directory must now pass `shot.png` from its working directory, or an absolute path inside its run directory.
- **insert_assets fail-closed**: when intercept or task allow/block rules cannot be loaded, `insert_assets` refuses the whole call instead of evaluating an empty rule set (which allowed every asset). The tool result does not include the underlying database error.
- **BlobRange length no longer wraps int64**: `offset+length > size` overflowed when length was near MaxInt64, so the cap was skipped and `make([]byte, length)` could exhaust memory. The bound is now `length > size-offset`.
- **PostgreSQL text[] literals escape backslashes**: `marshalStringArray` quoted double quotes but not backslashes, so a value ending in `\"` could close the current array element and inject another. It now doubles backslashes before quotes, matching `marshalJSONBArray`.
- **Browser MCP env null no longer crashes startup**: a stored MCP `env` of JSON `null` unmarshaled into a nil map, and `syncBrowserMCPProxy` then assigned `NODE_EXTRA_CA_CERTS` when traffic capture was on, panicking the process on every start. `decodeStrMap` now always returns a writable map.
- **Provider refusals reported as refusals**: a turn the provider's safety system declined (Anthropic `stop_reason: "refusal"`, OpenAI-compatible `finish_reason: "content_filter"`) has no text and no tool calls, so it ended as `completed` with "the model ended this turn normally", and the idle-turn rerun resent the same conversation up to the empty-response retry count, where it is expected to be refused again. The LLM recorder now hands each call's stop reason to the run (`llmrec.WithStopSink`, `agent.WithStopTracker`). After a refusal the idle-turn rerun is skipped, and the run summary says the provider refused the turn and the intent was not completed.
- **LLM health badge on every task path**: `/system/llm/` read a breaker state that only the failover Pool wrote, so a profile used by an explicit task chain, by a bound agent with failover off, or as the global provider stayed "Healthy" through a real outage. Every task LLM attempt now records its outcome through the same policy the Pool uses, keyed by the profile that served it. The LLM recording page also explains an empty list when the `llm_record` toggle is off instead of looking broken.
- **LLM health persistence** (#304): persisted breaker rows are applied in sequence at startup and written before the database closes, and a slow write no longer holds the registry lock while it runs.
- **Skill visibility survives restarts**: turning a skill off now writes an explicit disabled row instead of deleting it, so the startup seed, which only inserts missing rows, can no longer bring the skill back. Skills that were turned off before this version have no row to keep, so they come back once after the upgrade and must be turned off again.
- **Browser extension traffic capture**: CDP delivers `Network.responseReceived` response headers as an object, not an array of `{name, value}` records, so the extension threw on every response and never queued an exchange. It now accepts both shapes and lower-cases names before the body-capture gate, so captures work again and `content-type` correctly controls body capture.
- **bbot on arm64**: the image now builds on arm64 hosts such as Apple Silicon. bbot's `gowitness` downloaded an x86-64 Chrome and `retirejs` an x86-64 Node, which can't run there, so the build failed. On arm64 the image uses Debian's `chromium` for gowitness and an arm64 Node for retirejs, and the build check now covers `retirejs` too. amd64 builds are unchanged.
- **Auto-scope hygiene**: auto `task_scope` rows count for coverage and untested assets only under a declared root, and direct coverage counts anchored assets through fact anchors only; the scope is described as "coverage scope, not enforced" (#196).
- **Enrich HTTP probe**: it now dials through `intercept.PinnedDialer`, so the target gate applies and a probe without a target scope is refused (#198).
- **Agent hook data races**: the eight package-level hooks are synchronised, so a second wire-up no longer races tasks that already read them (#227).
- **Coverage flag read failure** (#303): asset-coverage tools stay available when the coverage flag cannot be read, and a failed read no longer disables coverage for the task.
- **RSI lesson correctness** (#287, #290): lessons are scoped from `task_scope`, UTF-8 is no longer split mid-character, caution lessons are looked up by cue, lesson evidence and every retest are retracted when a finding's disposition reverses, and extraction is skipped on a task with more than one company.
- **Finding disposition mirror** (#289): finding triage is mirrored onto its exploration node, so the graph and the findings table agree.
- **RSI lessons in the data envelope** (#288): lesson text reaches the planner and worker inside the same nonce-framed envelope as other target-derived content.
- **Intent summaries framed** (#311): the intent summary on the worker-finished and finding lines, and the summaries in `graph_overview`, now reach the planner inside the nonce frame; the structured intent fields stay in the plain JSON, and the operator-typed deletion reason stays outside.
- **LLM pool context window** (#286): the pool and side-question paths use the model's real context window instead of a default when computing compression thresholds.
- **Sensitive rewrite values** (#273 follow-ups): a kept sensitive value is matched on action type, header name and regex, and a host change is refused while a stored sensitive value is kept.
- **Triage bot context** (#274): the PR triage bot receives a directory map instead of a truncated file list.

### Security

- **Secrets at rest**: the credential-bearing columns and settings (LLM API keys and proxy, task credentials, MCP server env, notification channel config, search and proxy settings), the exec spec of custom tools and the evidence snapshot heads are encrypted in PostgreSQL with AES-256-GCM (`enc:v1:`) using `ARTEX_SECRET_KEY`, else `keys/secret.key`; existing plaintext rows are backfilled once at startup, and a missing or wrong key while encrypted data exists refuses to start. This protects database dumps and backups, not a running instance (#213, #216, #217, #218).
- **URL credentials are dropped before storage**: scan and bbot ingestion, the asset store and archive restore strip URL userinfo, so `https://user:pass@host/app` is stored and matched as `https://host/app` (#222, #223, #224).
- **Agentic injection and proof hardening** (#437): the four surfaces that still carried target or agent text outside the per-message data envelope now go through it: the worker intent assets move out of the system prompt into the framed user message, and the planner stagnation warning, agent-written task credentials and planner todos are framed. Scanner output is opened with `os.Root` and every symlink and hard-linked file is refused. `prove_goal` closes a goal with a finding only when a bound `traffic_ref` still exists in the live capture and its host matches the finding's linked asset, and refuses fail-closed when that check is unavailable, so a planted or unbound finding stays recorded but cannot close a goal. An agent scope addition outside the declared scope, or one whose classification read fails, is recorded as `expanded` with its source and reason. A deterministic adversarial bench runs in CI against a hostile local target.
- **Interception, session, log and endpoint hardening** (#436): when the interception rules cannot be read, tool calls are blocked instead of allowed, and a rule whose pattern does not compile blocks the calls it would target. A cookie-authenticated POST, PUT, PATCH or DELETE must carry a same-origin `Origin` or `Sec-Fetch-Site`, otherwise it answers 403; bearer tokens, GET and HEAD are unaffected. The one-time setup token is no longer written to the log the API exposes, and proxy credentials are masked at write time, on stderr and on read-back. The workspace refuses the capture CA, index and blob store for read and write, and refuses a delete that would remove them. `POST /api/llm/models` refuses carrier-grade NAT, benchmark, IETF protocol and reserved ranges for literal hosts, resolved names, redirects and at dial time, judges NAT64 and 6to4 addresses by the IPv4 they embed, and no longer echoes an upstream error body.

### Thanks

Thanks to the contributors whose work is in this release:

- Security and core reliability: @Coff0xc, @Protonmatter, @liuxiaobo0920, @cyberd4ch, @Dev-next-gen, @Michael-JRead, @anqorithm, @renaesop, @tglemser, @adam-33-pixel
- Features, UI and locales: @Toys0125, @cocodedk, @BlackGownQ, @Amineharrabi, @chetanvhsk-wq, @YunzhouLi-hub, @mervedalgin
- Build, packaging and tests: @ldevbr, @Lusitaniae, @mp-web3, @Alexandre200-lab, @maheshBawa
- Documentation: @err0rgod, @yeting1107, @henefesto, @mathkartel

## [0.6.0] - 2026-11-12

Machine-checked finding verification (S1), intercept decisions traced to their actor, configurable notification limits, prompt hardening and goal guards from the #124 agentic audit, the target gate (S3b phase 1 and step 2.1, off by default and API-only), intent claim leases, and checksum-verified tool downloads in the Docker image.

### Verified findings (S1)

- **POE level and verification status**: findings carry `poe_level` (0-4), `verification_status` and `verification_note`; a finding is `verified` only with a bound proof or verification response, a declared POE level of 3 or more, and every evidence quote of 8 runes or more found verbatim in a bound body. Existing rows stay `unverified` (#162).
- **Verification in the findings UI**: a badge (`Verified L3`/`L4`, `Unverified`, `Blocked`, `Not assessed`, `False positive`) beside the triage status, a filter, and the note on the detail page; the list, groups, asset tree and export accept `verification_status` (#163).
- **Retest linkage**: a completed retest with verdict `reproduced` or `fixed` upgrades an `unverified`, `not_assessed` or `blocked` finding to `verified`, with a note naming the retest; other verdicts never change the state (#164).
- **Evidence gzip decoding**: gzip and deflate bodies are decoded (one layer, 4 MiB cap, fail-closed) before the quote check, so compressed HTML can verify; brotli and zstd stay `not_assessed` (#169).
- **Counterevidence and confidence rationale**: findings take two optional free-text fields, `counterevidence` (what was tried and did not work) and `confidence_rationale` (why the conclusion holds), through `report_finding`, the findings API and the finding detail page; a missing field never refuses a finding. An idempotent upgrade adds both properties to the stored `report_finding` schema on existing databases and keeps user edits (#193).

### Intercept decision tracing

- **`decided_by` on approvals**: each settled approval records its actor (`human`, `rule`, `model`, `system`), shown in a "Decided by" column with a filter; legacy rows show "Legacy" and are not rewritten (#165).
- **Judge decisions no longer stored as `unknown`**: the `[model]`/`[Model]` prefix case mismatch is fixed, and an idempotent backfill relabels the affected rows `decision_source=model` (#159, fixes #156).

### Notifications (closes #168)

- **"and N more" counts the hidden items**: the notification asset list reported the total instead of the assets left out (#170); the malformed-IP asset warning had the same bug (#171).
- **Configurable limits**: `notify_summary_max_runes` (default 120, 0-4000) and `notify_assets_max_shown` (default 3, 0-50) in `/api/settings`, `0` meaning no truncation; the Feishu card guard drops from 24000 to 16000 bytes to stay under the 20 KB request limit (#172).
- **Limits on the settings page**: both fields under System > Notifications > Global settings, validated before anything is saved (#173).
- **Feishu digest overflow**: digests are packed against the serialized request body, so a digest of many short items is split instead of exceeding 20 KB and being rejected (#174).

### Agentic prompt hardening (#124 phase B)

- **Untrusted-content tails owned by code**: the planner and the compactor get an untrusted-data rule appended after their prompt body, and the #49 block moves out of the editable bodies into a tail appended for every chat agent (worker, main agent, auto, pentest, reporter), so a prompt override cannot remove it (#189).
- **Planner goal anchor**: the planner user message ends with a block that restates the task goal and constraints and asks each new intent to name the goal it serves (#189).
- **bbot skill rules**: every bbot event field is untrusted, only `scope_distance == 0` reaches `insert_assets`, and other events stay intel facts that are never auto-scoped or probed (#189).

### Agentic guards (#124 phase B)

- **`goal_met` refuses while a goal is open**: it names the open count and points to `prove_goal`; a task with no goal node keeps the old behaviour (#190).
- **`prove_goal` requires an observed fact**: a fact is accepted only with `confidence` `observed`; findings are accepted as before (#190).
- **Hint origin**: `add_hint` stores who wrote the hint (`human`, `orchestrator` or the agent name) and the planner trigger names it, instead of always "The human (main agent)" (#190).
- **Built-in rules for the planner and the main agent**: the `[built-in]` deny rules now apply to their Bash and other intercept-enabled tools; user rules and the judge still do not (#190).
- **Deny reasons to the model are capped**: the reason in a block message (deny rule, judge or target gate) is cut to 4000 runes, with a truncation marker, before it reaches the model; the reason stored in the interception log is unchanged (#192).

### Target gate (S3b phase 1 and 2.1): OFF by default, API-only

- **Opt-in only**: `target_gate` on tasks and conversations defaults to `false` and is set through the API (`POST`/`PATCH`); there is no UI. With it off, nothing below runs. Decision D2 (gate on by default) is NOT in 0.6.0 (#178).
- **Hooks and scope**: the planner and the main agent run with tool hooks, and every session carries a target-gate scope (#177).
- **Schema, flag and approval requests**: `target_pins`, one open request per scope and `host:port`, and a human approval that pins the displayed addresses; pin list and revoke routes (#178).
- **Extraction, resolver, exemptions, decision in the hook**: declared targets are extracted, block rules and exemptions applied, names resolved, and uncovered targets wait for a human approval (#179).
- **Rendered custom-tool targets**: the URL or command that a custom `http` or `command` tool renders goes through the gate (#180).
- **Inheritance**: a task spawned by a gated caller and a conversation triggered by a gated task start gated; pins are never copied (#182).
- **Declared form first**: block and URL rules match the declared targets before any DNS lookup (#183).
- **Redirect hops**: every redirect hop of WebFetch and of the custom `http` tool is checked; an uncovered hop fails with `TARGET_APPROVAL_PENDING` (#185).
- **Pinned dialer**: in a gated scope, WebFetch and a direct custom `http` call connect only to pinned or exempt addresses, on every hop, with no direct retry (#186).

### Other changes

- **Custom User-Agent for LLM requests**: an optional per-profile `User-Agent` (trimmed, no control characters, 256 bytes max); empty keeps the default. Part of #154, without the fixed-full-URL mode (#155).
- **Untrusted-output boundary toggle**: `ARTEX_UNTRUSTED_BOUNDARY=1` enables Norma's framing of tool results for worker, planner, chat and main agent; off by default (#166).
- **Intent claim lease and fencing (S3c)**: a claim has a 15 s heartbeat, a 90 s TTL and an owner token, so a stale worker can no longer overwrite a result; in-flight intents reopen when their claim expires, not at startup (#176).

### Fixed

- **Custom-tool proxy scheme**: `use_recording_proxy` produced `http://http://...`; a value with a scheme is now used as is and a bare `host:port` gets `http://` (#187).
- **`list_task_credentials` schema**: it emitted `"required": [""]`, which strict providers reject, failing every worker; fixed, with a one-time heal of existing databases at startup. Reported and first fixed by @kiserufetch (#130).
- **MCP protocol version**: the Streamable HTTP client negotiates the version (offers 2025-11-25, then 2025-06-18) and sends the negotiated one after `initialize` (#132, fixes #131).

### Build, Docker and tests

- **Docker tool verification**: the recon tools are checked against their release checksums, sqlmap is pinned to tag 1.9.9 at a fixed commit, Node 22.23.4 comes from nodejs.org verified against `SHASUMS256.txt`, and the Playwright packages are pinned. Carries over the Dockerfile part of #127 (#188).
- **Secrets out of the build context**: `.dockerignore` excludes `.env`, `config.json`, `jwt.key` and `backups` (#153).
- **Server tests skip without a DSN**: the Postgres-backed `./server` tests skip when no database is configured instead of failing (#175).
- **Norma v0.4.4 to v0.4.8**: v0.4.5 (#160), v0.4.6 (#161), v0.4.7 (#184), v0.4.8 (#186).

### Docs

- **LLM base URL note in the README**: the Anthropic base has no `/v1` suffix; OpenAI-format bases end in `/v1` (`/chat/completions` or `/responses` is appended). Follow-up to #154 (#157, #158).

### Known limits

- **Target gate**: OFF by default and API-only; turning it on by default (D2) is not part of this release.
- **Declared-input detection, not enforcement, for subprocesses**: the gate reads the targets a call declares; a subprocess that builds its target at run time, or a custom script tool's stored code, is not seen. Only WebFetch and direct custom `http` calls are bound at connect time; calls through a proxy are not (recording-proxy binding is step 2.2).
- **URL rules match the URL as written**, with no normalisation; use a domain, IP or CIDR rule to keep an agent away from a host (#183).
- **Fact provenance**: `prove_goal` trusts the `confidence` the model declared in `record_fact`; F4 fact provenance and the source check come in 0.7.0. There is no pending-proof UI.
- **Prompt rules are advisory to the model**: the tails, the goal anchor and the skill rules are prompt text, and their effect on model behaviour was not measured. The enforced parts are the `goal_met`/`prove_goal` handlers, the built-in deny rules and the target gate.
- **Stored tool descriptions**: the new `add_hint` and `prove_goal` descriptions reach fresh databases only (#190).
- **Intent claim leases need one version per database**: an older binary does not heartbeat, so a 0.6.0 instance takes over its live claims (#176).
- **Verification**: no path produces `blocked` or `false_positive` yet, and non-HTTP findings stay `unverified` until S1b.
- **Finding reasoning fields are not enforced**: the prompts and the `report_finding` descriptions ask for `counterevidence` and `confidence_rationale` when confidence is not high, but findings have no confidence field, so nothing requires them; a confidence field and its enforcement are 0.7.0 items. The prompt sentence reaches fresh databases only; the schema descriptions reach existing ones through the upgrade (#193).

## [0.5.1] - 2026-11-05

Login rate limiting per client IP with trusted-proxy support, a 12-character minimum for new passwords, a loopback-only default for the binary, and the JWT signing key in its own Docker volume.

### Changed

- **The binary listens on `127.0.0.1:8787` by default**: the `-addr` default was `:8787`, every interface. Running `artex` or `./start.sh` without `-addr` now accepts connections from the local machine only; pass `-addr :8787` to listen on every interface. Docker is unchanged: the image starts the server with `-addr :8787` inside the container, and `docker-compose.yml` already publishes it on `127.0.0.1` by default (`ARTEX_BIND_ADDR`).
- **New passwords need at least 12 characters** (was 8), at first-run setup (`POST /api/auth/init` and the `/setup` page) and in `POST /api/auth/change-password`. Login does not check length, so a password set under the old minimum keeps working until it is changed. `reset-password.sh` enforces the same minimum.

### Fixed

- **The JWT signing key survives container rebuilds**: the server wrote `jwt.key` to the executable's directory, `/app` in the image, so the key lived in the container's writable layer and recreating the container (for example `docker compose up -d --build`) generated a new key and signed every user out. The key directory is now set by `ARTEX_JWT_KEY_DIR` (default: the executable's directory, unchanged for non-Docker installs). `docker-compose.yml` sets it to `/app/keys` and bind-mounts `./keys` there, outside `./data`, which the file manager browses. On start, when `<key dir>/jwt.key` is missing, a key at `data/jwt.key`, then at the executable's directory, is copied there with mode 0600 and the old file is removed; otherwise a new key is generated. An existing key is never overwritten. Upgrading a Docker install signs every user out once, because the old key is in the container being replaced; to keep sessions, copy it out before recreating the container:

  ```bash
  mkdir -p keys
  docker cp artex-artex-1:/app/jwt.key ./keys/jwt.key
  ```

### Security

- **Login rate limit**: `POST /api/auth/login` allows 10 attempts per minute per client IP. The next attempt gets 429 with a `Retry-After` header (seconds) and a JSON error, and the IP is blocked for 1 minute. Each attempt while blocked, and each later block, doubles the wait, up to 15 minutes. A successful login clears the IP's count. There is no per-account lockout, so failing logins cannot lock the admin out from other addresses. The state is in memory: a restart clears it, an IP idle for 15 minutes is dropped, and at most 10,000 IPs are tracked (the least recently seen is dropped first). The IP is the TCP peer address, and `X-Forwarded-For` is ignored, unless the peer is listed in the new `ARTEX_TRUSTED_PROXIES` (comma-separated CIDRs, empty by default, passed through by `docker-compose.yml`). For a listed peer the header is read from the right and the first address that is not a trusted proxy is the client; entries the client added further left are never used. Behind a reverse proxy, set it: otherwise every client has the proxy's address and shares one budget, and an attacker failing logins through the proxy blocks the real users, admin included, for up to 15 minutes. The README nginx example now sends `X-Forwarded-For`.
- **`GET /api/health` hides the version from anonymous callers**: without a valid token it answers `{"ok":true}` only. With a token (the web UI sends one for the version in its top bar) the answer is unchanged: `ok`, `service` and `version`.

## [0.5.0] - 2026-11-04

Ollama support, a setup token for the first-run claim, secret scrubbing for child processes, a Docker image that builds from a fresh clone, Go 1.26.10 and Node 22, and race and shellcheck runs in CI. Entries marked "direct commit" landed on `main` without a pull request.

### Added

- **Ollama support**: an `ollama` LLM profile format for a local or LAN Ollama server. Keyless by design: no API key is needed, `OPENAI_API_KEY` is never inherited, the base URL defaults to `http://127.0.0.1:11434/v1` and is normalized to the `/v1` root, and the profile works in the failover pool, task LLM chains, model listing and connection tests. Contributed by @glauberportella (#128).
- **Setup token for first-run setup**: while no admin password is set, the server generates a random 128-bit one-time token at startup and prints it to its log (`docker compose logs artex | grep "Setup token"`). The `/setup` page has a field for it, `POST /api/auth/init` requires it (`setup_token`, compared in constant time, 403 when missing or wrong), and it is cleared once the password is stored (#137).
- **`ARTEX_BIND_ADDR`**: the address `docker-compose.yml` publishes the web UI on, default `127.0.0.1` (#137).

### Security

- **First-run claim**: before, anyone who reached port 8787 on a fresh install before the operator could set the admin password. Init now needs the setup token above, and Docker installs publish 8787 on `127.0.0.1` by default; set `ARTEX_BIND_ADDR=0.0.0.0` in `.env`, or use a reverse proxy or SSH tunnel, for remote access (#137).
- **Startup secrets removed from the environment**: `ARTEX_PG_DSN`, `ANTHROPIC_API_KEY` and `OPENAI_API_KEY` are unset inside `server.New` after they are read and before the MCP discovery goroutine and the task resume loop start, so agent shells, MCP stdio servers and custom tools spawned afterwards no longer inherit them (#136). The binary passes the option that enables this, but `New` did not apply its options, so the scrub from #136 never ran until #146 fixed it (#146). See Known issues for what this does not cover.
- **Saved API keys go only to their own endpoint**: `POST /api/llm/test` and `POST /api/llm/models` used a saved key (the profile's, or the global one) for any `base_url` in the request body, so a crafted request could send it to another host. A saved key is now sent only when the request's provider and normalized base URL match the ones it was saved for; otherwise the request must carry `api_key` (#142).
- **`ARTEX_LLM_PROVIDER` is matched case-insensitively**: `Ollama` or a value with surrounding spaces fell through to the Anthropic default and sent `ANTHROPIC_API_KEY` to the configured base URL (#140).

### Fixed

- **Ollama base URL on the environment path**: `ARTEX_LLM_BASE_URL=http://host:11434` is normalized to the `/v1` root the same way the saved-profile path already was, so a LAN Ollama server configured through the environment no longer answers 404 on chat (direct commit 1c35c9c).
- **Malformed `config.json` is an error**: `config.Load` used to ignore a parse error and treat the file as missing, which surfaced as a misleading "no database config found" or a silently ignored `skill_dir`. A read or parse error is now returned with the file path and startup exits (#141).
- **`install.sh` escapes what it writes**: the local install writes `config.json` string values through a JSON escaper and requires a numeric port; the Docker install escapes `\`, `|` and `&` in the Postgres password and API key before the `.env` substitution (#141).
- **`scripts/backup.sh --keep 0` is rejected**: it used to delete every dump, including the one just written, and still report success. Values with a leading zero are rejected too (#139).
- **`backups/` is ignored again**: a missing newline had merged it into the previous `.gitignore` line, so database dumps showed up as untracked files (#138).

### Build and CI

- **The image is built from the checkout instead of pulled**: `docker-compose.yml` no longer references the `autumn27/artex` namespace, whose repository is gone and whose name its holder could republish at any time. `install.sh` and `update.sh` build the image locally (direct commit 1ed154e).
- **A fresh clone builds the image**: the Dockerfile copied a prebuilt `dist/<arch>/artex` that a clean checkout does not have. It is now a multi-stage build: a Node 22 stage runs the frontend static export, a Go 1.26 stage runs `build.sh` with the release binaries job's flags (CGO-free cross-compile, `-trimpath`, version stamp), and the runtime stage copies the result (#147).
- **`update.sh` refuses to rebuild stale code**: when `git pull --ff-only` fails on a diverged clone (including every clone made before the 2026-10-23 history rewrite), it now stops with recovery commands instead of warning, rebuilding the old tree and printing "Build complete" (direct commit 1ed154e).
- **CI runs the race detector and shellcheck**: `go test -race -p 1` over every package with tests except `./server`, and `shellcheck -S warning` on `install.sh`, `update.sh`, `start.sh` and `scripts/backup.sh` (#144). The race run uses its own database (direct commit 0477751).

### Upgrades

- **Go 1.26.10** in `go.mod` (was 1.26.3), and **`golang.org/x/net` v0.62.0** (was v0.58.0), which raised x/crypto to 0.58.0, x/text to 0.43.0, x/sys to 0.49.0, x/sync to 0.24.0, x/mod to 0.41.0 and x/tools to 0.51.0 (#145).
- **Node 22 in the runtime image**: NodeSource `setup_22.x` instead of the end-of-life `setup_20.x`, matching CI and the release workflow (#143).

### Known issues

- **The secret scrub has limits**: `os.Unsetenv` changes only the Go runtime's copy, so a process running as the same user can still read `/proc/<pid>/environ` of the server (and, in Docker, of PID 1, `start.sh`). Other variables that may carry credentials, such as `ARTEX_LLM_PROXY`, are not scrubbed.
- **The LLM test and model-list endpoints still take `proxy` from the request**, so a saved key can travel through a request-supplied proxy when the base URL is plain HTTP.
- **Postgres passwords with URL-special characters** (`@`, `/`, `#`, `?`) break the `ARTEX_PG_DSN` that `docker-compose.yml` builds by interpolation, and Compose interpolates `$` in `.env`.
- **The build context includes local secrets**: `.dockerignore` does not exclude `.env`, `config.json` or `jwt.key`, so the build stage's `COPY . .` puts them in a build-stage layer of the local builder cache. They are not copied into the final image.
- **Image supply chain**: the recon tools are downloaded at pinned versions but not checksum-verified, sqlmap tracks its default branch, and the Playwright packages are installed at `@latest`. Draft #127 proposes checksum verification.
- **No Docker image is published for this release**: the release workflow's `docker` job only runs when `DOCKERHUB_ENABLED` is set, and it still downloads prebuilt binaries into `dist/` that the Dockerfile no longer uses. Build the image from the checkout with `docker compose up -d --build`.
- **CI gaps**: there is no `docker build` job. Four tests skip themselves (fresh-database or timing failures, see #9).
- **Reported and not fixed in this release**: `list_task_credentials` emits an invalid `"required": [""]` schema that strict LLM providers reject (#130), and the MCP Streamable HTTP client fails against servers that only support protocol version 2025-11-25 (#131; fix proposed in #132).

## [0.4.0] - 2026-10-23

First release of this fork. The engine underneath is still upstream's, but this tree carries a long stretch of operational work on top of it: a recon toolbox and skills, findings discipline, cost accounting, and container packaging.

### Added

- **Recon toolbox in the image**: nuclei, httpx, subfinder, katana, ffuf and dalfox installed at release-pinned versions, plus a sqlmap checkout that tracks the default branch, each smoke-tested at build time (run once, not checksum-verified), so hunt workers can call them directly (#112). Checksum verification arrives with #127.
- **Recursive recon skill** (`bbot-recon`): bbot v3.0.2 in its own virtualenv with module dependencies preinstalled. Policy-gated modes (passive seeds-only when scanners are banned, active-safe, loud only on explicit permission) and JSONL ingestion into the facts graph (#118).
- **php2shell skill**: turns a file-inclusion finding into verified RCE through the session.upload_progress chain, lab first and gated on program policy (#113).
- **Code audit and reverse-engineering skills** (#55, #77).
- **Goal ambiguity gate**: task creation rejects goals that name no target and goals under 80 characters; `[targetless]` opts out for tasks that legitimately have none (#111).
- **LLM chain hardening**: consecutive-failure escalation across the ordered profile chain, plus per-intent profile rotation with a strict round-robin (`llm_rotate`) (#111, #114).
- **Stagnation warning**: the graph overview tells a worker when it has repeated the same direction without producing findings (#111).
- **Per-task credentials vault**: accounts stored per task, agent-generated passwords allowed, injected into the worker context and managed from the task page.
- **Cost ledger**: token spend metered per call, with usage surfaced in the UI and judge/fallback calls counted separately (#71, #74).
- **Backup script and MCP health reporting** (#43, #44).
- **Ask-operator tool with environment handoff** (#45, #57).
- **DrostBench corpus and benchmark harness** (#59, #95, #96, #97, #98).
- **One database per test package**, removing cross-package test races (#82).

### Changed

- **Norma upgraded to v0.4.4**: per-task process-tree memory watchdog, UTF-8 aligned output truncation, non-interactive subprocess environments, message_delta usage recovery, and streaming fixes (#121).
- **Planner quiet mode** for less narration during long runs (#81, #88).
- **Findings discipline**: proof-basis gates, measured impact, adversarial verification and dedup rules decide what a finding must contain before it is reported (#51, #53, #63, #69).
- **Prompt-injection hardening** on seed material and MCP tool descriptions (#49).
- **Pause preserves the task timeout budget**, and paused time is frozen in the task clock (#2, #110).
- **Deadline contract** in the executor seeds (#65).
- **Task detail metrics and outcome cards** summarise a finished run (#92, #102).
- **Skill-gap and loop-contract reporting** for the planner (#104, #106); the RSI notes land as documentation in #108.

### Removed

- **In-app self-update and the automatic version check**: the update source was hard-coded to a repository that no longer exists, and its checksums came from the same release. The `selfupdate` package, the `/api/update/*` endpoints, the top-bar update badge and the "Version & Updates" settings card are gone; `start.sh` / `start.bat` now only restart the process after a crash. Upgrade with `update.sh` or a release archive. (#125, contributed by @stevenAthompson)

## [0.3.15] - 2026-10-07
>>>>>>> be8a79dd (fix(server): cap insert_assets request bodies)

### Interception

#### Added

- Added a built-in delete-path deny rule. Existing destructive HTTP rules recognized DELETE methods (`curl -X DELETE`, `requests.delete(`, `method:'DELETE'`), while path vocabulary covered only `/clear /wipe /flush /purge /truncate /drop /destroy /factory-reset /reset-all`. GET/POST deletion such as `curl 'http://t/api/user/delete?id=1'` could therefore delete real data without matching. The new rule covers `/delete /del /remove /unlink /erase /destroy`, including suffix forms `/deleteAll`, `/delete_user`, `/delete-user`, with boundaries avoiding `/delivery`, `/details`, `/delta`, `/delegate`. Its independent seed marker also installs it on upgraded instances; users can disable/delete it under System → Command interception.
### Finding notifications

#### Added

- Added System → Notifications with DingTalk, Feishu, WeCom, generic Webhook, Telegram and email delivery. Each type supports multiple instances, such as separate emergency/daily bots, with independent enablement, rate limits and filters.
- Notifications support immediate per-finding delivery or digests on a global interval, default 30 minutes. Digests begin with new-finding counts/time range and severity distribution. Separate channels can implement high-severity immediate delivery and other-severity digests without hardcoded policy.
- Four filter dimensions: minimum severity, task/asset scope, finding-type keyword inclusion/exclusion (exclusion wins), and disposition changes (off by default because notifications usually mean new findings). Individual messages link to details using the global backlink URL; blank URL omits the button.
- Delivery history lists status, attempts, failure reason and channel with channel/status filters. Manual resend resets retry counts, assuming the operator addressed the cause.
- Channels declare secrets via `SecretKeys()` (webhooks, signing keys, bot tokens, SMTP passwords). APIs return masks with trailing hints; submitting the unchanged mask preserves the value, while clearing removes it.

#### Fixed

> This section records an upstream security audit after the initial feature implementation. Each issue was reproduced, fixed and covered by a regression test.

- Fixed a critical mask bypass through changing destinations while retaining credentials. Separate destination/credential fields and merge-preservation of omitted keys let a destination-only edit send stored Webhook Authorization, Telegram URL bot tokens and SMTP passwords after STARTTLS to arbitrary hosts, silently and without redirects. The path was reproduced across four channels. Destination changes now require an explicit new value or explicit empty value for every secret; unchanged masks, meaning reuse, are rejected too. Secrets are not silently discarded because optional fields such as headers would otherwise lose authentication while the API reports success.
- Escaped untrusted Markdown in DingTalk, WeCom and Feishu, which previously lacked the protection already present in Telegram/email. Model-derived titles/summaries and target-controlled asset query strings could render attacker links or image beacons, revealing that a finding was read and the reader's IP. Content now becomes a single line with Markdown metacharacters escaped at each renderer. Escaping is not done in the shared title function, because Markdown backslashes leaked visibly into Telegram HTML.
- Hardened notification SSRF at dial time. Scheme/host-only validation previously allowed cloud metadata `169.254.169.254`, loopback and internal destinations; failed-response prefixes of 200 bytes entered `last_error` and history, exposing a partial internal-response read. Dial checks cover DNS rebinding and same-host redirects, while cross-host redirects are rejected because URL credentials would transfer. Loopback/link-local need `ARTEX_NOTIFY_ALLOW_LOCAL=1` for legitimate local SMTP relays. RFC1918 remains intentionally allowed for self-hosted Mattermost/SMTP, avoiding breaking normal private deployments.
- Redacted notification secrets from errors. `http.Client.Do` returns `*url.Error` containing full URLs with DingTalk access_token, WeCom key, Feishu hook IDs or Telegram `/bot<token>/`. These leaked to plaintext `notification_deliveries.last_error`, history bypassing masks, logs and test-send UI errors. Errors now retain only scheme://host and the underlying DNS/connectivity/certificate cause, dropping paths/queries. URL-parse validation failures get the same treatment. The preceding fix missed parsing errors and its tests only hit scheme validation; genuine coverage now exercises the missed branch.
- Fixed silently lost digest entries after truncation. Channel limits (WeCom's 4096 bytes being tightest) previously truncated a message but marked its whole batch delivered; omitted findings appeared neither in messages nor failures. Packing now keeps whole entries, marks only included ones delivered, requeues the remainder, and states “first N shown, M continue next”. Deferral refunds the optimistic attempt increment, preventing a 500-entry backlog's tail from exhausting retries by the third segment without an actual failure.
- Validate `min_severity` on writes. Typos such as `hgih` previously ranked as 0, making rank >= 0 always true and silently sending every finding despite an apparent high-only filter. Errors list valid values; reads tolerate legacy invalid values so channels remain readable.
- Restored explicit `rate_per_min=0` as unlimited. Storage had replaced <=0 with defaults 20 (DingTalk/WeCom/Telegram) or 100 (Feishu), contradicting docs/UI/token buckets. Defaults now apply at the API only when omitted, distinguishing absent from explicit zero.
- Digest delivery now honors token buckets: `takeTokens` had deducted `allow` without using it, making rate_per_min ineffective. Claim counts now obey both remaining allowance and memory bounds.
- Failure handling now evaluates each delivery's attempts, not the batch maximum. Previously an older twice-retried item could permanently fail new entries with no retries used. Permanent errors fail immediately, exhausted entries fail individually, and others requeue with their own backoff.
- Unparseable digest snapshots now fail explicitly with a history reason. Previously rendering skipped them but batch success marked them delivered.
- Bounded per-channel round size by lease duration divided by per-send timeout. A three-minute lease could expire during a long serial batch, letting another instance reclaim and duplicate sends/attempts. An assertion locks the constants' relationship; it exposed an old limit of 6 using the entire lease without margin, changed to 5.
- Telegram truncation avoids incomplete HTML entities as well as tags; an `&amp` fragment could otherwise make the parser reject an entire long digest.
- SMTP 4xx such as greylisting 450 now retries, 5xx fails permanently, and absent codes default to retry. Previously every temporary rejection was permanent, causing greylisted servers to fail each notification on its first attempt.
- Reject nested mask sentinels inside objects such as webhook.headers. These fields must be wholly masked or wholly submitted; nested sentinels previously persisted as real values and silently broke later authentication rather than preserving secrets.

#### Design notes

- `RecordFindingTx` writes `notification_events` with one blind INSERT in the finding transaction, pairing finding/event persistence on successful insertion. It deliberately does not read channels or execute user filters, so bad filter configuration cannot disrupt finding storage. The INSERT is wrapped in SAVEPOINT because a PostgreSQL statement error otherwise aborts the transaction, including COMMIT; notification insertion failure is logged without blocking the finding.
- Delivery claims use `FOR UPDATE SKIP LOCKED`, set sending and a future next_attempt_at lease, then commit before network work. No DB lock is held during delivery; sending rows left by crashes are reclaimed after expiry without unbounded retries.
- Rate limiting does not consume retry budgets. The engine computes token allowance before claiming exactly that many rows; claiming then discarding would burn the three attempts merely waiting. Excess work moves to the next tick without loss.

### Test infrastructure

#### Fixed

- Fixed permanent test-data residue in the company ICP ownership test. Cleanup used t.Cleanup but defer d.Close ran first, so cleanup hit a closed connection and discarded errors via `_, _ =`. Its fake TaskID MAX(companies.id)+1 could collide with other tests, breaking exact asset counts. Closing now uses an earlier-registered t.Cleanup so LIFO cleanup precedes it, and failures surface. More than ten similar patterns remain in db; this change fixed only the empirically triggered case.

### Traffic

#### Added

- Traffic Clear all ignores filters, deletes all records and orphaned legacy host directories, then runs optimize/VACUUM/wal_checkpoint(TRUNCATE) and reports freed space. Independently stored finding evidence remains intact. VACUUM on the empty DB is cheap and converts existing indexes to incremental reclamation, enabling later host deletions to reclaim space automatically.

#### Fixed

- Fixed traffic deletion retaining disk space. SQLite placed pages on freelists without auto_vacuum, while contentless_delete ex_fts wrote tombstones without reclaiming postings, so deletion could grow the index. Bodies below 256KB are inline and trigram indexes roughly double body size: a measured 6MB capture left a 16MB index even after deletion. New indexes enable auto_vacuum=incremental. After deletes, background chunks merge full-text indexes, incremental_vacuum and truncate WAL; the same case fell to 104KB. Chunks release write locks between work, avoid blocking capture, yield on shutdown and resume on the next deletion.

> Upgrade note: existing indexes retain their old auto_vacuum mode, so incremental_vacuum is a no-op and startup logs a notice. Full-text merges already stop tombstone growth. Clear all reclaims old space and converts the empty index once; subsequent ordinary deletions reclaim incrementally.

### LLM

#### Fixed

- Fixed custom session headers on one-shot LLM calls. agentcore only placed session IDs in context with a transcript store; round-zero goal decomposition and cold compaction body calls had none. Gateways requiring x-opencode-session returned 400 MissingSessionID while later planner turns worked, and compaction failures were hard to connect to a lone log. Both now use exploration-stable `exp<N>-goals` / `exp<N>-compactor` IDs, restoring headers and previously missing llmrec token attribution.

### Findings

#### Fixed

- Fixed missing scrolling in By asset. The outer card had only max-height, leaving height:auto so the inner 100% scroll viewport could not resolve; large trees overflowed or clipped. The native tree scroll container now receives responsive max-h with overflow-y-auto, shrinking for small lists and scrolling at the cap for large ones.

## [0.3.14] - 2026-09-24

### Task list

#### Added

- Added a findings column to the task list, showing critical/high/medium/low counts. Nonzero counts use severity colors to show each task's finding volume and distribution.

#### Changed

- Narrowed description/objective columns. Long content truncates with full text on hover, reducing horizontal crowding.

### Exploration graph and planning overview

#### Changed

- Reduced `graph_overview` size and capped lists. Recent facts, completed/pending intents, cold summaries and confirmed findings now show a recent window plus total counts; omitted entries remain queryable. This reduces per-turn LLM context and growth in long tasks, including cold sections of associated-task overviews.

#### Fixed

- Removed reverse duplicate edges between digest and finding nodes after cold-node folding.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.13] - 2026-09-19

### Asset interception

#### Added

- Added global asset deny rules for exact/fuzzy domain, IP and URL matches and CIDR ranges. System → Asset interception supports create/read/update/delete and enable/disable. Built-in fuzzy rules block government (`.gov` / `.gov.cn`) and education (`.edu` / `.edu.cn`) sites by default.
- Integrated asset interception into execution. Before `add_intent` or `insert_assets`, the agent's target is checked. Blocked intents are not dispatched and blocked assets are not inserted; asset details and the reason return to the agent.
- Added task-local deny/allow rules independent of global rules. Deny is evaluated first. If no deny matches but configured allow rules also do not match, testing is disallowed. With no allow rules, allowlisting is inactive. Configure at task creation or manage and toggle in task Overview.
- Task templates can store a category and task-level deny/allow rules, applying both to new-task forms.

### Operation review

#### Fixed

- Tightened model-review output: reduced `comment` from 500 to 120 Chinese characters and required JSON only, without introductions or code fences. This reduces `MaxTokens` truncation, parse failures and unintended fail-open decisions under the configured model-failure policy.

### Task archives

#### Fixed

- Archives now skip and log symlinks instead of failing the entire task archive. The format supports regular files/directories only; other files still archive normally without following links or leaving the directory tree. Previously any symlink caused complete failure.

### Accounts and compliance

#### Added

- Added a pre-login usage notice/disclaimer dialog requiring agreement before sign-in.

### License and dependencies

#### Changed

- Adopted AGPL-3.0 and expanded README license/disclaimer information.
- Upgraded norma to v0.4.1.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.12] - 2026-09-17

### Exploration timeline

#### Added

- Added an exploration timeline to task details (#144), displaying start/objective/intent/fact/finding/hint/digest nodes with type filters, keyword search, ascending/descending order, pagination and refresh. The first newest-first page updates live; elsewhere only unread counts increase, preserving the reading position, with an “N new updates · Return to latest” action. Daily grouping keeps dates clear in long tasks.
- Timeline rows show node IDs (#145) for graph comparison and precise lookup.
- Expanded timeline details show upstream/downstream relations and anchored assets (#147). Hovering related nodes opens type/status/source/time/summary/payload previews; assets show type labels and readable text. Data arrives with the timeline page, so expansion needs no extra request.
- Timeline search also matches node IDs (#150): plain digits or displayed forms such as `#41` locate exact nodes in addition to content/source search.

### Intent management

#### Added

- Intent deletion supports soft and hard modes (#149), selected in confirmation. Pending, running and paused intents can be deleted with a required reason.
  - Soft deletion (default) marks the intent deleted, stores the reason separately and retains the node, outputs and lineage.
  - Hard deletion physically removes the intent and exclusive descendants supported only by it, cascading output/intent chains to leaves without orphaned data. Token accounting remains archived on original dates. Shared nodes, objectives and task-root facts are retained; confirmation shows the expected cascade count.

  Both modes notify the planner that the user deleted the intent, including the reason, and trigger replanning.

### Operation review

#### Added

- Approval history supports status and decision-source filters (#139).

#### Fixed

- Pending approvals load independently of history pagination (#133), remaining fully visible while browsing older records.

### Agent

#### Changed

- Updated the Worker role description to general cybersecurity-platform wording (#138).

#### Fixed

- Connected Compactor to task planners, restoring cold-digest compaction that previously never ran because the integration was absent.

### Chat

#### Added

- Chat supports @ references to multiple record types, with scrolling pagination for candidates (#135).

#### Fixed

- Constrained long message bubbles to the conversation panel (#137).
- Fixed missing-file uploads before a new conversation exists. Composer snapshots `FileList` into an array before clearing the input and calling back. Previously asynchronous conversation creation resumed after the live input-bound `FileList` had emptied, omitting the file field and returning HTTP 400.

### Traffic

#### Fixed

- Traffic capture proxy now binds only to 127.0.0.1 by default (#129, #130), avoiding an open proxy on other interfaces.

### Web

#### Fixed

- Restored the global header on the statically exported task-list page.
- Added missing traffic mocks to demo finding details, fixing a blank page.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)
- [@dingpotian](https://github.com/dingpotian)

## [0.3.11] - 2026-09-15

### Operation review

#### Added

- Approval records can locate their originating tool execution (#125). Clicking Source opens the complete original conversation, pages to the target, expands commands/results and centers a highlight while keeping surrounding messages readable. Manual scrolling stops automatic repositioning. Persisted `tool_use_id` and task mappings, with a new scoped call-ID index, locate ordinary chat, Workers, planners and segmented MainAgent conversations. Missing, duplicate or ambiguous links produce explicit messages instead of navigating to another execution; deleted conversations, archived tasks and absent records are also explained.
- Added approval-history pagination (#115).

#### Changed

- Reduced model-review input (#124, #125) to versioned JSON containing the current complete tool call, explicitly selected brief context and local working directory. Context includes only real current user messages from chat/task MainAgent. Workers omit intent summaries and inherited parent context; planner/automatic sessions do not invent user messages. Task descriptions, objectives, constraints, global exploration state, historical calls and complete Worker intents still serve execution and separate session audit, but are excluded from operation review. Decisions must return JSON `decision`/`comment`, explaining the actual action, successful consequences and matching rule. Instructions embedded in arguments/context cannot alter review policy. Actual submitted input snapshots and fingerprints are saved; old snapshots retain version labels and are never reconstructed from current data.

#### Fixed

- Fenced model verdicts no longer silently fail open (#126). Code fences around returned JSON are stripped before parsing; only a remaining parse failure invokes the configured model-failure policy. Previously fenced JSON failed parsing and could be allowed silently.

### Agent

#### Added

- Added experimental noa context compaction with norma v0.4.0, off by default in system settings. When enabled, model-driven noa replaces built-in compaction for MainAgent, planner, Worker and chat. Original compacted content is archived centrally under `<workDir>/noa/<sessionID>/`, globally unique per session rather than scattered across tasks. Integration failure falls back without interrupting real tasks. The toggle is read once per run and affects only later starts.

#### Fixed

- Exploration tools return an explicit error without task context instead of panicking on a nil store.

### MCP

#### Added

- Added legacy SSE-only MCP server support (#117).

### Traffic

#### Fixed

- `traffic_search` host matching now includes ports, preventing records for the same host on different ports from mixing (#114).
- Isolated `traffic_search` description-migration failures so subsequent reporter migrations continue.

### Web

#### Changed

- Added separators between task finding-retest options for clearer display (#122).

#### Fixed

- Fixed a complete demo task-details crash. The conversation tab requested `GET /api/tasks/<id>/side-questions`, absent from mocks; a plural-path fallback returned `[]`, leaving `data.items` undefined. The side hook's `merge()` threw `TypeError: t is not iterable` in a `setItems` updater, rethrown during React render outside the caller's catch, triggering “This page couldn't load”. Mocks now return explicit empty side history, and `sideAPI.history` normalizes non-array `items` to `[]` as a second defense.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.10] - 2026-09-13

### Network

#### Added

- Added official DeepSeek web search using the active LLM profile. Unlike the other three sources, DeepSeek exposes search only as its Anthropic-compatible server tool `web_search_20250305`, executed remotely. It requires an official DeepSeek endpoint and Anthropic protocol; OpenAI-format endpoints reject server tools. Each search consumes another model call, bypasses the search egress proxy and traffic capture, and returns only titles/links, with WebFetch needed for body text. Settings explain but do not enforce these constraints; users can verify with Test search.

### Agent

#### Added

- Added cross-work review for Workers: `search_all_worker_traces` searches matching steps across this task without knowing intent_id; `get_worker_trace` lists a selected work's steps, searches within it and retrieves full content by step_id. This reuses observations absent from facts and avoids repeated work.
- Added `node_detail` to Workers so intent/node IDs from trace tools can resolve to complete details.
- `add_hint` now explicitly announces the planning turn it triggers. Rather than only folding hints into overview, each call records one “User added N strategic hints” trigger, including batch contents without one event per hint. The planner sees both the cause and content.

#### Changed

- Relaxed global-overview wording to encourage exploratory breadth and prompt cross-intent clues instead of premature convergence.
- Simplified Worker boundaries: an initial obstacle does not establish exhaustion; complete reasonable methods within the intent before concluding.
- Removed per-asset `related` from `insert_assets`. It only controlled task-scope inclusion and was never stored, so re-registration erased its effect and the UI could not show exclusions. It required an extra model judgment that could not be retained.
- `task_scope` no longer depends on the asset-coverage toggle. `insert_assets` always adds automatic scope (`source='auto'`), and `add_task_scope` remains available to planner, task MainAgent and objective decomposition. Scope defines authorization and asset-query filtering; coverage only decides whether to use it as a metric denominator. Previously disabling coverage stopped both auto/agent writes, leaving only manual UI rows. `list_untested_assets` still hides with coverage because it is purely a coverage view.

#### Fixed

- Fixed cross-task asset leakage (#59). `list_assets` had hardcoded task ID 0 and queried the shared database without scope filters the model could supply, misdirecting it toward other tasks' assets, especially IPs. It now returns only this task's and directly associated tasks' `task_scope`, matching ownership rather than literal values: root domains include child domains/services/endpoints; networks include hosts/services. Direct IDs outside scope also fail. Non-task Auto/pentest contexts retain whole-database fallback. Fixed direct-IP URLs such as `http://1.2.3.4/api` missing CIDR ownership. The producer-filtered UI Test assets view is unchanged.
- Fixed an old tool-reduction migration removing newly added Worker trace tools. Removed `search_all_worker_traces`, `get_worker_trace` and `node_detail` from the unbind list and added a one-time restoration for databases that already ran it.
- Fixed intermittent `/btw` checkpoint storage failures by stripping JSONB-unsupported NUL (`\u0000`) escapes before persistence.

### Triggers

#### Fixed

- Deduplicated descriptions/objectives by task in merged trigger sessions so repeated long objectives do not inflate context.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.9] - 2026-09-11

### Agent

#### Added

- Task MainAgent supports multiple interactive conversations with creation, switching and independent context reset.
- Added persistent `/btw` side questions without interrupting the main flow; exchanges survive restart.
- `spawn_task.source_task_ids` lets new tasks inherit source assets/findings read only.

#### Changed

- Disabled cross-engagement Worker memory and narrowed default tools. Context reading and cross-work review belong to planning; Workers execute and record one intent.
- Simplified Worker prompts and asset-tool descriptions; removed `terminated` and `worker_name` from `get_worker_output`.

#### Fixed

- Fixed premature completion after thinking-only turns. A model could emit reasoning without text/tools, yielding natural `end_turn` without `tool_use`; the harness marked an unfinished intent completed with an empty summary. None of five retry layers handled it: no error for stream retry, `err == nil` satisfied the breaker, intent retry required `model_error`, and SDK emptiness counted reasoning events as output. The work Stop hook now recognizes this and injects a continuation instruction, preserving reasoning for the next action. It intentionally does not resend the same request, because this often follows stable prompt/context structure and would merely repeat the thinking.
- This continuation reuses the LLM Retry/backoff empty-response retry count: default 2, `-1` disables it and restores completion on an empty turn. It caps the total per intent, not consecutive turns. The harness already permits only one push per consecutive idle stretch and refreshes that allowance after a real tool turn; the total cap prevents tool → idle → push cycles exhausting the intent budget. Logs record the Worker/intent and continuation count `(n/N)`. The upstream design reference was `docs/LLM重试设计.md` §1.1 (not included in this baseline's tracked documentation).
- Fixed `/btw` context budgets/input layout for long conversations and added a request-ID fallback in insecure, non-HTTPS contexts.

### Traffic

#### Added

- Findings support multiple traffic evidence records with ordering, notes and roles (request/response/supporting evidence).
- Report agents automatically attach relevant traffic before writing reports.
- Added an agent traffic-binding toggle and completed evidence handoffs between agents.

### Tasks

#### Added

- Added conversation-level finding retests in separate agent sessions, with running state in lists/details.

### Interception

#### Added

- Approval records include details and execution audits: tool context, initial model/rule verdict, output and argument fingerprints.

### Assets

#### Added

- Task test assets support DSL search.

### LLM

#### Fixed

- Connection tests now include custom session headers, fixing opencode zen HTTP 400 responses.

### Network

#### Added

- MCP HTTP transport can skip TLS certificate verification for self-signed services.

### UI

#### Added

- Conversation lists group by agent with collapse/expand and independent pinning; chat supports agent filters.
- Attack graphs render digest nodes and fold their members.

#### Fixed

- Fixed blank screens caused by unsynchronized login credentials.
- Interception messages explicitly identify platform controls so they are not mistaken for target defenses.

### Deployment and updates

#### Added

- Added in-page updates: a Version and updates card and top-bar notice. The flow downloads a release, verifies `SHA256SUMS`, smoke-tests, stages, exits and lets the supervisor restart/apply it; the page refreshes automatically.
- Added `start.sh` / `start.bat` supervisors as official upstream entry points, included in release archives/images without changing `install.sh`. They restart by exit code and forward SIGTERM to artex; verification/replacement remain in Go.
- Failed verification/smoke tests discard the update and keep the current version. Three consecutive new-version startup failures roll back. Settings also allow manual rollback; database schemas do not roll back.
- Updates accept only GitHub domains over HTTPS, with a fixed release source; development builds disable them. GitHub results cache for 30 minutes to protect API quota.

#### Known limits

- In upstream Docker deployments, updates replace the program only. Toolchains stay unchanged and recreating a container restores its image version; the historical command was `docker compose pull artex`. ARTEX's current source-build instructions are in README.
- Updates do not synchronize release `skills/`; new bundled skills do not appear automatically.
- Updates restart the process and interrupt running tasks.

### Dependencies

#### Changed

- Upgraded norma to v0.3.7.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@RuoJi6](https://github.com/RuoJi6)

## [0.3.8] - 2026-09-09

### LLM

#### Added

- Added Retry and backoff settings for five nested layers: SDK connection retry before streaming (reset/timeout/429/5xx), SDK empty-content retry (OpenAI format), same-provider replay before any output reaches the caller, round-robin circuit-breaker cooldown after consecutive failures, and whole-intent retry after Worker `model_error`. Inner layers exhaust before outer ones. Each has count/interval controls: blank retains defaults/exponential backoff, count sets attempts, interval selects fixed delay, `-1` disables. The first three support per-profile field overrides of global defaults, so setting only delay still inherits count; breaker/intent retry are process-global. Saves apply live. Blank settings preserve old behavior byte-for-byte (new columns 0; absent settings use defaults). Connection tests omit retries because their hard 30-second timeout could otherwise reject working endpoints. The upstream design reference was `docs/LLM重试设计.md`, absent from this baseline's tracked docs.
- Added per-profile `session_header_key`. When nonempty, every request sends the current session ID (`conv-<id>`, `exp<x>-worker-i<intent>`, etc.) under that header for gateways using prompt caching/sticky routing. IDs come from request context without modifying norma; shared providers can send different session values. Includes old-database migration and a configuration input.

#### Fixed

- Fixed `23502` when saving empty session headers. The `NOT NULL DEFAULT ''` column had incorrectly used `NULLIF($n,'')`; it now receives empty strings directly.

### Agent

#### Added

- Wall-clock timeouts now finalize in place with norma v0.3.6: `MaxDuration` interrupts active tools, then runs configured wrap-up turns on a live context to save findings and summarize, ending as timeout. Worker/planner no longer rely on external `maxDur+90s` contexts that killed stuck runs as `aborted_tools`. Chat inherits the harness behavior; MainAgent without `MaxDuration` is unaffected.
- Added stall recovery: heartbeat/no-change planner wakeups with no open/running intents begin with an explicit no-worker/no-queue warning and must produce at least one nonduplicate intent.

#### Changed

- Moved Worker intent, start instructions and anchored-asset raw JSON into the system prompt, rebuilt each turn and retained through compaction/resume independently of the first transcript message. The initial user message now only contains a degradable, possibly stale global overview. Per-intent system data sacrifices cross-intent cache reuse to preserve intent.
- Simplified planner/Worker defaults. Planner distinguishes `recent_done` states, checks traces before treating blocked/exhausted as dead ends or retrying, treats negative results as tentative observations and checks evidence, requires output when goals remain with no open/running intents, and prioritizes depth over coverage. Workers record negative observations with tentative interpretation; planners decide. Cross-intent clues go into fact summaries rather than being pursued by the Worker. Reseeding appends and activates a new version while retaining custom/old versions for rollback.
- Narrowed Worker tools to single-intent execution/recording. Removed `list_facts`, `node_detail`, `list_companies`, `search_all_worker_traces`, `list_worker_traces`, `get_worker_trace`; retained `list_findings` for deduplication, `add_finding`/`record_fact`, `insert_assets`/`list_assets`. Removed stale `type=tech`/`on_url`/`props` guidance conflicting with schema. A one-time migration removes Worker bindings only, leaving planner/MainAgent bindings.

### Exploration graph

#### Added

- Added cold-digest graph compaction. Old inactive intents/facts fold into overview digest nodes; originals remain permanently addressable by ID, making presentation folding reversible and storage lossless. Reverse reachability and any-live-branch rules classify heat, with R=6-turn debounce and connected components. Background minor compaction covers new cold blocks; major compaction rereads originals and merges fragments. Activity rechecks and cooldown exclusion keep it off active paths and prevent overwriting revived nodes. Overviews expose `cold_digests` and asset-indexed `cold_index`; planner/MainAgent-only `expand_digest`/`expand_index` restore them. Associated/inherited tasks reuse their own folded views, including cross-task read-only expansion. Includes old-database migrations.
- `graph_overview` now includes the full `finding_list`, unlike windowed facts, because confirmed findings are valuable and usually few. Planner/Workers see all each turn without `list_findings`. Entries are `{id, summary, evidence?, from_intent?, assets?}`, with readable URL/domain/ip:port assets instead of bare IDs.

#### Changed

- Removed flat `hosts` from `graph_overview.coverage`, retaining `host_count` and on-demand `list_assets`; up to 500 host strings per turn offered little planning value. Added `done_intents_total` beside the ≤15-entry `recent_done_intents` window so deduplication can detect omitted history.

### Tools

#### Changed

- Moved default `add_company_scope` binding from Worker to planner: company scope is a planner/MainAgent/Auto responsibility. New databases seed mainagent/planner/auto; existing databases receive a one-time migration.
- Planners now default to both `list_assets` (DSL whole-database search) and `list_untested_assets` (untested scope). A one-time backfill preserves user removals.

### Tasks

#### Added

- Added Running Workers to task lists, counting `state='running'` intent nodes exactly as task Overview does, including 0 when none run.

### Network

#### Added

- Added an independent global egress proxy (http/https/socks5, optional `user:pass`). With capture enabled it is the MITM proxy's upstream for both intercepted and passthrough traffic, retaining capture without leaking the source IP. With capture disabled it enters agent Bash/WebFetch settings; `proxyEnv.ALL_PROXY` supports socks5. Stored in settings KV without migration, it has a System Global proxy card separate from search and LLM proxies.

### UI

#### Fixed

- Corrected intent labels: `exhausted` means budget exhausted, not a fully explored direction; step/time limits can leave partial results. `blocked` means execution error after model/API/network retries, not target/WAF blocking. Added missing `stopped` for user-stopped work.

### Dependencies

#### Changed

- Upgraded norma to v0.3.4 for MCP output truncation/disk storage (`651b961`); later v0.3.6 supports in-place timeout wrap-up.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.7] - 2026-08-31

### LLM

#### Added

- Added per-profile output limits and selectable request field names. The limit caps generated tokens per response; 0 omits the field for server defaults. Context window is separate, used locally for compaction thresholds and not sent. Only OpenAI Chat Completions allows a field choice: blank sends `max_tokens`, common in compatible gateways; official o-series/GPT-5 reasoning endpoints require `max_completion_tokens` and reject the former with `unsupported_parameter`. Anthropic fixes `max_tokens`, Responses fixes `max_output_tokens`, so other formats disable/clear the selector. Connected limits across planner, Worker, chat, MainAgent and goal decomposition; previously OpenAI omitted them and Anthropic used SDK 8192 regardless of settings. Like streaming, values resolve each turn and follow failover on the next turn. Defaults 0/empty preserve old behavior.
- Connection tests log every HTTP attempt's status and raw gateway body, capped at 4K, independently of LLM recording. This exposes 401/quota messages, empty frames and HTML responses beyond the UI's compressed ok/error.

#### Changed

- Task LLM chains are editable in every state, including done/failed/timeout, because MainAgent conversations still use them. Removed terminal-state restrictions from HTTP/DB transactions and enabled UI editing. Saving a terminal task no longer reopens quota-blocked intents into unexecuted, non-retryable open states. To continue execution, retry intents or add goals, which return the task to running.

### Agent

#### Changed

- Worker steering messages now reuse pause/resume and transcript resume like MainAgent, replacing a custom persistent intervention protocol that affected scheduler barriers, recovery and many activity filters. Messages enter the next input turn; intents run in a dedicated goroutine outside the three-slot pool. The UI keeps messaging and restores Direct continue, sent over SSE. No schema change. Messages are memory-only without crash recovery, and a steering message can briefly add one concurrent Worker; this was accepted for infrequent use.

### Tasks

#### Fixed

- Fixed large-task archive OOMs by streaming snapshots instead of loading entire tasks. Closed three cold-archive recovery gaps: modern traffic exists only in SQLite, and a crash between PostgreSQL/SQLite commits could strand archived traffic in hot storage. Staging journals now write regardless of legacy directories, and missing `journal.json` is treated as disposable.
- Reduced slow chat/UI loading with many tasks by optimizing task-list/context/exploration queries and duplicate requests in dashboards, conversations and task details.

### Assets

#### Changed

- Removed the 256-rule company-scope cap from backend, frontend and demo mocks. Individually listed IPs/domains reached it unnecessarily and forced company splitting; scope performance did not justify it. Retained 1024 characters per rule and a 2 MiB request cap, roughly 40–50K rules.

### Skills

#### Fixed

- Fixed skill ZIP uploads using bzip2/Zstandard, adding pure-Go decompressors beyond the standard library's Store/Deflate without new external dependencies. Unsupported Deflate64/LZMA/XZ/PPMd and encrypted archives now fail before extraction with a readable Chinese message naming the file/method and repacking instructions rather than a raw English ZIP error.
- Replaced ASCII-only skill path validation with a Unicode denylist, allowing multilingual filenames and spaces. Still rejects controls, invalid UTF-8, zero-width/bidirectional controls (including RLO disguise), `\ % # ? * : " < > |`, `..`, absolute paths and empty segments, preserving traversal protection. Previously one Chinese filename rejected the entire ZIP. Skill names now allow non-ASCII letters; ASCII remains lowercase letters/digits/hyphens per agentskills.io, with no spaces, dots or separators.
- Fixed Chinese ZIP names from Windows tools lacking the UTF-8 flag by decoding GBK before validation. Quoted non-ASCII frontmatter names now parse correctly too.

### UI

#### Added

- Added default All findings flat view alongside task grouping via tabs. A cross-task table supports 10/20/50/100 rows, selection/export, inline evidence/report expansion, editing name/type/severity/disposition, deepening and deletion just like grouped view. Both share stats/filters; switching preserves filters and saves view/filter preferences locally. Polling targets only the active view; inline edits update both caches to avoid stale switches.
- Added By asset view: a left tree of company → root domain/IP → subdomain → service → endpoint, showing company only when owned, and findings for the selected subtree on the right using shared filters/table. Only assets with findings appear; ancestors are reconstructed, counts aggregate/deduplicate findings, and missing/unlinked assets go into Unassociated. Unlike other views it queries only on entry, filter changes, finding edits or manual refresh, avoiding five-second navigation-tree rebuilds. Labels show relative increments (subdomain without root suffix, `https :443`, endpoint path); full values appear in tooltips/breadcrumbs. Excess nodes drop whole endpoint/service levels with a notice while preserving counts in ancestors.

#### Fixed

- Collapsed the conversation list on narrow screens so mobile conversation content is no longer squeezed.

### Dependencies

#### Changed

- Upgraded norma v0.3.2 → v0.3.3 with `Config.MaxTokensField`, allowing Chat Completions to send `max_completion_tokens` for reasoning models. It sends exactly one of the two fields; default remains `max_tokens`.
- Upgraded `golang.org/x/mod` v0.37.0 → v0.40.0, resolving two dependency security alerts.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)
- [@begininvoke](https://github.com/begininvoke)

## [0.3.6] - 2026-08-27

### LLM

#### Fixed

- Fixed non-streaming settings appearing to revert (#69). Storage was correct, but the profile-list DTO omitted `streaming`, so undefined became the frontend's streaming default. Restored the field without `omitempty` so false is returned.
- Connection tests now require actual model content and use the configured streaming mode (#65). Previously HTTP success with zero content (reasoning budget exhaustion, safety removal or compatibility-layer loss) passed, and non-streaming profiles were always tested through streaming. Empty replies now fail; successful tests show the reply and reflect the same channel used by conversations.

### Agent

#### Changed

- `list_facts` now paginates and filters keywords (#74): latest 20 by default, `limit` up to 100, `before` cursor, `q` summary search, returning `{facts,total,has_more,next_before}`. Long summaries truncate by character count; `node_detail` retains full text. Worker/planner prompts match pagination. A one-time schema refresh fixes existing tool catalogs because `SeedTool` only inserts initially, otherwise showing no parameters.

### Tools

#### Added

- Tool execution adds statistics (#72): a dialog shows per-tool count, share and failures sorted by count. It uses task/keyword filters over the complete result set, fetching only when opened.

### Dependencies

#### Changed

- Upgraded norma v0.3.1 → v0.3.2: preserved dropped OpenAI reasoning/refusal (including Responses `reasoning_text`), deduplicated three reasoning field names, bounded zero-content retries, and fixed compaction-split tool pairs causing gateway 400s, including existing transcript orphans.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)

## [0.3.5] - 2026-08-25

### LLM

#### Added

- Added per-profile streaming toggles, default true. Off uses real `stream:false` complete JSON to avoid faulty SSE empty/reasoning frames, sacrificing live progress/token counts. Worker/planner/MainAgent/chat/goals resolve the active profile dynamically. llmpool, llmrec and task-runtime provider wrappers support non-streaming. Added/migrated `llm_profiles.streaming` default true without changing old profiles.
- Added `openai-responses` profiles alongside Chat Completions/Anthropic, using `POST /v1/responses`, normalized BaseURL and default `gpt-5`. Updated format constraints idempotently and added the UI format option. Upgraded norma to v0.3.1, including `reasoning_content` round-trip repair.
- Captured raw LLM HTTP bodies at transport level, retaining tool schemas, `tool_use` blocks and SSE frames absent from normalized views, including each norma retry attempt. Added/migrated `llm_records.raw_request/raw_response`; details now switch to raw view and copy either body, with `execCommand` fallback in insecure contexts.

### Conversations

#### Changed

- Conversation selection is now a mode: default rows hide checkboxes, the header shows total plus Select, selection mode offers checks/select-all/bulk delete, Done clears and exits. Successful bulk deletion exits automatically; failures retain selection for retry. Individual rename/pin/delete remain in row menus.

### UI

#### Changed

- Improved task/conversation/traffic interactions (#57): persisted task-column sorting, direct rename icons instead of menus, and refined sheets and traffic viewing.

#### Fixed

- Worker asset labels show only domains/IPs and handle empty labels correctly.

### Agent

#### Added

- Added quantitative goal checks before `prove_goal`: compare measured `graph_overview` values (`coverage.pct`, finding counts, etc.) against coverage/flag/privilege requirements. If unmet, dispatch intents to close gaps instead of marking “mostly done” as met. Fixed a 100%-coverage goal accepted at 40%.

#### Changed

- Rewrote zero-intent planning guidance. Previously calling zero intents the most common/important rule encouraged early stopping with unmet goals and untested scope. Zero is now appropriate only when all directions are covered by open/running/recent_done, or the next step awaits current work output and a later graph update. Uncovered independent directions or unmet goals with untested scope must not stop merely because zero intents are common.
- Strengthened evidence requirements for negative Worker conclusions such as no injection, closed port or no login. Exhaust reasonable methods within the intent (encoding/parameters/path/method) first; incomplete methods or weak evidence use `confidence=inferred`, avoiding premature observed negatives that block whole directions, especially early in a task.
- Applied planner/Worker prompt changes through guarded one-time `reseedPlannerPrompt` / `reseedWorkerPrompt` migrations, appending and activating new versions while retaining old/custom history for restoration.

### Operations

#### Added

- Added `reset-password.sh` for the fixed `ARTEX` administrator in local/docker deployments. Connection options are explicit or read from `--dsn`, `$ARTEX_PG_DSN`, config.json. pgcrypto generates compatible bcrypt in the database and writes `settings.auth.password_hash` without restart. Passwords use environment variables rather than argv, with injection-safe quoting.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

## [0.3.4] - 2026-08-24

### UI

#### Added

- Task selections can bulk-change category, including Uncategorized to remove current grouping. One transaction writes the batch; only tasks deleted since selection can fail.

#### Changed

- New-task category selection is searchable. Typing filters existing names; Enter/Create adds missing names immediately. The selected category is a removable tag; only one is allowed.

### Conversations

#### Fixed

- Fixed chat rejection when a conversation has an LLM profile but no global active profile. Preflight now matches execution resolution: conversation/agent binding first, global fallback.
- Configuration errors now distinguish no profiles (“add one”) from inactive profiles (“activate or assign one to this conversation”) rather than always saying unconfigured.

### Agent

#### Added

- When all goals are met, MainAgent `add_intent` work can run directly in Workers without Planner re-proving goals and cancelling it. Once the frontier drains, the task returns to done. Before dispatch, MainAgent asks whether to register the work as a formal goal; doing so resumes ordinary autonomous planning.

#### Changed

- Trace requests with more than five `step_ids` now return the first five rather than error, with `returned_step_ids`, `omitted_step_ids`, and `notice` indicating omissions and that further reads are unnecessary if sufficient. Duplicate/invalid IDs are removed before counting.

#### Fixed

- Task lists sort by descending ID instead of timestamps, avoiding equal-time tasks swapping during ten-second polling and random map iteration.

### Assets

#### Fixed

- Company-ownership recalculation uses safe `try_inet()` instead of `a.ip::inet`. A hostname in `assets.ip` previously raised `22P02`, rolling back company scope edits/deletion; invalid rows now skip safely.
- Invalid IPs are explicitly reported after company-scope saves with counts, IDs and values, explaining exclusion from IP/CIDR ownership. Server logs cover paths without UI responses, including company deletion, ScopeSentry sync and agent writes.
- Task IP/CIDR scope matching also uses `try_inet()`, replacing a character-only regex that let hexadecimal-letter hostnames such as `abc.def` reach the failing cast.

#### Changed

- `ip`, `service` and `endpoint` assets reject hostnames in `ip`. `insert_assets` and asset APIs return indexed per-item errors recommending `type=subdomain` with `domain`, or resolving A/AAAA first. Agents can correct/reinsert; other batch assets still save.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.3] - 2026-08-23

### Worker

#### Added

- Running Workers support individual pause/resume/cancel. Pause retains intents/facts/findings; cancel transactionally removes the intent and direct outputs after execution stops.
- Added paused intent state, execution fences and named termination reasons to prevent late blackboard writes after pause/cancel/task deletion.
- Added optional task concurrency limits; creation, recovery, finding deepening and queue refill share persistent FIFO admission.

#### Changed

- Increased default Worker wall-clock run time from 600 to 1200 seconds; revived timeout tasks start a new clock at their next actual execution.
- Real Worker/planner/MainAgent execution now drives task badges, fixing idle labels while Workers run.
- Worker conversations keep individual controls and current-session token totals; model names move beside the current title.

#### Removed

- Removed Worker multiselect/select-all/bulk pause/resume UI and its bulk-control API/mock contracts.
- Removed Worker row token badges/tooltips while retaining the complete ledger and task aggregation APIs.

### LLM

#### Added

- Added ordered task LLM chains with automatic next-profile switching on explicit provider quota exhaustion, persisting current profile, exhausted state and error summary.
- Running/paused tasks can edit/reorder the full chain and manually switch profiles, effective on the next LLM call.
- Automatic/manual switches and full exhaustion produce structured system activity and deduplicated task-stream notices.
- Added task-role model resolution for the next MainAgent/planner/Worker call's profile and model.
- Added optional global LLM Pool with call order, designated fallback, health, cooldown recovery and manual reset.
- Added an always-on usage ledger aggregating input/output/cache tokens by task, conversation, model and profile.

#### Changed

- Goal decomposition, planner, Worker and MainAgent share task LLM runtime; resolution order is described below.
- Failover responds only to explicit quota/balance/billing errors, not ordinary throttling, auth, network, server or context errors.
- LLM settings use cards and a right drawer with Pool order, priority, exclusions, health and recovery controls.
- Chain-exhaustion messages wrap on narrow screens; current model is a title-adjacent text label with full tooltip instead of a list icon.
- Role resolution is Agent binding → task chain → global: explicit role bindings always win, unbound roles use the task chain, and empty chains fall back globally.
- Empty egress proxy means direct connection, no HTTP_PROXY/HTTPS_PROXY fallback; explicit ARTEX_LLM_PROXY is unchanged. Authenticated `socks5://user:pass@host:port` is supported.

#### Fixed

- Transient stream failures before any output delivery safely back off/retry on the same provider, reducing short runs ending model_error without summaries. Quota exhaustion, excessive context and deterministic 4xx errors do not retry here and remain assigned to failover/compaction handling.

#### Removed

- Removed LLM icons from conversation lists to avoid confusion with Worker state icons.

### UI

#### Added

- Tasks support global category creation/rename/delete/filter, with direct selection during creation.
- Added task-template CRUD and a right management drawer; new tasks load preset descriptions/goals or save current content as a template.
- New tasks can link multiple source tasks/company scopes. One multiline field recognizes domains, URLs, IPs, CIDRs, ICP and company keywords.
- Task test assets support live add/delete with manual/company/inherited/agent source records; Worker titles show current assets and source summaries.
- Findings group by task with independent pagination; descriptions can create high-priority deepening intents.
- Conversations support rename/pin/unpin/delete; task lists support current-page bulk pause/resume.
- Traffic/tool details use right drawers. HTTP messages include Host and highlight request lines, status, headers, JSON and markup bodies.
- Task actions add Details and Pause/resume. System settings configure send keys (Enter/Cmd+Enter etc.); web search adds a proxy field.
- Conversation badges show profile names, with model IDs in tooltips. Tasks gain optional names, a name column and search, falling back to descriptions when empty.
- Skills show use counts, last use and missing dependencies to identify inactive/uninstalled skills.

#### Changed

- Task titles are focusable detail links. Task/company assets and finding groups/items use true server pagination, stable order and exact totals.
- Company creation uses a right drawer and multiline scope input with live recognition, validation and type preview.
- MainAgent input auto-grows, sends with Enter, inserts lines with Shift+Enter and avoids sending during Chinese IME composition.
- Agent previews, task reports and details share Markdown rendering; standardized deletion confirmation, long errors and mobile drawer widths.
- Template selectors display/search names but submit IDs. Restored original font sizing and unified displayed version 0.3.3.
- Task/current-session token totals highlight only input, cache-read and output values; send uses a simpler up-arrow icon.
- Task Overview scope displays company names rather than IDs.

#### Fixed

- Ordinary text containing dots is no longer mistaken for an ICP registration number.
- Fixed duplicate editor keys when variable catalogs collide with global runtime names such as `{{.Now}}`.

#### Removed

- Removed separate task-card Enter buttons; titles open details.
- Removed dashboard New task, company Logo URL input, and finding-count badges from headers/groups.
- Reverted the 10% global font increase and removed Worker checkboxes, model icons and row token totals from conversation lists.

### Agent

#### Added

- New tasks can link multiple existing tasks and inherit direct-source goals, facts, findings, completed intents, scope and blackboard context live and read only.
- Blackboard tools query source nodes/facts/findings/traces on demand; inherited nodes show provenance and all write tools reject changes.
- Company scope tools accept domains, URLs, IPs, CIDRs, ICP and company keywords; keywords guide agents but do not assign ownership automatically.
- Finding deepening creates a high-priority manual Worker intent in the original task, anchored to assets with a `derived_from` edge.
- Added goal management in Overview with view/add/edit/delete. Add/edit notifies Planner and revives tasks; deletion hard-removes goal nodes with edges/anchors but does not revive.
- MainAgent defaults to `steer_work` for live Worker corrections without interruption or lost progress, validating intent ownership.

#### Changed

- Source intents do not enter the new frontier; new tasks retain separate explorations, queues, work directories and histories.
- Deleting running intents now stops them as `stopped` without destroying data, requires a reason stored as a linked fact and payload, and notifies Planner via cancelled with intent/reason retained.
- MainAgent chat uses server activity streams, restores input after send failure, and scrolls to the last reply after opening/lazy loading.
- Task pause terminates current MainAgent/planner/Worker calls but allows new user-initiated MainAgent orchestration while paused.
- Cancel/shutdown/stream interruption retain actual reasons, generated content, rounds, duration, tokens and outstanding tool calls.

#### Removed

- Removed optimistic MainAgent client echoes to prevent duplicate/cross-session messages during pause, failure or concurrent activity.

### Constraints

#### Added

- Goal decomposition extracts allow/deny constraints before goals; MainAgent can add them later. They enter planner/Worker system prompts at highest priority, and diverse/broader exploration explicitly remains subject to them.
- Added constraint-management UI/APIs and separate planner/Worker injection toggles, both on by default and read each turn. `task_constraints` uses idempotent creation for existing databases.

### Interception

#### Added

- Added model fallback review when no regex/string command rule matches, returning ALLOW/ASK/DENY with configurable failure/timeout actions. Interception has Rules/Model tabs and model-prefixed decisions with reasons.

#### Fixed

- Hardened verdict parsing so correct DENY decisions are not misread as allow.

### Traffic

#### Changed

- Traffic exchanges now live in SQLite, with large bodies in hash-deduplicated blobs and trigram text indexes for arbitrary substring/Chinese search. Deletion becomes a single SQL transaction, dropping from hours to milliseconds without stopping capture. Added streaming large-body downloads and `traffic_search` full-text parameters. Legacy file trees remain readable/searchable/deletable without migration.

### Tasks and assets

#### Added

- Task creation adds asset coverage, on by default. Off disables calculation/display, shows only assets in the overview graph, stops automatic scope accumulation and removes related planner/MainAgent tools; company links remain unaffected.
- Added per-asset `insert_assets.related`, default true and effective only with coverage. False stores shared assets without adding task coverage, for incidental/unrelated discoveries.
- Company/task scopes share domain/URL/IP/CIDR/ICP/keyword recognition. Domains/IPs create or reuse global assets; CIDR/ICP/keywords remain scope context.

### Build

- `build.sh --release` builds Linux/macOS amd64/arm64 and Windows amd64 together, packaging skills, example config and README in ZIPs.
- Go linker stripping and ZIP compression are standard; UPX is explicitly optional because self-extracting ELF can segfault on some Linux systems.
- Release workflow smoke-tests Linux amd64 startup and includes SHA256SUMS.

### Contributors

- [@neouks](https://github.com/neouks)

## [0.3.2] - 2026-08-20

### Added

- New tasks link multiple direct sources and inherit facts/findings/completed intents/scope/blackboard live and read only, retaining independent queues, directories and histories.
- Ordered task LLM chains fail over on explicit provider quota exhaustion and persist profile, exhausted state and structured audit activity.
- Running/paused tasks can edit/reorder chains or switch profiles manually; automatic/manual/exhaustion notices appear in details.
- Workers support pause/resume/cancel. Pause retains blackboard data; cancel waits for writes to stop then transactionally removes the intent and direct facts/findings/traces.
- Task deletion can also remove linked assets, traffic, findings and files, with deletion barriers, concurrency protection and auditable counts.
- Optional task concurrency limits queue new tasks FIFO and start them when slots free.
- Added task/company asset pagination, category counts and paginated intent mock contracts.
- Added build.sh for single binaries with static frontend export, embedding, cross-platform targets and version injection.

### Changed

- Explicit task chains coexist with global Pool, retaining strict quota failover; without a chain, existing agent bindings/global rules apply.
- MainAgent input auto-grows with Enter send/Shift+Enter newline and Chinese IME composition protection.
- All task roles share LLM runtime; chains use the smallest candidate context window for safe compaction.
- Details show running/idle/paused from actual LLM calls; inherited facts/findings/intents/assets/graph nodes are source-labeled and read only.
- Agent previews, reports and details use shared Markdown rendering.
- LLM profiles use cards/drawers, with independent model-list scrolling inside the drawer.
- Paused tasks allow new MainAgent messages; pause only ends the current turn and does not block independent orchestration conversations.
- Increased default Worker run wall-clock time from 600 to 1200 seconds.

### Fixed

- Replaced optimistic MainAgent console echoes with server-only rendering, fixing mixed/cross-session messages on pause/send failure.
- MainAgent conversations open at the bottom and stay there after the last reply's full lazy-loaded content expands.
- Fixed MainAgent continuing after task pause and orchestration pause failing to stop it.
- Fixed task badges/actions out of sync at completion or during actual planner/Worker/MainAgent execution.
- Fixed late blackboard writes/residual files racing Worker cancellation, task deletion and concurrent writes.
- Fixed Markdown previews, long deletion text overflow, mobile widths and task-detail button alignment.
- Named every agent cancellation reason; activity details show initiator, terminal state, rounds, duration, usage and outstanding tools.
- Fixed parent context racing to overwrite named shutdown reasons and byte-truncated Chinese activity summaries becoming corrupt.
- Cancelled partial streams retain their true termination reason and already generated content.

### Contributors

- [@Autumn-27](https://github.com/Autumn-27)
- [@neouks](https://github.com/neouks)

[Unreleased]: https://github.com/Autumn-27/ARTEX/compare/v0.3.10...HEAD
[0.3.10]: https://github.com/Autumn-27/ARTEX/compare/v0.3.9...v0.3.10
[0.3.9]: https://github.com/Autumn-27/ARTEX/compare/v0.3.8...v0.3.9
[0.3.8]: https://github.com/Autumn-27/ARTEX/compare/v0.3.7...v0.3.8
[0.3.7]: https://github.com/Autumn-27/ARTEX/compare/v0.3.6...v0.3.7
[0.3.6]: https://github.com/Autumn-27/ARTEX/compare/v0.3.5...v0.3.6
[0.3.5]: https://github.com/Autumn-27/ARTEX/compare/v0.3.4...v0.3.5
[0.3.4]: https://github.com/Autumn-27/ARTEX/compare/v0.3.3...v0.3.4
[0.3.3]: https://github.com/Autumn-27/ARTEX/compare/v0.3.2...v0.3.3
[0.3.2]: https://github.com/Autumn-27/ARTEX/compare/v0.3.1...v0.3.2
