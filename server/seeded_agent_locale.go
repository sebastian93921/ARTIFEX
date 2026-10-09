package server

import (
	"github.com/sebastian93921/artifex/db"
	"github.com/sebastian93921/artifex/locale"
)

// Localize exact stock metadata on a response copy; edited values remain verbatim.
func localizeSeededAgentMetadata(a *db.Agent, lang locale.Lang) {
	if a.Key == "reporter" {
		if a.Name == "Report writer" || a.Name == "\u62a5\u544a\u64b0\u5199" {
			a.Name = locale.Text(lang, "Report writer")
		}
		if a.Description == "Writes detailed vulnerability reports: triggered by findings, reads evidence and execution traces, then saves a Markdown report." || a.Description == "\u6f0f\u6d1e\u8be6\u7ec6\u62a5\u544a\u64b0\u5199\uff1a\u53d1\u73b0\u6f0f\u6d1e\u65f6\u81ea\u52a8\u89e6\u53d1\uff0c\u67e5\u53d6\u8bc1\u636e\u4e0e\u6267\u884c\u8fc7\u7a0b\u540e\u5199 Markdown \u62a5\u544a\u5e76\u56de\u5199\u3002" {
			a.Description = locale.Text(lang, "Writes detailed vulnerability reports: triggered by findings, reads evidence and execution traces, then saves a Markdown report.")
		}
	}
	if a.Key == db.FindingRetestAgentKey {
		if a.Name == "Finding retest" || a.Name == "\u6f0f\u6d1e\u590d\u6d4b" {
			a.Name = locale.Text(lang, "Finding retest")
		}
		if a.Description == "Started manually from finding details; reads original evidence and saves an independent retest conclusion." || a.Description == "\u4ece\u6f0f\u6d1e\u8be6\u60c5\u624b\u52a8\u542f\u52a8\uff0c\u8bfb\u53d6\u539f\u8bc1\u636e\u5e76\u4fdd\u5b58\u72ec\u7acb\u590d\u6d4b\u7ed3\u8bba\u3002" {
			a.Description = locale.Text(lang, "Started manually from finding details; reads original evidence and saves an independent retest conclusion.")
		}
	}
}
