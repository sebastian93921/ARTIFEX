# Multiple traffic records as finding evidence

Finding details support selecting traffic across pages, assigning roles and notes, ordering records, and removing bindings. The traffic page also lets you select multiple records and attach them to an existing finding. Evidence inherited from another task is read only; edit it in the source task.

“Agent automatic traffic binding” is off by default. Read or change `agent_traffic_binding` through `/api/settings`. Enabling it adds token costs for request/response review, tool calls and instructions; the next agent turn uses the new setting. When off, automatic binding parameters and the supplemental binding tool are hidden, no automatic binding instructions are injected, and new automatic binding submissions from already-running sessions are rejected. Manual binding, capture, saved evidence reading and export remain available.

When enabled, the normal flow is: record finding, automatically start the report agent, verify and bind traffic, then write the report against the latest evidence version. The reporting agent preserves verification commands, important output, existing real traffic IDs and their purpose in `evidence`. The report agent checks the finding and execution record, verifies traffic through `traffic_search` / `traffic_get`, calls `bind_finding_traffic`, then reads the latest `version` before saving. When disabled, the report agent does not receive additional raw traffic search/read tools or bind traffic automatically; it can still read manually bound snapshots and write reports.

For compatibility, callers can still explicitly bind immediately with `report_finding.traffic_refs` / `evidence_hint_id`. Binding is optional: non-HTTP findings such as TCP issues, uncaptured traffic, or missing exact records can still be reported. Keep commands, logs and other verifiable evidence, and explain missing traffic when useful; there is no new required field. Every submitted ID must be valid with a complete body. Any failure rolls back the entire binding operation; failure of explicit binding during reporting rolls back the whole report submission. Appending an existing snapshot again neither duplicates the binding nor overwrites its note.

## Agent identifiers and report versions

`report_finding` accepts the optional parameter below. Array order sets initial evidence order.

```json
{
  "traffic_refs": [
    {"traffic_id": "real-traffic-id", "role": "baseline", "note": "Normal account request"},
    {"traffic_id": "another-real-traffic-id", "role": "proof", "note": "Reproduction request"}
  ]
}
```

Roles are `baseline` (normal comparison), `proof` (finding proof), `verification` (additional verification), and `supporting` (default supporting evidence). Verify real records with `traffic_search` / `traffic_get` first. Domain and time only narrow candidates; they do not establish task ownership.

The instructions reference [CyberStrikeAI's finding-reporting guidance](https://github.com/RuoJi6/CyberStrikeAI/blob/54d56774b8bd285817d16d48b70a4a5e6e0963f7/internal/app/vulnerability_tools.go), adapted to this project's optional binding contract. A reason for missing traffic is not mandatory. Never guess IDs or repeat probes solely to obtain a capture.

The first response line remains `finding recorded: <exploration node ID>`. Subsequent JSON supplies the separate finding record `finding_id`, exploration node `finding_node_id`, and a binding summary.

- `get_finding_traffic(finding_id)` takes the separate finding record ID and returns an ordered list and `version`. Supply `binding_id`, `side=request|response`, `offset`, and `length` to read chunks of at most 8192 bytes.
- `update_finding_report.finding_id` continues to take the exploration node ID. Set `evidence_version` to the version actually read. If evidence changes during generation, the old-version write is rejected; reread and regenerate.
- Legacy report calls without a version do not claim to cover existing traffic evidence. Binding, note, role or ordering changes mark existing reports as needing an update.

With automatic binding enabled, `add_hint` / `add_task_hint` can store `traffic_refs` in one hint or each item of a batch `hints` array. A planner reporting on another agent's behalf can use `evidence_hint_id` to select references from a hint in this task, never an inherited hint. The system does not infer bindings from domains, times or browsing history. Failed submissions neither create partial findings nor start report generation early.

Use `bind_finding_traffic(finding_id, traffic_refs)` to add missing bindings to an existing finding without registering it again. `list_findings`, `list_task_findings`, `node_detail` and `get_task_node_detail` expose both `finding_id` and `finding_node_id`; legacy `id` retains exploration-node semantics.

Startup only adds optional properties to old tool schemas. Original default traffic-tool bindings extend to the report agent, which receives the supplemental binding tool by default. Custom binding lists, prompts, descriptions and enabled states remain intact. Instructions are appended after final tool assembly: the reporting role hands over existing evidence; the report agent verifies, binds and writes. Platform chat without task context should hand over a structured hint to a task agent instead of reporting directly. Hand over existing evidence before declaring a task complete; absent captures do not force a wait. Failed `report_finding` calls do not start the report agent.

## API

Base path: `/api/exploration/findings/{finding_id}/traffic`, using the separate finding ID. Existing authentication applies; `context_task` checks task visibility and inherited read-only access.

| Method / relative path | Request / response |
| --- | --- |
| `GET` | Ordered summaries, evidence version, report evidence version |
| `POST` | Append the entire `{"traffic_refs":[...]}` batch |
| `PATCH /{binding_id}` | `{"version":1,"role":"proof","note":"Description"}` |
| `DELETE /{binding_id}` | `{"version":1}` |
| `PUT /order` | `{"version":1,"binding_ids":["2","1"]}`; must include the complete list |
| `GET /{binding_id}` | Snapshot metadata and bounded body previews |
| `GET /{binding_id}/body` | `side`, `offset`, `length`; `download=1` downloads all original bytes |

Version/order-set conflicts and writes during archiving return `409`; inherited writes return `403`; nonexistent bindings or bindings outside the finding return `404`. Traffic/attachment reading and verification failures return explicit errors.

## Storage and migration

Startup idempotently migrates PostgreSQL: `traffic_evidence_snapshots`, `finding_traffic_bindings`, and `findings.evidence_version` / `report_evidence_version` (default 0). It does not infer bindings from historical prose.

Snapshots store the original traffic ID, capture time, URL, method, status, request/response headers, body lengths and SHA-256. Bodies live at `<data>/evidence/blobs/<first-two-characters>/<hash>.bin`, independently of disposable `data/traffic`. Multiple findings can share snapshots/bodies. No snapshot-content update API exists; integrity mismatches fail reads and exports.

Complete bodies, including large blobs and legacy directory records, are read under the original traffic write lock. Files are persisted and verified first; a single PostgreSQL transaction then writes exploration nodes, intent relations, findings, snapshots and bindings. The planner is notified only after commit. Failure can leave unreferenced files but no partial business records.

PostgreSQL advisory lock `7337741004` coordinates evidence files and SQL references. Task row locks prohibit evidence changes once archiving is queued. Restore holds the evidence lock from body installation through metadata commit. Deleting a finding cascades to its bindings.

The cleaner runs hourly and only collects unreferenced content outside active operations after at least 24 hours. Normal traffic cleanup leaves the evidence directory intact. Back up PostgreSQL and `data/evidence` together for hot-data backups.

## Export and archives

Markdown includes the ordered evidence list and version; JSON includes metadata; CSV adds counts and binding IDs. `md-zip` keeps finding Markdown and includes:

```text
evidence/<finding_id>/<binding_id>/
  manifest.json
  request.http
  response.http
  request.bin
  response.bin
```

Markdown links to messages with relative paths. Before sending a download, the server copies attachments, verifies hashes, compresses and syncs the archive, and reads every ZIP entry to check CRCs. Missing or corrupt content fails the whole download. Full attachments preserve original binary bytes.

Archive v3 collects snapshots and bodies by finding bindings, independently of original traffic or domains. Hot data is cleaned only after archive verification; shared evidence remains. Restore verifies and installs bodies before transactionally restoring metadata and bindings, and supports retry after failure. v1/v2 remain restorable, with absent new fields explicitly set to 0.

## Verification and boundaries

Use a separate, freshly created PostgreSQL test database for each package through `ARTEX_PG_DSN`, so leftover task/model fixtures do not start background work. Run complete relevant packages and confirm missing configuration did not cause skips:

```sh
# Set ARTEX_PG_DSN to the package's isolated database before each run.
# Explicit configuration failures must fail the test.
go test ./<package> -count=1
go test -race -p 1 ./evidence ./db ./agent ./server -run 'TestEvidence|TestFindingTraffic|TestFindingEvidence|TestReportFindingAtomicContract|TestTaskArchive'
```

Frontend checks include `npx tsc --noEmit`, Biome on affected files, a Webpack build and a `NEXT_EXPORT=1` static export. Use separate cache directories to avoid overwriting a running development server.

Local end-to-end acceptance uses dedicated ports, controlled HTTP/domain-based HTTPS targets and temporary data directories. It covers both binding entry points, cross-page selection, order/notes, errors, inherited read-only access, downloads, and exporting after original traffic deletion, archiving, hot-body collection, restore and hash verification.

The initial implementation uses a global evidence coordination lock. Large binding/export operations or slow attachment downloads can make other evidence operations wait. Missing captures or incomplete bodies cannot be fabricated. This feature changes neither capture settings nor certificate handling for HTTPS connections made directly to an IP address.
