package server

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/Autumn-27/artex/locale"
)

func init() {
	locale.Register(reporterToolCallMessage, reporterToolCallMessage)
}

// reporterTriggerText localizes only exact bundled trigger messages, including
// their historical stock versions. Operator-authored trigger content is preserved.
func reporterTriggerText(text string, lang locale.Lang) string {
	digest := sha256.Sum256([]byte(text))
	if text == reporterToolCallMessage || isLegacyReporterTrigger(text) || hex.EncodeToString(digest[:]) == "2b766affdba89c92772698367e41fc6d570ee25f29964576c0aec96da4d176e6" {
		return locale.Text(lang, reporterToolCallMessage)
	}
	return text
}
