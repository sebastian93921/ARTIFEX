package locale

// zhCatalog holds Traditional Chinese translations keyed by the English
// template. Missing keys fall back to English (see Lookup).
var zhCatalog = map[string]string{}

// RegisterZH adds (or overrides) a Traditional Chinese translation for key.
// Called from zh_*.go init() functions; keys must match the English template
// byte-for-byte and keep fmt verbs in the same order.
func RegisterZH(key, zh string) {
	zhCatalog[key] = zh
}
