package server

import (
	"net"
	"net/http"
	"strings"

	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/locale"
)

// --- asset intercept rule CRUD ---
//
// Global asset blocklist: exact/fuzzy domain·ip·url + CIDR. This layer only
// persists rules; the actual match/enforcement logic lives elsewhere.

func (s *Server) assetInterceptListRules(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	rules, err := pg.ListAssetInterceptRules()
	if err != nil {
		writeError(w, 500, err)
		return
	}
	if rules == nil {
		rules = []db.AssetInterceptRule{}
	}
	writeJSON(w, 200, map[string]any{"rules": rules})
}

func (s *Server) assetInterceptCreateRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var req assetInterceptRuleReq
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err)
		return
	}
	if err := validateAssetInterceptRuleReq(&req); err != nil {
		writeError(w, 400, err)
		return
	}
	rule, err := pg.CreateAssetInterceptRule(req.Kind, req.Pattern, req.Note, req.Enabled)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, rule)
}

func (s *Server) assetInterceptUpdateRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad rule id"))
		return
	}
	var req assetInterceptRuleReq
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err)
		return
	}
	if err := validateAssetInterceptRuleReq(&req); err != nil {
		writeError(w, 400, err)
		return
	}
	rule, err := pg.UpdateAssetInterceptRule(id, req.Kind, req.Pattern, req.Note, req.Enabled)
	if err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, rule)
}

func (s *Server) assetInterceptDeleteRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad rule id"))
		return
	}
	if err := pg.DeleteAssetInterceptRule(id); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

func (s *Server) assetInterceptToggleRule(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeErr(w, 400, locale.Text(responseLanguage(w), "bad rule id"))
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &req); err != nil {
		writeError(w, 400, err)
		return
	}
	if err := pg.ToggleAssetInterceptRule(id, req.Enabled); err != nil {
		writeError(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "enabled": req.Enabled})
}

// --- helpers ---

type assetInterceptRuleReq struct {
	Enabled bool   `json:"enabled"`
	Kind    string `json:"kind"`
	Pattern string `json:"pattern"`
	Note    string `json:"note"`
}

// validateAssetInterceptRuleReq normalizes and validates a rule. It trims the
// pattern and, for the strictly-formatted kinds (exact_ip / cidr), rejects
// malformed values; fuzzy kinds and domain/url patterns are left as free text
// (the enforcement layer decides how to interpret them).
func validateAssetInterceptRuleReq(req *assetInterceptRuleReq) error {
	req.Pattern = strings.TrimSpace(req.Pattern)
	if req.Pattern == "" {
		return locale.Errorf("pattern is required")
	}
	switch req.Kind {
	case "exact_domain", "exact_url", "fuzzy_domain", "fuzzy_ip", "fuzzy_url":
		// free-form, no format check
	case "exact_ip":
		if net.ParseIP(req.Pattern) == nil {
			return locale.Errorf("exact_ip is not a valid IP address: %s", req.Pattern)
		}
	case "cidr":
		if _, _, err := net.ParseCIDR(req.Pattern); err != nil {
			return locale.Errorf("cidr is not a valid network (for example, 192.168.0.0/16): %s", req.Pattern)
		}
	default:
		return locale.Errorf("Invalid kind: %s", req.Kind)
	}
	return nil
}
