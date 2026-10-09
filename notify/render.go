package notify

import (
	"github.com/sebastian93921/artifex/locale"
	"strings"
	"unicode/utf8"
)

const ellipsis = "…"

// TruncateBytes keeps valid UTF-8 within max bytes. WeCom limits Markdown to 4096 bytes, and Chinese
// characters take three bytes; arbitrary byte slicing can split a character and cause rejection or
// mojibake. Backtrack from the budget to a rune boundary using utf8.RuneStart. max<=0 disables the
// limit; append an ellipsis unless the budget cannot hold it.
func TruncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	budget := max - len(ellipsis)
	suffix := ellipsis
	if budget < 0 {
		// If max cannot fit an ellipsis, truncate without one to avoid exceeding the limit.
		budget = max
		suffix = ""
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// OneLine collapses whitespace then truncates by rune count. This keeps multiline summaries from
// breaking IM titles or tables. max<=0 disables the limit.
func OneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	return TruncateRunes(s, max)
}

// TruncateRunes limits characters rather than bytes and adds an ellipsis on truncation; max<=0
// disables the limit. WeCom counts bytes while Telegram counts characters. Using bytes for Telegram
// silently cuts Chinese capacity to about one third, so retain both helpers and choose by channel.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + ellipsis
}

// TruncateHTML first truncates by characters, then removes an unfinished trailing tag to avoid parser
// rejection or accidental attribute consumption. It does not balance tags because Telegram closes
// them; a custom balancer would need complex quote, comment, and self-closing-tag handling.
func TruncateHTML(s string, max int) string {
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	cut := TruncateRunes(s, max)
	// Remove a trailing tag fragment whose final opening angle bracket has no closing bracket.
	if lt := strings.LastIndex(cut, "<"); lt >= 0 && !strings.Contains(cut[lt:], ">") {
		cut = cut[:lt]
	}
	// Also remove incomplete entities such as &amp without a semicolon. Entity-only parsers may reject the
	// entire message; oversized digests should not be lost for this reason.
	if amp := strings.LastIndex(cut, "&"); amp >= 0 && !strings.Contains(cut[amp:], ";") {
		cut = cut[:amp]
	}
	return cut
}

// packItemCount fits complete entries so omitted findings remain pending rather than disappearing
// behind false delivery receipts. maxSize<=0 disables limits; reserve covers header/footer overhead;
// size selects bytes for WeCom/DingTalk or runes for Telegram. Render each actual entry rather than
// estimating variable content. Always keep at least one existing item, letting final truncation handle
// oversized singles so they cannot stall a batch indefinitely.
func packItemCount(items []Item, maxSize, reserve int, footer string, size func(string) int, render func(Item, int) string) int {
	if maxSize <= 0 {
		return len(items)
	}
	budget := maxSize - reserve - size(footer)
	if budget < 0 {
		budget = 0
	}
	used := 0
	for i, it := range items {
		used += size(render(it, i))
		if used > budget && i > 0 {
			return i
		}
	}
	return len(items)
}

// byteSize and runeSize name the packing units explicitly instead of obscuring them in anonymous
// closures.
func byteSize(s string) int { return len(s) }
func runeSize(s string) int { return utf8.RuneCountInString(s) }

// assetLine lists assets on one line, omitting excess entries with a total count so findings spanning
// many assets cannot overwhelm messages.
func assetLine(assets []string, limit int, langs ...locale.Lang) string {
	if len(assets) == 0 {
		return ""
	}
	if limit <= 0 || len(assets) <= limit {
		return strings.Join(assets, ", ")
	}
	return locale.Text(locale.First(langs), "%s (%d total)", strings.Join(assets[:limit], ", "), len(assets))
}

// itoa is a local integer-to-string helper for display text, avoiding repeated strconv imports.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
