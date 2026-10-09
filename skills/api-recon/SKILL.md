---
name: api-recon
description: Use to inventory a website's API interfaces within authorized scope.
---

# API Recon (frontend interface discovery)


With authorization, discover backend APIs (paths, methods, parameters, response bodies), frontend routes and UI triggers (tabs, dialogs, table actions) as completely as possible.

---

## Boundaries and prohibitions (required reading)

This skill only inventories APIs and parameters. It does not perform vulnerability discovery or exploitation.

### Task boundaries

| Scope | Allowed | Prohibited |
|---|---|---|
| Objective | Enumerate paths, methods, parameters, routes and UI triggers | SQLi/XSS, access-control attacks, brute force, vulnerability fuzzing, tampered-request attacks, destructive operations |
| Authentication | Hook and stub/mock client-side login guards | Requesting or guessing credentials; real login form submissions |
| Runtime | Hook without credentials; render the post-login SPA shell using mocks | Flows requiring an actual backend session |

### Dynamic analysis without credentials (Phase 3 default)

1. Intercept and stub login, permission, menu and other bootstrap APIs using `preload.js` / `runtime_harvest.js`.
2. Return mock query bodies with the correct shape and success code; data may be empty.
3. Render post-login pages without a backend or despite 401s to trigger more XHR/fetch/WebSocket requests.
4. Empty data, blank tables and placeholder UI are expected. Do not switch to real login or vulnerability testing.

Use mocks to mount frontend routes/components and record outbound requests only. The objective is discovering what else the frontend sends, not obtaining real backend data.

### Workflow prohibitions

| Prohibited | Required alternative |
|---|---|
| grep/curl/Read of the main `index-*.js` for API paths before Phase 1 completes | Run `OUTDIR/harvest_static.py` |
| Handwritten replacements such as `extract_apis.py` | Adapt and rerun `OUTDIR/harvest_static.py` |
| Repeating the same grep/command after at least two failures | Change strategy: read tool_logs, adapt harvest, consult reference |
| Skipping gates A/B and running original `scripts/` templates | Copy to OUTDIR and adapt to the target |
| Real credentials, OTP or OAuth authentication | Use stub/mock as above |
| Skipping stubs for real data through access-control/injection tests | Record outbound only within recon scope |
| Deletion, sensitive-data export, bulk writes or other irreversible actions | Applies to coverage clicks too |
| Claiming all pages/APIs without runtime and dynamic enumeration | Meet the completion definition or state limitations |
| Claiming all parameters without trigger matrix and diff | Phase 3b matrix + Phase 5 diff |
| Inferring required/optional from one runtime sample | Compare samples or inspect validation rules/errors |

---

## Two layers and runtime modes

| Layer | Output | Limits |
|---|---|---|
| Static (JS bundle) | Endpoint paths, draft routes, candidate fields at payload assembly sites | No HTTP methods; parameters need Phase 1b; misses runtime-built URLs |
| Runtime (live session) | Methods, bodies, responses, dynamic URLs, WS/SSE; sample diffs fill parameter gaps | Pages must render to emit requests; one sample cannot establish required/optional |

| Mode | Engine | Use |
|---|---|---|
| **depth** | `runtime_harvest.js` (Puppeteer) | API inventory, methods/parameters/responses, WS/SSE, repeatable batch runs |
| **coverage** | browser + `preload.js` | Click tabs/dialogs/tables for deeper feature coverage |
| **both** | depth then coverage | Most complete; takes longest |

Parameter method (no universal script): harvest/regex for paths; anchor-window expansion, UI binding chains, sample diffs and error inference for parameters. See section J of [reference.md](reference.md) for grep recipes.

---

## Definition of completion

Claim completion only when every condition is met:

- [ ] Static: Phase 1 produces `api_static.txt`, `routes.txt`, `js/`
- [ ] Runtime: depth or coverage at minimum; coverage/both requires working hooks and a dynamic enumeration loop
- [ ] Shell: business paths stay outside `/login` (check hash routing)
- [ ] Parameters: coverage/both completes trigger matrix and `param_samples.json`; Phase 5 merges `params_merged.json`
- [ ] Depth if modules are blank: restore the permission tree in Phase 4 and rerun until module APIs appear, beyond locale/bootstrap
- [ ] Delivery: all Phase 5 outputs; `insert_assets` stores service and endpoint assets

---

## Scripts and gates

`scripts/` contains reference templates. Never run an unadapted original and treat it as final.

Read, adapt to the target, write into `OUTDIR` (e.g. `recon/`), and record `CHANGES.md`. If unsuitable, rewrite using the method and borrow only the structure.

| Gate | When | Template → OUTDIR copy | Typical changes |
|---|---|---|---|
| A (static) | After Phase 0, before the first harvest/spider run | `harvest_static.py` / `spider_mpa.py` | Default regex works on most sites; change endpoint regex, webpack/Vite `publicPath`, MPA exclude/cookie only for manifest/syntax mismatch |
| B (runtime) | After Phase 2, before depth/coverage | `runtime_harvest.js` / `preload.js` + `config.json` | Cookie/localStorage keys, neutralize success values, stubs, login regex, API prefixes, hash/history |

Mandatory SPA order: phase numbers take priority over exploratory browsing before scripting. Do not reorder:

| Step | Required | Prohibited |
|---|---|---|
| After Phase 0 | Next Bash command = `python3 OUTDIR/harvest_static.py <URL> OUTDIR` | curl/grep/Read main `index-*.js` (usually >500KB) |
| Gate A | Copy script, make necessary small edits, run immediately | Manually extracting APIs before deciding whether to harvest |
| Before Phase 1 finishes | Check outputs with `wc -l`; adapt harvest and retry on 404 | Writing extraction scripts; repeatedly grepping undownloaded URLs |
| From Phase 1b | grep only `OUTDIR/js/*.js` | Using the main bundle instead of harvest |

- Correct: copy `harvest_static.py`, optionally adjust regex, run immediately
- Wrong: curl main bundle, repeated grep, temporary extractor, harvest last
- MPA: after Phase 0, next Bash command = `python3 OUTDIR/spider_mpa.py ...`

---

## Tool and output limits

| Constraint | Rule |
|---|---|
| Large files | Never Read/grep >100KB `index-*.js` into context; batch-process with OUTDIR scripts |
| grep output | Require `\| head -20` or `-m 5`; retain path summaries, not bundle excerpts |
| Validation | Use `wc -l`, `ls \| wc -l`; do not Read entire directories |
| Regex preview | Optional, at most once, only chunks/HTML ≤50KB; harvest is the static source of truth |
| reference | Recipes/templates/troubleshooting in [reference.md](reference.md); do not repeat it inline |

---

## Execution roadmap

```
Phase 0 classification + OUTDIR
  → Gate A → Phase 1 harvest (run immediately)
  → Phase 1b parameter reconstruction
  → Phase 2 three authentication gates → config.json
  → Gate B → Phase 3 runtime + parameter matrix
  → Phase 4 permission tree if needed → rerun Phase 3
  → Phase 5 merge report + insert_assets for every discovered service/endpoint asset; omit none
```

Check in order; do not advance until the preceding step is complete.

1. [ ] **Phase 0**: Classify SPA/MPA; create `OUTDIR` → [Phase 0](#phase-0-classification)
2. [ ] **Gate A + Phase 1**: Copy scripts, harvest immediately, verify with `wc -l` → [Phase 1](#phase-1-static-discovery)
3. [ ] **Phase 1b**: Anchor windows + binding layer → `param_candidates.json` → [Phase 1b](#phase-1b-parameter-reconstruction)
4. [ ] **Phase 2**: three authentication gates → `config.json` → [Phase 2](#phase-2-three-authentication-gates)
5. [ ] **Gate B**: Adapt runtime scripts → [Phase 3](#phase-3-runtime)
6. [ ] **Phase 3**: depth / coverage / both；verify shell entry; parameter trigger matrix → `param_samples.json`
7. [ ] **Phase 4**if needed: permission tree → patch stubs → rerun Phase 3 → [Phase 4](#phase-4-permission-tree-reconstruction)
8. [ ] **Phase 5**: Merge outputs + report + `insert_assets` → [Phase 5](#phase-5-merge-and-report)

---

## Phase 0: classification

Fetch entry HTML and create `OUTDIR`; do not modify the skill's `scripts/`:

- **SPA**: Empty shell + `<div id=app>` + chunks → Phase 1–5
- **MPA**: SSR + `<form>`, no endpoint bundle; after Gate A:

```bash
python3 recon/spider_mpa.py <BASE_URL> <OUTDIR> [--cookie "session=..."] [--max 300] [--depth 5] [--exclude "logout|delete|destroy"]
```

Outputs: `forms.txt`, `links.txt`, `api_inline.txt`. For SPA with approximately zero forms, switch to Phase 1.

---

## Phase 1: static discovery

Follow [scripts and gates](#scripts-and-gates) and [tool/output limits](#tool-and-output-limits).

```bash
python3 recon/harvest_static.py <BASE_URL> <OUTDIR>
```

Harvest parses HTML scripts and webpack/Vite manifests, downloads all lazy chunks, and outputs `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt`.

```bash
wc -l OUTDIR/api_static.txt OUTDIR/routes.txt
ls OUTDIR/js | wc -l
```

- Compare chunk count with manifest; fix harvest on 404 and retry, not one-by-one curl
- Too few `api_static.txt` entries: broaden OUTDIR endpoint regex and rerun (see reference)

### Phase 1b: parameter reconstruction

Paths come from Phase 1; investigate parameter fields separately. Follow [grep output limits](#tool-and-output-limits).

Completion: for important APIs, identify field name, transport location, inferred type, required status, sample value and confidence.

#### 1b.0: transport forms

| Form | Parameter location | Static evidence |
|---|---|---|
| REST JSON | body + query | Near the path anchor `(params\|data\|body)\s*:\s*\{` |
| GraphQL | `variables` | gql templates, `$page: Int` |
| Traditional form | urlencoded | `<form>`, `FormData` |
| File upload | multipart | `FormData.append` |
| Path parameters | `/user/:id` | Route table + `useParams` / `$route.params` |
| Encryption/signing | Wrapped in `sign`/`data` | Hook encryption-function inputs (reference D) |

Output: annotate each API with `transport: query|json|form|graphql|encrypted`.

#### 1b.1: expand anchor windows

Expand around known paths to find payload assembly:

```bash
grep -n '"/api/user/list"' OUTDIR/js/*.js | head -20
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' OUTDIR/js/*.js | head -20
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' OUTDIR/js/*.js | head -20
```

| Wrapper | Parameter clues |
|---|---|
| axios instance | `data` / `params` |
| Shared request wrapper | Interceptor-injected global fields |
| OpenAPI client | Generated method signatures |
| React Query / SWR | Second hook argument |
| Vue composable | Composable arguments |

Type clues: `yup`/`zod`/rules, `Form.Item name=`, embedded Swagger.

→ `param_candidates.json`: `{ path, fields[], source: "static-callsite", confidence }`

#### 1b.2: binding layer

```
Form field → onFinish/handleSubmit → transform → API payload
```

| Binding source | Method |
|---|---|
| Form submit | Trace submit → transform → API |
| Table search | `getFieldsValue()` → `params` |
| Routes | `:id` / `?tab=` |
| Interceptor | Global `tenantId`, pagination, sign |
| Enum select | `options` → API enum values |

Trace the DevTools call stack upward from `fetch`/`XHR.send` to the payload builder.

#### 1b.3: three payload questions (distinct from Phase 2 auth gates)

| Question | Required answer |
|---|---|
| Assembly | Where payload is built and transformed |
| Validation | required, pattern, enum |
| Transport | path / query / body / multipart / headers |

At the interceptor gate (Phase 2), also inspect global injected fields (Authorization, `X-Tenant-Id`, sign).

#### 1b.4: connect to Phase 3

Candidates come from static/binding analysis. Required, optional and conditional dependencies need Phase 3 matrices/diffs and Phase 5 error inference.

---

## Phase 2: three authentication gates

grep `OUTDIR/js/` with `head`; record findings in `config.json` (reference recipes):

| Gate | Question | Keywords |
|---|---|---|
| Rendering | How is login state determined? | `isLogin`, `getToken`, Cookie/localStorage |
| Interceptor | What triggers `/login` redirects? | `response_code`, `errno`, axios interceptor |
| Content | Where do menus/permissions come from? | `menu`, `permission`, `role`, `acl`, `routes` |

Do not treat localStorage key names as credentials; verify through chunks/request chains.

Exit = Gate B: write conclusions to `config.json` and adapt `OUTDIR/runtime_harvest.js` / `preload.js`.

### Phase 2b: API observation (optional)

Use OUTDIR's `preload.js` to identify session keys, Authorization and nested API URLs:

| Configuration | Output |
|---|---|
| `recordDetail: true` | `__API_RECON_DETAIL__` |
| `observe.xhrHeaders: true` | Header observations |
| `extractUrlsFromResponse: true` | Nested APIs in responses |
| `observe.storageReads/cookieReads: true` | Update config |
| `neutralizeVueRouter: true` | `__API_RECON_ROUTES__` |

Export each coverage round:`__API_RECON_LOG__`, `__API_RECON_DETAIL__`, `__API_RECON_ROUTES__`, `__API_RECON_OBSERVE__`.

---

## Phase 3: runtime

Pass Gate B first. Follow the [boundaries](#boundaries-and-prohibitions-required-reading) and credential-free mock strategy.

Set `config.json` `"runtimeMode": "depth" | "coverage" | "both"` (template in reference).

### Hooks and stubs (shared by depth and coverage)

| Layer | Scope | Purpose |
|---|---|---|
| L1 exact | auth/permission/bootstrap stub | Pass initial render auth |
| L2 negative-result correction | All JSON responses | Not-logged-in code → success |
| L3 fallback | `/api` etc. not matched by L1 | Empty success body to render UI |

- **depth**: fake auth + `forward` business-code patching + `stubs`; traverse `routes` (hash/history); output `runtime_api.json`
- **coverage**: Inject `preload.js` at document start (CDP `addScriptToEvaluateOnNewDocument` or userscript)

Verify `window.__API_RECON_PRELOAD__` exists and business paths do not return to `/login`.

```bash
cd recon && npm install
node runtime_harvest.js config.json
```

### 3b: coverage dynamic enumeration (required)

1. Main navigation/sidebar: click each item, wait 1–3 seconds for network
2. Tabs: `role=tab`, `.ant-tabs-tab`
3. Tables: first-row view/edit/details
4. Toolbar: export, filter, create (avoid irreversible deletion; all prohibitions still apply)
5. Each module: merge APIs/routes
6. SPA: controlled `pushState` for uncovered `routes.txt` paths (forbidden for MPA)

Required parameter trigger matrix: record each operation type per module and compare multiple samples:

| Operation | Typical additional parameters |
|---|---|
| Initial list | Pagination + default filters |
| Search | keyword, filter |
| Advanced filters | More optional fields |
| Create/edit | Full entity |
| Bulk/export/sort | `ids[]`, `exportType`, `sortField` |

Outbound bodies/headers remain real under stubs; use requests as evidence. Record `scan_raw.json`, `param_samples.json`, `api_detail.json`.

- **Vue**: `neutralizeVueRouter: true` + document-start preload
- **React**: `routes.txt` + sidebar clicks + `pushState`
- **both**: 3a depth first, then 3b coverage

---

## Phase 4: permission-tree reconstruction

Trigger: blank module pages or only bootstrap (e.g. locale) on every route means the content gate is not passed.

| Symptom | Meaning |
|---|---|
| Shell entered | Rendering and interceptor gates passed |
| Missing sidebar items/blank click results | Incomplete stub shape or permission codes |
| Few identical APIs on each route | `v-if permission` failed |
| `routes.txt` much smaller than bundle routes | Complete from auth modules |

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl)[^"]*"' OUTDIR/js/*.js | sort -u | head -30
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|menuList|authList' OUTDIR/js/*.js | head -20
```

Typical chain:`role_permissions`（flat codes）+ `permissions/all`（tree）→ `getResultTree` → `userRouteAuth[CODE].url`.

```bash
python3 recon/extract_route_map.py recon/js recon/
python3 recon/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Intermediate outputs:`route_map.json`, `userRouteAuth.json`, `permissions_tree.json`, `*_stub.json`, `perm_codes_all.txt`.

Check stubs: outer `response_code` matches the interceptor gate; flat codes match the tree; `routes` covers every `route_map` link.

After updating `config.json`, rerun Phase 3. Large SPAs can tune `waitUntil`, `routeTimeout`, `perRouteMs` (reference A3/I).

---

## Phase 5: merge and report

### Outputs

| File | Phase | Content |
|---|---|---|
| `js/`, `api_static.txt`, `routes.txt`, `chunkmap.txt` | 1 | Static bundles and paths |
| `param_candidates.json` | 1b | Static parameter candidates |
| `config.json` | 2 | Three gates + runtime config |
| `runtime_api.json` | 3a | Detailed depth capture, including WS/SSE |
| `param_samples.json`, `scan_raw.json`, `api_detail.json` | 3b | Multiple samples, click logs, details |
| `route_map.json` and related files | 4 | Permission-tree intermediate files if used |
| `params_merged.json` | 5 | Merged parameter fields + confidence |
| `api_merged.txt` | 5 | `METHOD /path [params] [static\|runtime\|both]` |
| `site_map.json` | 5 | Routes, APIs, parameters, features, limitations |
| **insert_assets** | 5 | Store all service and endpoint assets |

### 5b: merge parameters

Diff `param_samples.json`; there is no universal merge script. Confidence rules are in reference J7 (high/medium/low/pending trigger).

### 5c: infer from errors

Within authorization, incomplete requests may inspect 400 errors for parameter discovery, not vulnerability testing: `field 'x' is required`, enum errors, etc. Account for `data` wrappers, `variables`, and pre-encryption `bizData`.

Report runtimeMode, static/runtime API counts, parameter confidence, uncovered modules and a `CHANGES.md` summary relative to the templates.

Suggested `site_map.json`:

```json
{
  "site": "https://example.com",
  "runtimeMode": "both",
  "appType": "vue-spa",
  "routeGuardStrategy": ["nav-neutralize", "L1-auth", "L2-patch", "forward"],
  "apisFromStatic": [],
  "apisFromRuntime": [],
  "apis": [],
  "params": [{ "method": "POST", "path": "/api/user/list", "transport": "json", "fields": [] }],
  "frontendRoutes": [],
  "routesVerifiedByClick": [],
  "featuresTriggered": [],
  "limitations": ""
}
```

More fields and grep recipes: [reference.md](reference.md).

---

## General notes

- Framework independent: same method for webpack/Vite/Angular lazy loading
- Transport: REST/JSON, GraphQL, WebSocket, SSE; gRPC-web excluded
- SSR: client fetch is capturable; RSC/Server Actions cannot be fully enumerated
- Blind spots: JSVMP, WASM, strict HMAC/mTLS → static analysis and explicit limitations
- Parameter blind spots: conditional interactions, hidden parameters, WASM assembly → pending trigger/unreachable
- Static fallback: endpoints remain discoverable when runtime is blocked

---

## Additional resources

- Grep recipes, `config.json` templates, troubleshooting, hooks, parameter reconstruction (J), site_map template: **[reference.md](reference.md)**
- Template paths: [scripts and gates](#scripts-and-gates)
