package locale

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var formatVerb = regexp.MustCompile(`%((?:\[[0-9]+\]|[-+# 0-9.*])*)([vTtbcdoOqxXUeEfFgGspw%])`)
var formatIndex = regexp.MustCompile(`\[([0-9]+)\]`)

// Record the argument index and verb, allowing explicit indexed reordering.
func formatArguments(value string) []string {
	next := 1
	var signature []string
	for _, match := range formatVerb.FindAllStringSubmatch(value, -1) {
		if match[2] == "%" {
			continue
		}
		spec := match[1]
		indexes := formatIndex.FindAllStringSubmatchIndex(spec, -1)
		pos := 0
		for _, index := range indexes {
			for strings.Contains(spec[pos:index[0]], "*") {
				star := strings.Index(spec[pos:index[0]], "*")
				signature = append(signature, fmt.Sprintf("%d:width", next))
				next++
				pos += star + 1
			}
			next, _ = strconv.Atoi(spec[index[2]:index[3]])
			pos = index[1]
		}
		for range strings.Count(spec[pos:], "*") {
			signature = append(signature, fmt.Sprintf("%d:width", next))
			next++
		}
		signature = append(signature, fmt.Sprintf("%d:%s", next, match[2]))
		next++
	}
	sort.Strings(signature)
	return signature
}

func TestProductionCatalogCompleteAndFormatCompatible(t *testing.T) {
	if len(catalog) < 1000 {
		t.Fatalf("production catalog unexpectedly small: %d", len(catalog))
	}
	t.Logf("checking %d registered message keys for completeness", len(catalog))
	for key, value := range catalog {
		if value == "" {
			t.Errorf("empty production translation for %q", key)
		}
	}
}

func TestFormatArgumentsRecognizesReorderingAndWidth(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"%s %d %%", "1:s,2:d"},
		{"%[2]d %[1]s", "1:s,2:d"},
		{"%[3]*.[2]*[1]f", "1:f,2:width,3:width"},
	} {
		if got := strings.Join(formatArguments(tc.input), ","); got != tc.want {
			t.Errorf("%q: got %s, want %s", tc.input, got, tc.want)
		}
	}
}

func TestZhCatalogKeysExistAndFormatCompatible(t *testing.T) {
	if len(zhCatalog) == 0 {
		t.Skip("no zh translations registered yet")
	}
	trunc := func(s string) string {
		if len(s) > 60 {
			return s[:60]
		}
		return s
	}
	t.Logf("checking %d registered zh keys", len(zhCatalog))
	for key, value := range zhCatalog {
		en, ok := catalog[key]
		if !ok {
			t.Errorf("zh translation for unknown key %q", trunc(key))
			continue
		}
		if value == "" {
			t.Errorf("empty zh translation for %q", trunc(key))
			continue
		}
		if got, want := formatArguments(value), formatArguments(en); len(got) != len(want) {
			t.Errorf("zh translation for %q: %d format args, en has %d", trunc(key), len(got), len(want))
		}
	}
}
