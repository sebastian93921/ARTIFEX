package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/sebastian93921/artifex/db"
	actool "github.com/Autumn-27/norma/tool"
)

// assetInterceptCandidates extracts domain/IP/URL candidates from an asset insertion.
// Classifying URL hosts lets domain/IP rules also match URL-only services/endpoints.
func assetInterceptCandidates(item assetInputItem) (domains, ips, urls []string) {
	add := func(dst *[]string, s string) {
		if s = strings.TrimSpace(s); s != "" {
			*dst = append(*dst, s)
		}
	}
	add(&domains, item.Domain)
	for _, d := range item.BoundDomains {
		add(&domains, d)
	}
	add(&ips, item.IP)
	add(&ips, item.ServiceIP)
	add(&urls, item.URL)
	if item.URL != "" {
		if u, err := url.Parse(item.URL); err == nil {
			if h := u.Hostname(); h != "" {
				if net.ParseIP(h) != nil {
					add(&ips, h)
				} else {
					add(&domains, h)
				}
			}
		}
	}
	return domains, ips, urls
}

// assetInputLabel returns a short pending-asset identifier for interception messages.
func assetInputLabel(item assetInputItem) string {
	typ := strings.TrimSpace(item.Type)
	var target string
	switch {
	case strings.TrimSpace(item.Domain) != "":
		target = strings.TrimSpace(item.Domain)
	case strings.TrimSpace(item.URL) != "":
		target = strings.TrimSpace(item.URL)
	case strings.TrimSpace(item.IP) != "":
		target = strings.TrimSpace(item.IP)
	case strings.TrimSpace(item.ServiceIP) != "":
		target = strings.TrimSpace(item.ServiceIP)
	default:
		target = locale.Text(locale.ServerDefault(), "(unknown)")
	}
	if typ != "" {
		return fmt.Sprintf("[%s] %s", typ, target)
	}
	return target
}

// =====================================================================
// Unified asset insertion tools
// =====================================================================

// SetAssetStore wires the asset store and company store onto this ToolSet
// so the insert_assets, add_company_scope, and list_assets tools are active.
func (t *ToolSet) SetAssetStore(as *db.AssetStore, cs *db.CompanyStore) {
	t.as = as
	t.cs = cs
}

// assetInputItem is one element of the insert_assets "assets" array.
type assetInputItem struct {
	Type string `json:"type"` // root_domain|ip|subdomain|app|service|endpoint

	// ---- root_domain / subdomain ----
	Domain      string   `json:"domain"`
	ICP         string   `json:"icp"`
	RecordType  string   `json:"record_type"`
	RecordValue []string `json:"record_value"`

	// ---- ip ----
	IP           string           `json:"ip"`
	BoundDomains []string         `json:"bound_domains"`
	OpenPorts    []db.PortService `json:"open_ports"`

	// ---- app ----
	AppName     string `json:"app_name"`
	BundleID    string `json:"bundle_id"`
	Category    string `json:"category"`
	Description string `json:"description"`
	AppICP      string `json:"app_icp"`
	CompanyID   *int64 `json:"company_id"` // explicit company link (app only; others auto-attribute via scope)

	// ---- service (http) ----
	URL           string           `json:"url"`
	Technologies  []string         `json:"technologies"`
	StatusCode    *int             `json:"status_code"`
	ContentLength *int64           `json:"content_length"`
	PageTitle     string           `json:"page_title"`
	FaviconMMH3   string           `json:"favicon_mmh3"`
	Auth          []map[string]any `json:"auth"`
	ServiceName   string           `json:"service_name"`
	ServiceIP     string           `json:"service_ip"` // optional enrichment IP

	// ---- service (other) ----
	Port  int    `json:"port"`
	Proto string `json:"proto"`

	// ---- endpoint ----
	Method string           `json:"method"`
	Params []map[string]any `json:"params"`
}

// insertAssets is the unified insert_assets agent tool.
func (t *ToolSet) insertAssets() actool.CoreTool {
	return writeTool(
		"insert_assets",
		locale.Text(t.language, "Register newly discovered assets in a batch; types may be mixed (see type enum).\n")+
			locale.Text(t.language, "Required fields: root_domain -> domain; ip -> IPv4/IPv6 ip, not a hostname; subdomain -> domain; app -> app_name; HTTP service -> url; non-HTTP service -> service_name + port and at least ip or domain; endpoint -> url + method. See individual field descriptions.\n")+
			locale.Text(t.language, "auth/technologies/params append and merge without overwriting existing values.\n")+
			locale.Text(t.language, "Returns {results:[{index,id,type}], errors:[{index,error}]}"),
		obj(map[string]any{
			// Do not expose task_id to the model; SetTaskID authoritatively supplies worker ownership.
			"assets": map[string]any{
				"type":        "array",
				"description": locale.Text(t.language, "Asset array, one record per element"),
				"items": obj(map[string]any{
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{"root_domain", "ip", "subdomain", "app", "service", "endpoint"},
						"description": locale.Text(t.language, "Asset type"),
					},
					// root_domain / subdomain
					"domain":      str(locale.Text(t.language, "Root domain or subdomain; required for root_domain/subdomain")),
					"icp":         str(locale.Text(t.language, "Optional ICP registration number")),
					"record_type": str(locale.Text(t.language, "Optional subdomain DNS record type: A/AAAA/CNAME/MX, etc.")),
					"record_value": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": locale.Text(t.language, "Optional subdomain DNS values, for example [\"1.2.3.4\",\"2.3.4.5\"]"),
					},
					// ip
					"ip": str(locale.Text(t.language, "IPv4/IPv6 address, never a hostname. For hostnames use type=subdomain with domain. Required for ip assets; optional for service/endpoint IP association.")),
					"bound_domains": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": locale.Text(t.language, "Optional domains bound to this IP"),
					},
					"open_ports": map[string]any{
						"type":        "array",
						"description": locale.Text(t.language, "Optional open ports for an IP asset"),
						"items": obj(map[string]any{
							"port":    intp(locale.Text(t.language, "Port number")),
							"service": str(locale.Text(t.language, "Optional service name, such as http/ssh/mysql")),
						}, "port"),
					},
					// app
					"app_name":    str(locale.Text(t.language, "Application name; required for app assets")),
					"bundle_id":   str(locale.Text(t.language, "Optional application bundle ID")),
					"category":    str(locale.Text(t.language, "Optional application category")),
					"description": str(locale.Text(t.language, "Optional application description")),
					"app_icp":     str(locale.Text(t.language, "Optional application ICP registration")),
					"company_id":  intp(locale.Text(t.language, "Optional owning company ID for apps. Apps cannot be attributed automatically from scope; explicitly supply an ID returned by add_company_scope.")),
					// service (http)
					"url":         str(locale.Text(t.language, "Full URL with scheme and port; required for HTTP services (service_type becomes http)")),
					"status_code": intp(locale.Text(t.language, "Optional HTTP response status, such as 200/301/403/404")),
					"content_length": map[string]any{
						"type":        "integer",
						"description": locale.Text(t.language, "Optional HTTP response body length in bytes"),
					},
					"page_title":   str(locale.Text(t.language, "Optional page <title> content")),
					"favicon_mmh3": str(locale.Text(t.language, "Optional favicon MMH3 hash")),
					"technologies": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": locale.Text(t.language, "Optional fingerprint/technology list, such as [\"Nginx\",\"Vue\",\"Bootstrap\"]"),
					},
					"auth": map[string]any{
						"type":        "array",
						"description": locale.Text(t.language, "Optional discovered authentication entries with type/username/password, etc.; appended without overwriting"),
						"items":       map[string]any{"type": "object"},
					},
					// Non-HTTP service.
					"service_name": str(locale.Text(t.language, "Service name such as ssh/mysql/redis; required for non-HTTP services")),
					"port":         intp(locale.Text(t.language, "Port number; required for non-HTTP services")),
					// endpoint
					"method": str(locale.Text(t.language, "HTTP method such as GET/POST/PUT/PATCH/DELETE; required for endpoints")),
					"params": map[string]any{
						"type":        "array",
						"description": locale.Text(t.language, "Optional request parameters with location(query/body/header/path)/name/value/type; appended without overwriting"),
						"items":       map[string]any{"type": "object"},
					},
				}, "type"),
			},
		}, "assets"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "insert_assets unavailable: AssetStore is not initialized")), nil
			}
			var a struct {
				Assets []assetInputItem `json:"assets"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("invalid input: " + locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			// Program-owned task_id avoids omitted/wrong model values misattributing assets.
			// Callers without task context, such as auto/pentest/chat, have t.taskID=0.
			taskID := t.taskID

			type result struct {
				Index int    `json:"index"`
				ID    int64  `json:"id"`
				Type  string `json:"type"`
			}
			type errEntry struct {
				Index int    `json:"index"`
				Error string `json:"error"`
			}

			var results []result
			var errs []errEntry

			// Load asset-gate rules once; a read failure skips evaluation rather than blocking insertion.
			// Blocks combine global and task rules; allows are task-local.
			blockRules, _ := t.as.ListAssetInterceptRules()
			var allowRules []db.AssetInterceptRule
			if t.taskID > 0 {
				if tb, ta, err := t.as.TaskInterceptRulesSplit(t.taskID); err == nil {
					blockRules = append(blockRules, tb...)
					allowRules = ta
				}
			}

			for i, item := range a.Assets {
				// Evaluate blocks then allows; rejected assets skip Upsert and all subsequent side effects.
				domains, ips, urls := assetInterceptCandidates(item)
				if d := db.EvaluateAssetGateForLanguage(locale.FromContext(ctx), blockRules, allowRules, domains, ips, urls); !d.Allowed {
					errs = append(errs, errEntry{
						Index: i,
						Error: fmt.Sprintf(locale.Text(locale.FromContext(ctx), "Asset %s %s; insertion prohibited"), assetInputLabel(item), d.Reason),
					})
					continue
				}

				typ := strings.TrimSpace(item.Type)
				var id int64
				var err error

				switch typ {
				case "root_domain":
					id, err = t.as.UpsertRootDomain(db.UpsertRootDomainReq{
						Domain: item.Domain,
						ICP:    item.ICP,
						TaskID: taskID,
					})

				case "ip":
					id, err = t.as.UpsertIP(db.UpsertIPReq{
						IP:           item.IP,
						BoundDomains: item.BoundDomains,
						OpenPorts:    item.OpenPorts,
						TaskID:       taskID,
					})

				case "subdomain":
					id, err = t.as.UpsertSubdomain(db.UpsertSubdomainReq{
						Domain:      item.Domain,
						RecordType:  item.RecordType,
						RecordValue: item.RecordValue,
						ICP:         item.ICP,
						TaskID:      taskID,
					})

				case "app":
					id, err = t.as.UpsertApp(db.UpsertAppReq{
						Name:        item.AppName,
						BundleID:    item.BundleID,
						Category:    item.Category,
						Description: item.Description,
						ICP:         item.AppICP,
						CompanyID:   item.CompanyID,
						TaskID:      taskID,
					})

				case "service":
					// distinguish HTTP vs other by presence of url
					if item.URL != "" {
						// agent may send "ip" or "service_ip" for the enrichment IP; accept both
						svcIP := item.ServiceIP
						if svcIP == "" {
							svcIP = item.IP
						}
						id, err = t.as.UpsertHTTPService(db.UpsertHTTPServiceReq{
							URL:           item.URL,
							Technologies:  item.Technologies,
							StatusCode:    item.StatusCode,
							ContentLength: item.ContentLength,
							PageTitle:     item.PageTitle,
							FaviconMMH3:   item.FaviconMMH3,
							Auth:          item.Auth,
							IP:            svcIP,
							TaskID:        taskID,
						})
					} else {
						id, err = t.as.UpsertOtherService(db.UpsertOtherServiceReq{
							Domain:      item.Domain,
							IP:          item.IP,
							Port:        item.Port,
							ServiceName: item.ServiceName,
							Auth:        item.Auth,
							TaskID:      taskID,
						})
					}

				case "endpoint":
					id, err = t.as.UpsertEndpoint(db.UpsertEndpointReq{
						URL:    item.URL,
						Method: item.Method,
						Params: item.Params,
						IP:     item.ServiceIP,
						TaskID: taskID,
					})

				default:
					errs = append(errs, errEntry{Index: i, Error: "unknown type: " + typ})
					continue
				}

				if err != nil {
					errs = append(errs, errEntry{Index: i, Error: err.Error()})
					continue
				}
				results = append(results, result{Index: i, ID: id, Type: typ})
				t.writes.Assets++
				t.anchorOwner(id)
				if taskID > 0 {
					var sourceNodeID *int64
					if t.ownerNode > 0 {
						nodeID := t.ownerNode
						sourceNodeID = &nodeID
					}
					summary := locale.Text(locale.FromContext(ctx), "Registered by agent through insert_assets")
					if t.ownerNode > 0 {
						summary = fmt.Sprintf(locale.Text(locale.FromContext(ctx), "Registered by worker intent #%d through insert_assets"), t.ownerNode)
					}
					_ = t.as.SetTaskAssetSource(taskID, id, "agent", summary, sourceNodeID)
				}
				// Auto-scope only the worker's explicitly inserted top-level item at conservative
				// granularity. Derived assets bypass this path, preventing scope expansion; taskID=0 is a no-op.
				// Scope accumulation is independent of coverage metrics: task_scope defines query
				// boundaries, while the coverage toggle only controls whether metrics use that denominator.
				{
					svcIP := item.ServiceIP
					if svcIP == "" {
						svcIP = item.IP
					}
					_ = t.as.AddAutoScope(taskID, typ, item.Domain, item.URL, svcIP)
				}
			}

			return jsonResult(map[string]any{
				"results": results,
				"errors":  errs,
			})
		},
	)
}

// addCompanyScope writes to company_scope table and triggers asset attribution.
func (t *ToolSet) addCompanyScope() actool.CoreTool {
	return writeTool(
		"add_company_scope",
		locale.Text(t.language, "Add domains/IPs/CIDRs/ICP registrations/company keywords to a company's asset scope. Domains, networks, and ICP entries claim matching assets automatically; keywords only give agents scope hints.\n")+
			locale.Text(t.language, "Company names are unique: create if missing, otherwise reuse and merge scope.\n")+
			locale.Text(t.language, "One scope entry per line, automatically detected as root domain, URL, IP, CIDR, ICP registration, or company keyword.\n")+
			locale.Text(t.language, "Always provide reason with attribution evidence such as whois, certificate, or ASN.\n")+
			locale.Text(t.language, "Guardrails reject bare TLDs and overly broad networks (IPv4 /16-/32, IPv6 /32-/128 only). Invalid lines are skipped and returned in errors."),
		obj(map[string]any{
			"company": str(locale.Text(t.language, "Unique company name: create if missing, reuse if present")),
			"scope":   str(locale.Text(t.language, "One scope entry per line: domain/URL/IP/CIDR/ICP registration/company keyword")),
			"reason":  str(locale.Text(t.language, "Required attribution reason with evidence/source")),
			"logo":    str(locale.Text(t.language, "Optional company icon URL; applies only when creating a company")),
		}, "company", "scope"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.cs == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "add_company_scope unavailable: CompanyStore is not initialized")), nil
			}
			var a struct {
				Company string `json:"company"`
				Scope   string `json:"scope"`
				Reason  string `json:"reason"`
				Logo    string `json:"logo"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			if strings.TrimSpace(a.Company) == "" {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "company cannot be empty")), nil
			}
			companyID, _, err := t.cs.UpsertCompany(a.Company, a.Logo)
			if err != nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "Create/get company failed: ") + locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			lines := splitLines(a.Scope)
			added, skipped, invalid, errMsgs := t.cs.AddScope(companyID, lines, a.Reason)
			out := map[string]any{
				"company_id": companyID,
				"added":      added,
				"skipped":    skipped,
				"invalid":    invalid,
			}
			if len(errMsgs) > 0 {
				out["errors"] = errMsgs
			}
			return jsonResult(out)
		},
	)
}

// addTaskScope lets the plan agent add test scope to THE CURRENT TASK — the coverage
// denominator and the task's authorization edge. Worker discoveries are auto-scoped
// (precise host) by insertAssets; this tool is for DELIBERATELY WIDENING: pull a whole
// root domain or whole company into scope, or add a specific subdomain / ip.
func (t *ToolSet) addTaskScope() actool.CoreTool {
	return writeTool(
		"add_task_scope",
		locale.Text(t.language, "Add test scope to THIS task: its authorization boundary and asset coverage denominator.\n")+
			locale.Text(t.language, "Kinds: company (all company assets), root_domain (entire domain and subdomains), subdomain (one exact hostname), ip, cidr, icp, keyword.\n")+
			locale.Text(t.language, "Hosts encountered by workers are added automatically as exact subdomains. This tool explicitly expands authorized scope to a whole root domain/company or adds a specified subdomain/IP.\n")+
			locale.Text(t.language, "value: existing company name/ID for company; domain for root_domain/subdomain; IP/network for ip/cidr; registration number/company keyword for icp/keyword.\n")+
			locale.Text(t.language, "Always provide an auditable reason. Use entries for multiple scope items."),
		obj(map[string]any{
			"entries": map[string]any{"type": "array", "description": locale.Text(t.language, "Batch [{kind,value}], with kind company/root_domain/subdomain/ip/cidr/icp/keyword"), "items": map[string]any{"type": "object"}},
			"kind":    str(locale.Text(t.language, "Single item: company/root_domain/subdomain/ip/cidr/icp/keyword")),
			"value":   str(locale.Text(t.language, "Single item: company name/ID, domain, IP, CIDR, ICP, or keyword")),
			"reason":  str(locale.Text(t.language, "Required auditable reason for adding scope")),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "add_task_scope unavailable: AssetStore is not initialized")), nil
			}
			if t.taskID <= 0 {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "add_task_scope requires task context; no current task")), nil
			}
			type scopeEntry struct {
				Kind  string `json:"kind"`
				Value string `json:"value"`
			}
			var a struct {
				Entries    []scopeEntry `json:"entries"`
				scopeEntry              // Single-item mode.
				Reason     string       `json:"reason"`
			}
			_ = json.Unmarshal(in, &a)
			items := a.Entries
			if len(items) == 0 {
				items = []scopeEntry{a.scopeEntry}
			}
			var added []map[string]any
			errs := map[string]string{}
			for i, e := range items {
				ts, err := t.as.AddAgentScope(t.taskID, strings.TrimSpace(e.Kind), e.Value, a.Reason, "agent")
				if err != nil {
					errs[strconv.Itoa(i)] = err.Error()
					continue
				}
				added = append(added, map[string]any{"kind": ts.Kind, "domain": ts.Domain, "net": ts.Net, "value": ts.Value, "company_id": ts.CompanyID})
			}
			out := map[string]any{"added": added}
			if len(errs) > 0 {
				out["errors"] = errs
			}
			return jsonResult(out)
		},
	)
}

// listUntestedAssets lets the plan agent pull the current + directly inherited
// scope's not-yet-tested assets on demand (filter by type, paginated).
func (t *ToolSet) listUntestedAssets() actool.CoreTool {
	return readTool(
		"list_untested_assets",
		locale.Text(t.language, "Find assets within this task's and directly associated tasks' scopes that are not covered by fact anchors. Associated scope is read-only. Use the result to decide whether more tests are needed; it does not decide for you.\n")+
			locale.Text(t.language, "Optional asset-type filter: root_domain/subdomain/service/app/endpoint/ip.\n")+
			locale.Text(t.language, "Pagination starts at page 1; page_size defaults to 10. Returns {assets:[{id,type,label}],total,page,page_size}. Requires task context."),
		obj(map[string]any{
			"type":      str(locale.Text(t.language, "Optional asset type: root_domain/subdomain/service/app/endpoint/ip")),
			"page":      intp(locale.Text(t.language, "Page number, starting at 1 (default 1)")),
			"page_size": intp(locale.Text(t.language, "Page size, default 10")),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "list_untested_assets unavailable: AssetStore is not initialized")), nil
			}
			if t.taskID <= 0 || t.ts == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "list_untested_assets requires task context")), nil
			}
			var a struct {
				Type     string `json:"type"`
				Page     int    `json:"page"`
				PageSize int    `json:"page_size"`
			}
			_ = json.Unmarshal(in, &a)
			if a.Page <= 0 {
				a.Page = 1
			}
			if a.PageSize <= 0 {
				a.PageSize = 10
			}
			offset := (a.Page - 1) * a.PageSize
			assets, total, err := t.as.ListUntestedAssetsWithSources(t.taskID, strings.TrimSpace(a.Type), a.PageSize, offset)
			if err != nil {
				return actool.Errorf(locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			return jsonResult(map[string]any{
				"assets": assets, "total": total, "page": a.Page, "page_size": a.PageSize,
			})
		},
	)
}

// listAssets lets an agent query the asset table.
func (t *ToolSet) listAssets() actool.CoreTool {
	return readTool(
		"list_assets",
		locale.Text(t.language, "Search the asset store with DSL or retrieve by id/ids, with pagination. Only assets in this task's and directly associated tasks' test scopes are returned.\n")+
			locale.Text(t.language, "DSL: field=value fuzzy (ILIKE), field==value exact, field!=value exclusion; numeric fields support > >= < <=; bare terms search all text. Combine AND/OR (AND binds tighter), with parentheses for groups. Supply asset type separately, not in DSL.\n")+
			locale.Text(t.language, "Without id/ids, dsl must be nonempty; unfiltered full-store queries are prohibited.\n")+
			locale.Text(t.language, "Fields: domain (root/subdomain/service hostname), root_domain, ip, url, page_title, icp, service_name, app_name, method (GET/POST), service_type (http or other), record_type (A/CNAME), technology (array: = fuzzy, == exact), and integer port/status_code/company_id.\n")+
			locale.Text(t.language, "Examples: status_code>=400 AND technology=shiro; (port==80 OR port==443) AND technology=nginx"),
		obj(map[string]any{
			"dsl":    str(locale.Text(t.language, "DSL expression; see tool description for syntax/fields. Required when id/ids are omitted.")),
			"type":   str(locale.Text(t.language, "Asset type filter: root_domain, ip, subdomain, app, service, endpoint. Combines with dsl; type alone is insufficient, dsl is still required.")),
			"id":     intp(locale.Text(t.language, "Optional single asset ID; mutually exclusive with dsl/type")),
			"ids":    map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": locale.Text(t.language, "Optional multiple asset IDs; mutually exclusive with dsl/type")},
			"limit":  intp(locale.Text(t.language, "Optional result limit, default 10")),
			"offset": intp(locale.Text(t.language, "Optional pagination offset, default 0")),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.as == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "list_assets unavailable: AssetStore is not initialized")), nil
			}
			var a struct {
				DSL    string  `json:"dsl"`
				Type   string  `json:"type"`
				ID     int64   `json:"id"`
				IDs    []int64 `json:"ids"`
				Limit  int     `json:"limit"`
				Offset int     `json:"offset"`
			}
			_ = json.Unmarshal(in, &a)
			if a.Limit <= 0 {
				a.Limit = 10
			}

			var assets []*db.Asset
			var err error
			switch {
			case a.ID > 0:
				assets, err = t.as.GetByIDsInScope(t.taskID, []int64{a.ID})
			case len(a.IDs) > 0:
				assets, err = t.as.GetByIDsInScope(t.taskID, a.IDs)
			case a.DSL != "":
				assets, err = t.as.QueryDSLInScope(a.DSL, a.Type, t.taskID, a.Limit, a.Offset)
			default:
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "dsl cannot be empty without id/ids: unfiltered full-store queries are prohibited; provide search criteria")), nil
			}
			if err != nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "DSL error: ") + locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			return jsonResult(map[string]any{
				"count":  len(assets),
				"assets": assets,
			})
		},
	)
}

// listCompanies enumerates companies with their scopes and asset counts.
func (t *ToolSet) listCompanies() actool.CoreTool {
	return readTool(
		"list_companies",
		locale.Text(t.language, "List companies in the asset store with their scope and attributed asset counts. Use this to inspect companies and ")+
			locale.Text(t.language, "obtain company_id for app association in insert_assets or company filtering in list_assets.")+
			locale.Text(t.language, "Optional search filters company names case-insensitively; empty returns all."),
		obj(map[string]any{
			"search": str(locale.Text(t.language, "Optional case-insensitive company-name filter; empty returns all")),
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.cs == nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "list_companies unavailable: CompanyStore is not initialized")), nil
			}
			var a struct {
				Search string `json:"search"`
			}
			_ = json.Unmarshal(in, &a)
			cos, err := t.cs.ListCompanies()
			if err != nil {
				return actool.Errorf(locale.Text(locale.FromContext(ctx), "Company query failed: ") + locale.ErrorMessage(locale.FromContext(ctx), err)), nil
			}
			q := strings.ToLower(strings.TrimSpace(a.Search))
			type companyOut struct {
				ID         int64    `json:"id"`
				Name       string   `json:"name"`
				AssetCount int      `json:"asset_count"`
				Scope      []string `json:"scope"`
			}
			out := make([]companyOut, 0, len(cos))
			for _, c := range cos {
				if q != "" && !strings.Contains(strings.ToLower(c.Name), q) {
					continue
				}
				scope := make([]string, 0, len(c.Scope))
				for _, r := range c.Scope {
					scope = append(scope, r.Raw)
				}
				out = append(out, companyOut{ID: c.ID, Name: c.Name, AssetCount: c.AssetCount, Scope: scope})
			}
			return jsonResult(map[string]any{"count": len(out), "companies": out})
		},
	)
}

// splitLines splits a multi-line string into non-empty trimmed lines.
func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// WorkerTools returns the tool set for a work agent.
func (t *ToolSet) WorkerTools() []actool.CoreTool {
	return []actool.CoreTool{
		// Keep list_findings so workers can check existing confirmed findings before reporting duplicates.
		t.listFindings(),
		t.addFinding(), t.recordFact(),
		// asset management (handlers guard nil store internally)。
		// Workers do not get add_company_scope; defining company scope belongs to planner/main/Auto.
		t.insertAssets(), t.listAssets(),
		// Workers may reuse other work traces to avoid duplicate exploration.
		// search_all_worker_traces finds keyword matches without first knowing intent_id.
		// get_worker_trace lists/searches steps and retrieves full content for one work item.
		t.searchAllWorkerTraces(), t.getWorkerTrace(),
		// node_detail fetches full details once the worker knows an intent/node ID.
		t.nodeDetail(),
		// Keep list_facts/list_companies/list_worker_traces with planner/main: workers
		// execute and record one intent, while broader context review belongs to planning.
	}
}

// MainAgentTools returns the human-interface tool set.
func (t *ToolSet) MainAgentTools() []actool.CoreTool {
	return []actool.CoreTool{
		t.graphOverview(), t.listFindings(), t.listFacts(), t.nodeDetail(),
		t.expandDigest(), // cold-digest §6.1
		t.getWorkerOutput(), t.getWorkerTrace(), t.searchAllWorkerTraces(), t.addHint(), t.addIntent(),
		// steer_work lets humans correct a running worker without interruption or progress loss.
		t.steerWorkTool(),
		// set_goals lets humans add final objectives at runtime for planner reassessment.
		t.setGoals(),
		// set_constraints lets humans add/change allow/deny boundaries for planner/worker exploration.
		t.setConstraints(),
		// asset management (handlers guard nil store internally)
		t.insertAssets(), t.addCompanyScope(), t.listAssets(),
		t.addFinding(), t.recordFact(),
		t.addTaskScope(),
		// list_untested_assets supports type-filtered, paginated inspection for additional testing decisions.
		t.listUntestedAssets(),
	}
}

// AllDomainTools returns the union of all domain tools across all agent types,
// deduped by name (mainagent order wins). Used by the server to build a registry
// for injecting domain tools into agents (Auto, custom) that don't own a per-task
// ToolSet. The caller provides real stores; tools are callable at taskID=0 scope.
func (t *ToolSet) AllDomainTools() []actool.CoreTool {
	seen := map[string]bool{}
	var out []actool.CoreTool
	all := append(append(t.MainAgentTools(), t.PlannerTools()...), t.WorkerTools()...)
	for _, tool := range all {
		if !seen[tool.Name()] {
			seen[tool.Name()] = true
			out = append(out, tool)
		}
	}
	return out
}
