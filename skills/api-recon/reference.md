# api-recon reference


Grep recipes, `config.json` templates and troubleshooting. Run grep against `js/`. For one-line bundles, optionally use `js-beautify` or `sed 's/}/}\n/g'`; raw grep with context windows is usually enough.

## Script notes

All files in `scripts/` are reference templates. Adapt them to the target before running. Typical changes:

| Script | Typical adaptations |
|---|---|
| `harvest_static.py` | Endpoint regex, webpack/Vite manifests, microfrontend publicPath, retries/concurrency |
| `runtime_harvest.js` | neutralize field names/success values, stub matching/body shape, route sources, WS capture, `waitUntil`/`routeTimeout`/`proxy` |
| `preload.js` | `loginPathRe`, L1 stubs, `neutralize.fields`, `apiPattern`, whether to enable L3, `recordDetail`, `observe.*`, `neutralizeVueRouter` |
| `spider_mpa.py` | `--exclude` destructive links, cookies, depth/max, same-origin filters |
| `extract_route_map.py` | `routeMap` / `routeLink` regex, KEY naming patterns |
| `build_perm_tree.py` | `userRouteAuth` parsing, `ROOTS`/`PREFIX_PARENT` hierarchy heuristics, outer stub field names |
| `config.json` | Central configuration for all site-specific parameters above |

Keep adapted files in the task directory, e.g. `recon/`, and report exact changes from the templates.

---

## A. Reconstruct the three gates

### A1. Rendering: how is login state determined?

```bash
grep -rhoaE '.{0,40}(isLogin|isAuthenticated|loggedIn|hasLogin|requireAuth)\b.{0,80}' js | head
grep -rhoaE 'function (getUser|getToken|getAuth)[0-9]?\([^)]*\)\{.{0,200}' js | head
grep -rhoaE '(localStorage|sessionStorage)\.getItem\("[^"]+"\)' js | sort -u
grep -rhoaE '(Cookies?|cookie)\.(get|load)\("[^"]+"\)' js | sort -u
grep -rhoaE '\batob\(|JSON\.parse\(|jwt|decode' js | head
```

Trace `isLogin = f(getUser())` → `getUser = decode(storage.read(KEY))` to identify storage key, container (Cookie/localStorage), and encoding:

| Encoding | Mock value in config |
|---|---|
| Plain string / `"1"` / token | `"value": "anything-truthy"` |
| `JSON.parse(x)` | `"value": "json:{\"id\":1,\"username\":\"admin\"}"` |
| `JSON.parse(atob(x))` | `"value": "b64json:{\"id\":1,\"username\":\"admin\"}"` |
| JWT | Unsigned/`alg:none` JWT, or signing with a key present in the bundle, only for the client-side mock guard |
| Encryption (SM2/AES/RSA) | Find hardcoded keys; mock only if the rendering guard merely needs a decodable blob, otherwise fall back to static analysis |

Write to `cookies` / `localStorage`.

### A2. Interceptor: what triggers /login redirects?

```bash
grep -rhoaE '.{0,60}(interceptors\.response|axios|request\.use).{0,120}' js | head
grep -rhoaE '.{0,40}(response_code|errcode|errno|\bcode\b|\bret\b|\bstatus\b)\s*[=!]==?\s*[\-0-9]{1,4}.{0,60}' js | head -20
grep -rhoaE '.{0,40}(未登录|请重新登录|登录已过期|unauthorized|登录失效|授权|token.{0,10}invalid).{0,40}' js | head
grep -rhoaE '.{0,30}(location\.href|router\.(push|replace)|navigate)\([^)]*login[^)]*\)' js | head
```

Identify field names, success values (usually `0` or `200`) and failure values triggering redirects. Verify with a junk session:

```bash
curl -sk -X POST -H 'Cookie: <fakekey>=junk' https://target/api/<protected> -d '{}' -H 'Content-Type: application/json'
```

Write to `neutralize.fields` + `neutralize.success`.

### A3. Content: where do menus and permissions come from?

```bash
grep -rhoaE '"/api[^"]*(permission|perm|role|menu|acl|resource|nav)[^"]*"' js | sort -u
grep -rhoaE '.{0,30}(menus|permissions|menuList|routeList|authList|role_permissions)\b.{0,120}' js | head
grep -rhoaE 'userRouteAuth|getResultTree|routeMap|routeLink|hasPermission|checkAuth' js | head
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head
```

Two data layers common in business admin interfaces:

| API | Typical payload | Consumer |
|---|---|---|
| `.../role_permissions` | `{ permissions: string[], role_type }` | Route guards, button-level ACL |
| `.../permissions/all` | `tree[{ code, position, children }]` | Sidebar menu rendering |
| In bundle: `userRouteAuth` | `{ CODE: { url, name? } }` | code → frontend path |
| In bundle: `routeMap` | `{ KEY: { name, link } }` | Alias resolution (webpack `o.DASHBOARD`) |

Read consumer code to determine how `getResultTree(tree, permissions)` filters and which fields `v-if` / `hasAuth(code)` checks.

For small sites, manually construct a permissive mock payload in `stubs`.

For large sites with blank sidebars/submodules, reconstruct the complete permission tree in section I.

---

## B. config.json template

```json
{
  "baseUrl": "https://target/",
  "runtimeMode": "both",
  "chromium": "/usr/bin/chromium",

  "cookies": [
    { "name": "auth", "value": "b64json:{\"id\":1,\"username\":\"admin\",\"role\":\"admin\",\"func\":{},\"permissions\":[\"*\"]}" }
  ],
  "localStorage": { "token": "faketoken", "isLogin": "1" },

  "neutralize": {
    "fields": ["response_code", "code", "errno", "ret", "status"],
    "success": 0,
    "flags": { "success": true, "message": "ok" }
  },
  "forward": true,
  "loginUrlPattern": "/login",
  "apiPattern": "/api/|/rest/|/graphql",

  "mockTier": "L1+L2",
  "recordDetail": true,
  "observe": {
    "storageReads": false,
    "cookieReads": false,
    "xhrHeaders": true
  },
  "neutralizeVueRouter": true,
  "stubs": [
    {
      "match": "permissions/all|/menu|role_permissions",
      "body": {
        "response_code": 0, "code": 0,
        "data": {
          "permissions": ["*"],
          "menus": [
            { "name": "dashboard", "path": "/dashboard", "show": true, "children": [] },
            { "name": "alert", "path": "/alert", "show": true, "children": [] }
          ]
        }
      }
    }
  ],

  "explore": {
    "clickTabs": true,
    "clickTables": true,
    "pushStateFallback": true,
    "maxMenuItems": 50
  },

  "routes": ["/dashboard", "/alert", "/asset", "/device", "/report", "/config", "/system"],
  "waitMs": 1500, "perRouteMs": 900, "headless": true,
  "waitUntil": "domcontentloaded",
  "routeTimeout": 12000,
  "proxy": "",

  "captureResponses": true, "recordWs": true, "respMax": 600
}
```

Fields:
- `runtimeMode`: `depth` (Puppeteer), `coverage` (browser MCP), `both`
- `cookies[].value` prefixes: `b64json:` → base64(JSON); `json:` → raw JSON; none → literal
- `forward: true` forwards real requests and rewrites status-code fields; `false` uses offline stubs
- `mockTier`: coverage preload layers, e.g. `L1+L2`, `L1+L2+L3`
- `routes` comes from `routes.txt`; after mock menus, the harness appends `<a href>` routes
- `captureResponses` / `recordWs` apply only to depth
- `waitUntil`: use `domcontentloaded` for large SPAs to avoid `networkidle2` hangs
- `routeTimeout`: per-route `page.goto` timeout in milliseconds
- `proxy`: Puppeteer `--proxy-server`; alternatively `HTTP_PROXY` / `HTTPS_PROXY`

### B1. Two-stub template (role_permissions + permissions/all)

```json
"stubs": [
  {
    "match": "role_permissions",
    "body": {
      "response_code": 0,
      "data": {
        "permissions": ["MONITOR", "MONITOR_ALERT", "THREAT", "ASSETS_RISK"],
        "role_type": "SUPER_ADMIN"
      }
    }
  },
  {
    "match": "permissions/all",
    "body": {
      "response_code": 0,
      "data": [
        {
          "code": "MONITOR",
          "position": 1,
          "children": [
            { "code": "MONITOR_ALERT", "position": 1, "children": [] }
          ]
        }
      ]
    }
  }
]
```

Outer fields (`response_code` / `code` / `data`) must match gate A2; `permissions` must cover every leaf code in the tree.

---

## C. Coverage preload configuration

Edit `CONFIG` at the top of your copied `preload.js`, or replace it before CDP injection:

```javascript
const CONFIG = {
  loginPathRe: /\/(login|signin)(\/|$|\?)/i,
  mockTier: 'L1+L2',
  forward: true,
  recordDetail: true,
  extractUrlsFromResponse: true,
  neutralizeVueRouter: true,
  observe: { storageReads: false, cookieReads: false, xhrHeaders: true },
  neutralize: { fields: ['response_code', 'code'], success: 0 },
  stubs: [ /* Same as config.json stubs */ ],
  apiPattern: /\/(api|apis|v\d+|dev|internal|graphql)\//i,
};
```

Verify `window.__API_RECON_PRELOAD__ === true` and a stable pathname.

Export capture results:

```javascript
JSON.stringify({
  apis: [...window.__API_RECON_LOG__],
  detail: window.__API_RECON_DETAIL__,
  routes: [...(window.__API_RECON_ROUTES__ || [])],
  observe: window.__API_RECON_OBSERVE__,
}, null, 2)
```

---

## D. Preload/runtime hook coverage

Built-in browser hooks in preload (coverage) and runtime_harvest (depth):

| Hook | Discovery value | Coverage |
|---|---|---|
| Hook fetch / XHR.open | Capture URL/method | ✅ `recordDetail` + `__API_RECON_LOG__` |
| Hook XHR.setRequestHeader | Discover Authorization and other headers | ✅ `observe.xhrHeaders` |
| Hook localStorage/cookie reads | Identify session keys | Optional `observe.storageReads/cookieReads` |
| Vue route collection | Complete frontendRoutes | ✅ `__API_RECON_ROUTES__`(loaded routes) |
| Neutralize Vue guards / block login redirects | Render modules to trigger APIs | ✅ `neutralizeVueRouter` + native redirect neutralization |
| React route collection | Complete routes | Static + clicks; no dedicated hook |
| Block page redirects (login path) | Stay on page for analysis | Only block login paths, preserving business navigation |
| Hook encryption libraries (CryptoJS/SM etc.) | Encrypted parameters → plaintext API body | Manual encryption-input hook required; record findings in config |
| Anti-debugging bypass | Otherwise runtime cannot capture APIs | Manual handling required; static remains available |

---

## E. Endpoint regex when static results are sparse

Broaden `extract_endpoints` in `harvest_static.py`, or inspect manually after the required harvest phase:

```bash
grep -rhoaE '"/[a-z][A-Za-z0-9_/\-]{3,}"' js | sort -u
grep -rhoaE '/api/[a-zA-Z0-9_./-]+' js | sort -u
```

---

## F. Troubleshooting

| Symptom | Cause and action |
|---|---|
| Few static APIs | Endpoint syntax mismatch: broaden regex (section E) |
| Chunk count much smaller than manifest | CSS-only or undeployed chunks; retry 404s |
| Runtime still shows login | Rendering guard: recheck A1 key, container, encoding, domain |
| Shell renders, modules blank | Content gate: mock menus (A3); check `routes` paths |
| Only bootstrap/locale per route | Missing permission codes: reconstruct tree (I), check both `role_permissions` and `permissions/all` stubs |
| Sidebar present, subpages blank | Missing intermediate tree nodes or codes mismatch `userRouteAuth` |
| Every API redirects to login | Interceptor: check `neutralize`; extend traversal for nested fields |
| Zero WS frames | Subscription needs interaction; increase `perRouteMs` |
| Empty response body | Real responses require `forward: true` |
| Chromium missing | Install Chromium or set `config.chromium` / `CHROMIUM` |
| Mocks still return to login | Late hook or missing `location.href` setter: document-start preload |
| All lists empty | Empty L3 arrays are normal; continue tabs/settings/details |
| Redux actions mistaken for routes | Filter internal paths containing get/set/change/clear/toggle/upload |
| Vue still redirects to login | Inject preload at document start; if `neutralizeVueRouter: false`, clear client guards manually |
| Response URLs missing from log | Enable `extractUrlsFromResponse` or extract from `__API_RECON_DETAIL__` |
| Unknown authorization-header name | Enable `observe.xhrHeaders` or inspect DevTools request headers |
| Runtime slow/timeouts | Use `waitUntil: domcontentloaded`, lower `routeTimeout`, avoid `networkidle2` |
| Proxy connection failure | Check `proxy`/environment; align Puppeteer and curl proxy ports |

---

## G. Hardened targets

When the server validates sessions throughout the flow (signed cookies that cannot be mocked, server-rendered menus that cannot be stubbed), runtime stops at the shell. Expected behavior:

- Static analysis still enumerates endpoints because module paths remain in code
- The upstream reference also describes using an explicitly authorized real session with the same harness (`forward: true`, no neutralize) to capture actual methods/parameters/responses. This is outside this skill's credential-free default and must not override its prohibition on requesting credentials or performing real login.

---

## H. Per-task checklist

1. Confirm authorized scope
2. Read `scripts/harvest_static.py`, adapt, run, inspect `api_static.txt` and `routes.txt`
3. Phase 1b: path-anchor windows + binding layer → `param_candidates.json` (J)
4. Reconstruct A1/A2/A3 → site-specific `config.json`
5. Read and adapt `runtime_harvest.js` / `preload.js` before execution
6. `runtimeMode=depth`: `npm install` → Run adapted harvest script
7. `runtimeMode=coverage/both`: Inject adapted preload at document start → browser MCP enumeration + parameter trigger matrix
8. Modules not rendering → permission-tree reconstruction (I) → patch stubs → rerun
9. Multiple parameter sample diffs + error inference → `params_merged.json`
10. Merge → `site_map.json` + `api_merged.txt`; accurately state coverage, gaps and script changes

---

## I. Permission-tree reconstruction (Phase 4 detail)

Use when a simple `menus: [{ path, show: true }]` mock does not mount submodules.

### I1. Locate auth modules

```bash
grep -l 'userRouteAuth' js/*.js
grep -l 'routeMap\|routeLink' js/*.js
grep -rhoaE 'getResultTree|role_permissions|permissions/all' js | head
```

Record permission API paths, response field names and consuming chunk filenames.

### I2. Extract routeMap

```bash
python3 scripts/extract_route_map.py recon/js recon/
# Output recon/route_map.json
```

For `[!] no routeMap pattern found`, broaden `extract_route_map.py` regex or inspect with grep:

```bash
grep -rhoaE '([A-Z_][A-Z0-9_]*):\{name:"[^"]*",link:"/[^"]+"\}' js | head -20
```

### I3. Build permission tree and stubs

```bash
python3 scripts/build_perm_tree.py recon/js recon/ --config recon/config.json
```

Script logic:
1. Parse `userRouteAuth={MONITOR:{url:...},...}`, including webpack aliases such as `He=o.DASHBOARD`
2. Resolve aliases to real paths through `route_map.json`
3. Infer parent from code prefix (`MONITOR_ALERT` → `MONITOR`)
4. Output `permissions_tree.json`, `permissions_all_stub.json`, `role_permissions_stub.json`
5. With `--config`, update `stubs` and extend `routes` in `config.json`

Adapt at the top of the copied script:
- `DEFAULT_ROOTS`: Top-level module codes
- `DEFAULT_PREFIX_PARENT`: `PREFIX_` → parent mapping
- `DEFAULT_EXTRA_PARENT`: Orphan nodes without prefix relationships

### I4. Verify stub consistency

```bash
# Permission count should approximate userRouteAuth entries
wc -l recon/perm_codes_all.txt
# routes should cover all route_map links
python3 -c "import json; m=json.load(open('recon/route_map.json')); r=set(json.load(open('recon/config.json'))['routes']); print('missing', [v['link'] for v in m.values() if v['link'] not in r])"
```

### I5. Rerun runtime and compare

```bash
node recon/runtime_harvest.js recon/config.json
# Compare runtime_api.json counts before/after mocks; check module APIs under /attack, /asset, etc.
```

| Before mock | Successful mock |
|---|---|
| Same 3–5 bootstrap requests per route | Different routes trigger different module APIs |
| Only `/api/locale/language` | Module endpoints such as `/api/web/...` |
| Single-digit routes in `routes.txt` | 80–110+ `routes` from route_map |

### I6. If it still fails

- Coverage: click sidebar and tabs; permission-related requests may need interaction
- Stub fields: compare nesting against an already authorized API response. The upstream example used curl with a real session; do not acquire credentials or log in under this credential-free skill.
- Extra guards: grep button checks such as `hasPermission|checkRole|func.`, extend `role_permissions.permissions`
- Static fallback: module paths remain in `api_static.txt`; runtime adds methods/bodies. Keep `param_candidates.json` and captured samples

---

## J. Parameter reconstruction (Phase 1b / 5b / 5c)

A method, not a universal script. Use regex for paths; anchor windows, UI bindings, sample diffs and error inference for parameters.

### J1. Anchor windows: path to payload builder

```bash
# Anchor on known Phase 1 paths
grep -n '"/api/user/list"' js/*.js
grep -rhoaE '.{0,120}("/api[^"]+").{0,200}' js | head
grep -rhoaE '(params|data|body|payload)\s*:\s*\{' js | head
grep -rhoaE '(get|post|put|delete|patch)\([^,]+,\s*\{' js | head
```

### J2. Wrappers and transport forms

```bash
# axios / shared request wrapper
grep -rhoaE '(axios|request)\.(get|post|put|delete|patch)\(' js | head
grep -rhoaE 'interceptors\.(request|response)' js | head

# GraphQL
grep -rhoaE '(query|mutation)\s+\w+|gql`|graphql\(' js | head
grep -rhoaE '\$[a-zA-Z_]+\s*:\s*(Int|String|Boolean|\[)' js | head

# FormData / multipart
grep -rhoaE 'FormData|\.append\(' js | head

# Path parameters
grep -rhoaE 'path:\s*"/[^"]*:[^"]+"' js | head
grep -rhoaE 'useParams|route\.params|\$route\.params' js | head
```

### J3. Validation: required fields, formats, enums

```bash
grep -rhoaE '(required|message|pattern|enum|validator)\s*:' js | head
grep -rhoaE 'yup\.|zod\.|async-validator|Form\.Item|a-form-item|el-form-item' js | head
grep -rhoaE 'rules\s*:\s*\[|name:\s*["\'][a-zA-Z_]+["\']' js | head
grep -rhoaE 'label.*value|options\s*:\s*\[' js | head
```

### J4. Binding layer: form to API

```bash
grep -rhoaE 'onFinish|handleSubmit|getFieldsValue|validateFields' js | head
grep -rhoaE '(pick|omit|transform|dayjs|moment)\(' js | head
```

Runtime evidence: DevTools → Network → request → Initiator (call stack); trace upward from `fetch`/`send` to payload assembly.

### J5. Encrypted parameters

```bash
grep -rhoaE 'encrypt|decrypt|sign|CryptoJS|sm2|sm3|sm4|RSA|AES' js | head
```

Never guess fields from ciphertext. Hook encryption-function inputs, record the plaintext payload before encryption, and save conclusions in `config.json` / `param_candidates.json`.

### J6. Parameter trigger matrix (required in Phase 3)

Record each operation per module and diff request bodies/queries:

| Operation | Inspect |
|---|---|
| Initial list | Pagination defaults |
| Search | keyword, filters |
| Advanced filters | Optional fields |
| Create/edit | Full entity |
| Bulk/export | `ids[]`, `exportType` |
| Sort/page | `sortField`, `order` |

Output `param_samples.json`: `[{ "path", "method", "action": "search", "body", "query", "headers" }]`

### J7. Confidence rules

| Confidence | Condition |
|---|---|
| High | Static callsite and at least two matching runtime samples |
| Medium | Static only or one runtime sample |
| Low | Inferred from response/error without second verification |
| Pending trigger | Known static field, UI/permission path not reached |

### J8. Scenario recipes

| Scenario | Order |
|---|---|
| REST list page | J1 payload object → J6 four diffs → J3 rules |
| Create/edit form | J3 form name → J4 submit chain → authorized, non-destructive runtime submission and empty-field 400 inspection |
| GraphQL | J2 variable declarations → capture variables for each runtime operation |
| Encrypted body | J5 hook inputs → pre-encryption fields are actual parameters |

### J9. Mapping to api-recon phases

| api-recon | Parameter recon |
|---|---|
| Phase 1 static | J1 anchor windows |
| Phase 2 A2 interceptor | Global injected fields (tenantId, sign) |
| Phase 3 runtime | J6 trigger matrix + `param_samples.json` |
| Phase 4 permission tree | Modules have different forms; sufficient mock permissions expose all fields |
| Phase 5 merge | `params_merged.json` + confidence; no required-field conclusions from one sample |

### J10. Troubleshooting

| Symptom | Action |
|---|---|
| Static field never appears at runtime | Mark pending trigger; complete permission tree, advanced filters, linked select options |
| Same path, different body shapes | Normal: record separately by `action`, do not force schemas together |
| Need parameters despite mock responses | Inspect outbound request bodies/headers, not inferred stub responses |
| 400 names a nested field | Check outer `data`/`bizData`/`variables` wrappers |
| Only GraphQL operation name visible | Expand `variables` JSON; find `$var: Type` statically |

---
