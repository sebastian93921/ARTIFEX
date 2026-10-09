package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/sebastian93921/artifex/locale"
)

// Hashes identify exact upstream stock templates without storing or rewriting
// arbitrary historical/user templates. Any edited byte prevents this match.
var legacyPromptDefaults = map[string]string{
	"0afba34ad25d8dfe87bac7596dcaeb59040049ce5c412bd62c04f5881ad5dcce": plannerWrapUpDefault,
	"12bb69ee0886c4fb9beb936d882021a91104860f1da69a5e3eda3d9e345cd399": mainAgentDefaultTmpl,
	"14e163f4a44da9fee20a214048a607adc3659b2025275569eb512532ca8c15d5": plannerDefaultTmpl,
	"2613bf7d5d9fdc2f549667355eb747d92f53de2be3756a57361732aed8fd7ebe": settleWrapUpPrompt,
	"52333c82468d757ce3818c8c88379f9d1e7488d9bc388d116abc3423fe9529fa": mainAgentWrapUpDefault,
	"72a7a0159443b41dde29dfba8a3de6a2c8a15b273935495a7b88561fa708aa68": goalsDefaultTmpl,
	"80fbb1c610dd57e266c20b9145793339eee540b809f6db8f5336261b45879f08": workerDefaultTmpl,
	"8ba98208f6c5f2ccb103c88ef13239c4264733400c0e3dfec96d3a3572d78d39": ReporterDefaultPrompt,
	"9d0b47458ae54a85cad722d7078eacbe40a11e40e6e7aef774bd8610a61a08b1": genericWrapUpDefault,
	"a179c990bff1ab4e0a6f326945a1c44036ef4c8a54c7c5b635965f6cefdc9f0a": DefaultAssistantPrompt,
	"a54bd5713b1c121d0f2ff3a72e0304820b1368c4b4b8159d5d651895663ac652": pentestDefaultTmpl,
	"b2a71b82243607337b19f0642317fed88c144d536412d80eaaf90807f2905a1d": autoDefaultTmpl,
	"bbefbdf2059d09703a0c6c665d4b4734f99c4df270974bb10c795d987b672343": workerTaskTimeoutDefault,
	"d002b51c2a5c42f8197714f5b86b11539f85c3451a19e87472e8483ed0adfd76": plannerTaskTimeoutDefault,
	"d08326aa9fbd9a1367e716cf7ef46533e9096e9a56493f77d364efbf696f7fe0": RetesterDefaultPrompt,
}

// CanonicalBuiltinPrompt returns canonical English only for an exact stock
// English/Korean/legacy template. All other text is returned byte-for-byte.
func CanonicalBuiltinPrompt(text string) string {
	for _, builtin := range []string{goalsDefaultTmpl, plannerDefaultTmpl, workerDefaultTmpl, mainAgentDefaultTmpl, autoDefaultTmpl, pentestDefaultTmpl, DefaultAssistantPrompt, ReporterDefaultPrompt, RetesterDefaultPrompt, settleWrapUpPrompt, plannerWrapUpDefault, mainAgentWrapUpDefault, genericWrapUpDefault, workerTaskTimeoutDefault, plannerTaskTimeoutDefault} {
		if text == builtin {
			return builtin
		}
	}
	digest := sha256.Sum256([]byte(text))
	if builtin, ok := legacyPromptDefaults[hex.EncodeToString(digest[:])]; ok {
		return builtin
	}
	return text
}

func BuiltinPromptText(text string, lang locale.Lang) string {
	canonical := CanonicalBuiltinPrompt(text)
	for _, builtin := range []string{goalsDefaultTmpl, plannerDefaultTmpl, workerDefaultTmpl, mainAgentDefaultTmpl, autoDefaultTmpl, pentestDefaultTmpl, DefaultAssistantPrompt, ReporterDefaultPrompt, RetesterDefaultPrompt, settleWrapUpPrompt, plannerWrapUpDefault, mainAgentWrapUpDefault, genericWrapUpDefault, workerTaskTimeoutDefault, plannerTaskTimeoutDefault} {
		if canonical == builtin {
			return locale.Text(lang, builtin)
		}
	}
	return text
}
