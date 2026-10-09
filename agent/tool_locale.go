package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/sebastian93921/artifex/locale"
	actool "github.com/Autumn-27/norma/tool"
	"strconv"
	"sync"
)

var builtinTranslations struct {
	sync.Once
	entries map[string][2]actool.CoreTool
}

// localizeBuiltinTools translates only matching bundled defaults, including exact
// historical stock metadata. Edited prose/defaults and tool behavior are preserved.
func localizeBuiltinTools(tools []actool.CoreTool, lang locale.Lang) []actool.CoreTool {
	builtinTranslations.Do(func() {
		builtinTranslations.entries = map[string][2]actool.CoreTool{}
		for _, t := range NewToolSet(nil, "", locale.En).AllDomainTools() {
			builtinTranslations.entries[t.Name()] = [2]actool.CoreTool{t, nil}
		}
	})
	out := append([]actool.CoreTool(nil), tools...)
	for i, t := range out {
		if p, ok := builtinTranslations.entries[t.Name()]; ok && p[0] != nil && p[1] != nil {
			out[i] = TranslateToolMetadata(t, p[1], p[0])
		}
	}
	return out
}

// TranslateToolMetadata renders an API/runtime copy from the corresponding
// bundled source/target language tools. Only exact default description fields
// are eligible. JSON keys, enums, defaults, raw results, and handlers are untouched.
func TranslateToolMetadata(current, source, target actool.CoreTool) actool.CoreTool {
	if current == nil || source == nil || target == nil {
		return current
	}
	legacy := legacyToolMetadataHashes[current.Name()]
	desc := current.Description()
	if desc == source.Description() || matchesMetadataHash(desc, legacy["description"]) {
		desc = target.Description()
	}
	schema := cloneToolSchema(current.InputSchema())
	if schema == nil {
		return current
	}
	translateSchemaDescriptions(schema, cloneToolSchema(source.InputSchema()), cloneToolSchema(target.InputSchema()), "schema", legacy)
	return &localizedTool{CoreTool: current, desc: desc, schema: schema}
}
func cloneToolSchema(schema map[string]any) map[string]any {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}
func matchesMetadataHash(text, expected string) bool {
	if expected == "" {
		return false
	}
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:]) == expected
}

type localizedTool struct {
	actool.CoreTool
	desc   string
	schema map[string]any
}

func (t *localizedTool) Description() string         { return t.desc }
func (t *localizedTool) InputSchema() map[string]any { return t.schema }

func translateSchemaDescriptions(current, source, target map[string]any, path string, legacy map[string]string) {
	for key, value := range current {
		next := path + "/" + key
		if key == "description" {
			if text, ok := value.(string); ok && (text == source[key] || matchesMetadataHash(text, legacy[next])) {
				if translated, ok := target[key].(string); ok {
					current[key] = translated
				}
			}
			continue
		}
		switch child := value.(type) {
		case map[string]any:
			src, _ := source[key].(map[string]any)
			dst, _ := target[key].(map[string]any)
			if src != nil && dst != nil {
				translateSchemaDescriptions(child, src, dst, next, legacy)
			}
		case []any:
			src, _ := source[key].([]any)
			dst, _ := target[key].([]any)
			for i, item := range child {
				if i >= len(src) || i >= len(dst) {
					continue
				}
				m, ok := item.(map[string]any)
				a, aok := src[i].(map[string]any)
				b, bok := dst[i].(map[string]any)
				if ok && aok && bok {
					translateSchemaDescriptions(m, a, b, next+"/"+strconv.Itoa(i), legacy)
				}
			}
		}
	}
}
