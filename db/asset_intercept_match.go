package db

import (
	"fmt"
	"github.com/sebastian93921/artifex/locale"
	"net"
	"net/url"
	"strings"
)

// Asset interception matching/execution layer. asset_intercept.go stores rules;
// this file matches target domains, IPs, and URLs against enabled rules. Agent tools
// (add_intent, insert_assets) call it before dispatching intents or inserting assets and reject matches.

// AssetInterceptKindLabel returns the human-readable kind label for agent messages.
func AssetInterceptKindLabel(kind string) string {
	return AssetInterceptKindLabelForLanguage(locale.ServerDefault(), kind)
}

// AssetInterceptKindLabelForLanguage renders only the built-in kind label.
func AssetInterceptKindLabelForLanguage(lang locale.Lang, kind string) string {
	switch kind {
	case "exact_domain":
		return locale.Text(lang, "Domain (exact)")
	case "exact_ip":
		return locale.Text(lang, "IP (exact)")
	case "exact_url":
		return locale.Text(lang, "URL (exact)")
	case "fuzzy_domain":
		return locale.Text(lang, "Domain (partial)")
	case "fuzzy_ip":
		return locale.Text(lang, "IP (partial)")
	case "fuzzy_url":
		return locale.Text(lang, "URL (partial)")
	case "cidr":
		return locale.Text(lang, "CIDR network")
	}
	return kind
}

// Reason returns a readable match reason, including the rule kind, pattern, and optional note.
func (r AssetInterceptRule) Reason() string { return r.ReasonForLanguage(locale.ServerDefault()) }

// ReasonForLanguage preserves raw patterns and user notes.
func (r AssetInterceptRule) ReasonForLanguage(lang locale.Lang) string {
	s := locale.Text(lang, "Matched asset interception rule [%s: %s]", AssetInterceptKindLabelForLanguage(lang, r.Kind), r.Pattern)
	if note := strings.TrimSpace(r.Note); note != "" {
		s += " (" + note + ")"
	}
	return s
}

// matchOne tests one enabled rule against domain/IP/URL candidates and returns the matching value.
func matchOne(r AssetInterceptRule, domains, ips, urls []string) (string, bool) {
	p := strings.TrimSpace(r.Pattern)
	if p == "" {
		return "", false
	}
	switch r.Kind {
	case "exact_domain":
		for _, d := range domains {
			if strings.EqualFold(strings.TrimSpace(d), p) {
				return d, true
			}
		}
	case "exact_ip":
		for _, ip := range ips {
			if strings.TrimSpace(ip) == p {
				return ip, true
			}
		}
	case "exact_url":
		for _, u := range urls {
			if strings.TrimSpace(u) == p {
				return u, true
			}
		}
	case "fuzzy_domain":
		lp := strings.ToLower(p)
		for _, d := range domains {
			if d != "" && strings.Contains(strings.ToLower(d), lp) {
				return d, true
			}
		}
	case "fuzzy_ip":
		for _, ip := range ips {
			if ip != "" && strings.Contains(ip, p) {
				return ip, true
			}
		}
	case "fuzzy_url":
		lp := strings.ToLower(p)
		for _, u := range urls {
			if u != "" && strings.Contains(strings.ToLower(u), lp) {
				return u, true
			}
		}
	case "cidr":
		_, ipnet, err := net.ParseCIDR(p)
		if err != nil {
			return "", false
		}
		for _, ip := range ips {
			if pip := net.ParseIP(strings.TrimSpace(ip)); pip != nil && ipnet.Contains(pip) {
				return ip, true
			}
		}
	}
	return "", false
}

// MatchAssetInterceptRules returns the first enabled matching rule and the matched value.
// insert_assets uses this with raw input (assetInputItem not yet persisted).
func MatchAssetInterceptRules(rules []AssetInterceptRule, domains, ips, urls []string) (AssetInterceptRule, string, bool) {
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if v, ok := matchOne(r, domains, ips, urls); ok {
			return r, v, true
		}
	}
	return AssetInterceptRule{}, "", false
}

// interceptCandidates extracts domain/IP/URL candidates from a persisted asset.
// URL hosts are classified so domain/IP rules also cover service assets that contain only a URL.
func (a *Asset) interceptCandidates() (domains, ips, urls []string) {
	add := func(dst *[]string, s string) {
		if s = strings.TrimSpace(s); s != "" {
			*dst = append(*dst, s)
		}
	}
	add(&domains, a.Domain)
	add(&domains, a.RootDomain)
	for _, d := range a.BoundDomains {
		add(&domains, d)
	}
	add(&ips, a.IP)
	add(&urls, a.URL)
	if a.URL != "" {
		if u, err := url.Parse(a.URL); err == nil {
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

// InterceptLabel returns a short asset identifier for agent messages.
func (a *Asset) InterceptLabel() string { return a.InterceptLabelForLanguage(locale.ServerDefault()) }

// InterceptLabelForLanguage renders the label while preserving the asset identifier.
func (a *Asset) InterceptLabelForLanguage(lang locale.Lang) string {
	var target string
	switch {
	case a.Domain != "":
		target = a.Domain
	case a.URL != "":
		target = a.URL
	case a.IP != "":
		target = a.IP
	default:
		target = fmt.Sprintf("#%d", a.ID)
	}
	return locale.Text(lang, "Asset #%d [%s] %s", a.ID, a.Type, target)
}

// hasEnabledRule reports whether any rule in the set is enabled.
func hasEnabledRule(rules []AssetInterceptRule) bool {
	for _, r := range rules {
		if r.Enabled {
			return true
		}
	}
	return false
}

// AssetGateDecision is the block-before-allow decision for a set of candidates.
type AssetGateDecision struct {
	Allowed bool
	Reason  string // Rejection reason without the asset label; empty when Allowed=true.
}

// EvaluateAssetGate evaluates the task gate:
//  1. Any enabled blockRules match rejects the asset with the interception reason.
//  2. Otherwise, enabled allowRules with no match reject it as outside the allowed scope.
//  3. Otherwise, allow it.
//
// Empty or entirely disabled allowRules disable the allowlist gate and permit all candidates,
// avoiding rejection of every asset when no allow rules are configured.
func EvaluateAssetGate(blockRules, allowRules []AssetInterceptRule, domains, ips, urls []string) AssetGateDecision {
	return EvaluateAssetGateForLanguage(locale.ServerDefault(), blockRules, allowRules, domains, ips, urls)
}

// EvaluateAssetGateForLanguage changes message language without changing rule evaluation.
func EvaluateAssetGateForLanguage(lang locale.Lang, blockRules, allowRules []AssetInterceptRule, domains, ips, urls []string) AssetGateDecision {
	if rule, _, ok := MatchAssetInterceptRules(blockRules, domains, ips, urls); ok {
		return AssetGateDecision{Allowed: false, Reason: rule.ReasonForLanguage(lang)}
	}
	if hasEnabledRule(allowRules) {
		if _, _, ok := MatchAssetInterceptRules(allowRules, domains, ips, urls); !ok {
			return AssetGateDecision{Allowed: false, Reason: locale.Text(lang, "Testing is not allowed outside the task's allowed scope (allowlist)")}
		}
	}
	return AssetGateDecision{Allowed: true}
}

// AssetInterceptHit describes an asset rejected by a block match or an allowlist miss.
type AssetInterceptHit struct {
	Asset  *Asset
	Reason string // Human-readable reason.
}

// Describe returns readable asset information followed by the reason.
func (h AssetInterceptHit) Describe() string { return h.DescribeForLanguage(locale.ServerDefault()) }

// DescribeForLanguage localizes the asset label; the gate already rendered Reason.
func (h AssetInterceptHit) DescribeForLanguage(lang locale.Lang) string {
	return fmt.Sprintf("%s → %s", h.Asset.InterceptLabelForLanguage(lang), h.Reason)
}

// ListAssetInterceptRules forwards to the *DB method, allowing callers holding only
// an AssetStore (such as agent tools) to read the rules.
func (s *AssetStore) ListAssetInterceptRules() ([]AssetInterceptRule, error) {
	return s.db.ListAssetInterceptRules()
}

// CheckAssetsIntercept loads assets by ID, applies block-before-allow gating, and returns
// all rejected assets. Block rules combine global and task rules; allow rules belong only to this task.
// No IDs returns immediately. Global GetByIDs bypasses task scope so scope cannot weaken interception.
func (s *AssetStore) CheckAssetsIntercept(taskID int64, ids []int64) ([]AssetInterceptHit, error) {
	return s.CheckAssetsInterceptForLanguage(locale.ServerDefault(), taskID, ids)
}

// CheckAssetsInterceptForLanguage retains scope-independent blocking and renders messages for the caller.
func (s *AssetStore) CheckAssetsInterceptForLanguage(lang locale.Lang, taskID int64, ids []int64) ([]AssetInterceptHit, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	blockRules, err := s.db.ListAssetInterceptRules()
	if err != nil {
		return nil, err
	}
	var allowRules []AssetInterceptRule
	if taskID > 0 {
		tb, ta, err := s.TaskInterceptRulesSplit(taskID)
		if err != nil {
			return nil, err
		}
		blockRules = append(blockRules, tb...)
		allowRules = ta
	}
	// No block rules or enabled allow rules means no checks are needed; allow all assets.
	if len(blockRules) == 0 && !hasEnabledRule(allowRules) {
		return nil, nil
	}
	assets, err := s.GetByIDs(ids)
	if err != nil {
		return nil, err
	}
	var hits []AssetInterceptHit
	for _, a := range assets {
		domains, ips, urls := a.interceptCandidates()
		if d := EvaluateAssetGateForLanguage(lang, blockRules, allowRules, domains, ips, urls); !d.Allowed {
			hits = append(hits, AssetInterceptHit{Asset: a, Reason: d.Reason})
		}
	}
	return hits, nil
}
