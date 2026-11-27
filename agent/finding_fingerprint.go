package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sebastian93921/artifex/db"
)

// findingFingerprintMatch is a deterministic duplicate hit against an already
// registered finding (#473), complementing the advisory LLM triage pass.
type findingFingerprintMatch struct {
	FindingID   int64
	NodeID      int64
	Fingerprint string
}

var (
	fpURLRe        = regexp.MustCompile(`(?i)\bhttps?://[^\s"'<>\]\)]+`)
	fpMethodPathRe = regexp.MustCompile(`(?i)\b(?:GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\s+(/[^\s"'<>\]\)]+)`)
	fpBarePathRe   = regexp.MustCompile(`(?:^|[\s(\["'])(/[a-zA-Z0-9._~/%{}:-]+(?:\?[a-zA-Z0-9._~=&%-]+)?)`)
	fpParamRe      = regexp.MustCompile(`(?i)\b(?:param(?:eter)?|field|query\s+param(?:eter)?)\s*(?:=|:|\s+)\s*["'\x60]?([a-zA-Z_][a-zA-Z0-9_.-]{0,63})["'\x60]?`)
	fpNonAlnumRe   = regexp.MustCompile(`[^a-z0-9]+`)
)

// FindingFingerprint returns a deterministic SHA-256 fingerprint for a finding
// from three required axes (#473):
//  1. normalized vulnerability class / CWE,
//  2. canonical asset IDs (sorted/deduplicated) or URL host,
//  3. normalized affected endpoint path and/or parameter name.
//
// If any of the three axes cannot be extracted unambiguously, it returns "" so
// callers fall through to LLM triage / normal registration without false
// collisions.
func FindingFingerprint(in db.RecordFindingInput) string {
	vulnClass := normalizeFingerprintToken(in.VulnClass)
	if vulnClass == "" {
		return ""
	}
	corpus := strings.Join([]string{in.Name, in.Summary, in.Evidence}, "\n")
	assetKey := canonicalFindingAssetKey(in.AssetIDs, corpus)
	if assetKey == "" {
		return ""
	}
	endpointKey := canonicalFindingEndpointKey(corpus)
	if endpointKey == "" {
		return ""
	}
	h := sha256.Sum256([]byte(vulnClass + "\x00" + assetKey + "\x00" + endpointKey))
	return "fp:" + hex.EncodeToString(h[:16])
}

func normalizeFingerprintToken(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = fpNonAlnumRe.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

func canonicalFindingAssetKey(assetIDs []int64, corpus string) string {
	if len(assetIDs) > 0 {
		seen := make(map[int64]struct{}, len(assetIDs))
		ids := make([]int64, 0, len(assetIDs))
		for _, id := range assetIDs {
			if id > 0 {
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					ids = append(ids, id)
				}
			}
		}
		if len(ids) > 0 {
			sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
			parts := make([]string, len(ids))
			for i, id := range ids {
				parts[i] = strconv.FormatInt(id, 10)
			}
			return "ids:" + strings.Join(parts, ",")
		}
	}
	for _, raw := range fpURLRe.FindAllString(corpus, 4) {
		raw = strings.TrimRight(raw, ".,;:!?)")
		u, err := url.Parse(raw)
		if err != nil || strings.TrimSpace(u.Host) == "" {
			continue
		}
		return "host:" + strings.ToLower(strings.TrimSuffix(u.Host, "."))
	}
	return ""
}

func canonicalFindingEndpointKey(corpus string) string {
	var pathSig string
	var paramNames []string

	// Prefer explicit URLs first so scheme/host are stripped cleanly.
	for _, raw := range fpURLRe.FindAllString(corpus, 4) {
		raw = strings.TrimRight(raw, ".,;:!?)")
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		p := normalizeEndpointPath(u.Path)
		if p != "" {
			pathSig = p
			paramNames = append(paramNames, sortedQueryParamNames(u.RawQuery)...)
			break
		}
	}
	if pathSig == "" {
		if m := fpMethodPathRe.FindStringSubmatch(corpus); len(m) == 2 {
			pathSig, paramNames = splitPathAndQuery(m[1])
		}
	}
	if pathSig == "" {
		for _, m := range fpBarePathRe.FindAllStringSubmatch(corpus, 8) {
			if len(m) != 2 {
				continue
			}
			candidate := strings.TrimRight(m[1], ".,;:!?)")
			// Ignore common filesystem/OS paths unless nothing else; require
			// at least one route character after '/'.
			if len(candidate) <= 1 || strings.HasPrefix(candidate, "//") {
				continue
			}
			pathSig, paramNames = splitPathAndQuery(candidate)
			if pathSig != "" {
				break
			}
		}
	}
	for _, m := range fpParamRe.FindAllStringSubmatch(corpus, 4) {
		if len(m) == 2 {
			if name := strings.ToLower(strings.TrimSpace(m[1])); name != "" {
				paramNames = append(paramNames, name)
			}
		}
	}
	paramNames = dedupSortedStrings(paramNames)
	if pathSig == "" && len(paramNames) == 0 {
		return ""
	}
	if len(paramNames) == 0 {
		return "path:" + pathSig
	}
	return "path:" + pathSig + "|params:" + strings.Join(paramNames, ",")
}

func splitPathAndQuery(raw string) (string, []string) {
	raw = strings.TrimRight(strings.TrimSpace(raw), ".,;:!?)")
	pathPart, queryPart, _ := strings.Cut(raw, "?")
	return normalizeEndpointPath(pathPart), sortedQueryParamNames(queryPart)
}

func normalizeEndpointPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	p = strings.ToLower(p)
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	return p
}

func sortedQueryParamNames(rawQuery string) []string {
	if strings.TrimSpace(rawQuery) == "" {
		return nil
	}
	var names []string
	for _, pair := range strings.Split(rawQuery, "&") {
		if pair == "" {
			continue
		}
		k, _, _ := strings.Cut(pair, "=")
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			names = append(names, k)
		}
	}
	return dedupSortedStrings(names)
}

func dedupSortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func isDismissedFindingStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "false_positive", "ignored", "duplicate":
		return true
	default:
		return false
	}
}

// fingerprintDuplicate checks already registered active findings in this task,
// its directly related tasks, and tasks sharing the same assets for an exact
// deterministic fingerprint match (#473).
func (t *ToolSet) fingerprintDuplicate(in db.RecordFindingInput) (findingFingerprintMatch, bool) {
	if t.ts == nil {
		return findingFingerprintMatch{}, false
	}
	candFP := FindingFingerprint(in)
	if candFP == "" {
		return findingFingerprintMatch{}, false
	}
	nodes, err := t.ts.ListByKindWithSources(db.KindFinding, 500)
	if err != nil {
		slog.Warn("[dedup] fingerprint lookup skipped: failed to list findings", "err", err)
		return findingFingerprintMatch{}, false
	}
	if len(in.AssetIDs) > 0 {
		if cross, err := t.ts.FindingsSharingAssets(in.AssetIDs, t.taskID, 100); err == nil {
			seen := make(map[int64]bool, len(nodes))
			for _, n := range nodes {
				if n != nil {
					seen[n.ID] = true
				}
			}
			for _, n := range cross {
				if n != nil && !seen[n.ID] {
					nodes = append(nodes, n)
				}
			}
		}
	}
	if len(nodes) == 0 {
		return findingFingerprintMatch{}, false
	}
	if err := t.ts.PopulateFindingTrafficIDs(nodes); err != nil {
		slog.Warn("[dedup] fingerprint lookup skipped: populate finding ids failed", "err", err)
		return findingFingerprintMatch{}, false
	}
	findingIDs := make([]int64, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.FindingID > 0 {
			findingIDs = append(findingIDs, n.FindingID)
		}
	}
	if len(findingIDs) == 0 {
		return findingFingerprintMatch{}, false
	}
	meta, err := t.ts.FindingDedupMetadata(findingIDs)
	if err != nil {
		slog.Warn("[dedup] fingerprint lookup skipped: load finding metadata failed", "err", err)
		return findingFingerprintMatch{}, false
	}
	// Check newest first for deterministic match selection.
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].ID > nodes[j].ID })
	for _, n := range nodes {
		if n == nil || n.FindingID <= 0 {
			continue
		}
		m := meta[n.FindingID]
		if isDismissedFindingStatus(m.Status) {
			continue
		}
		var payload struct {
			VulnClass string `json:"vulnclass"`
			Name      string `json:"name"`
			Summary   string `json:"summary"`
			Status    string `json:"status"`
			Evidence  struct {
				PoC string `json:"poc"`
			} `json:"evidence"`
		}
		if json.Unmarshal(n.Payload, &payload) != nil {
			continue
		}
		if isDismissedFindingStatus(payload.Status) {
			continue
		}
		existingFP := FindingFingerprint(db.RecordFindingInput{
			VulnClass: payload.VulnClass,
			Name:      payload.Name,
			Summary:   payload.Summary,
			Evidence:  payload.Evidence.PoC,
			AssetIDs:  m.AssetIDs,
		})
		if existingFP != "" && existingFP == candFP {
			slog.Info("[dedup] deterministic fingerprint matched existing finding",
				"finding_id", n.FindingID, "node_id", n.ID, "fingerprint", candFP)
			return findingFingerprintMatch{
				FindingID:   n.FindingID,
				NodeID:      n.ID,
				Fingerprint: candFP,
			}, true
		}
	}
	return findingFingerprintMatch{}, false
}
