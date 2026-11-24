package platform

import (
	"strings"
	"unicode"
)

// ShareName keeps [A-Za-z0-9._-] and CJK letters, replaces the rest with "_", and caps the name at 80 characters.
// Shared by notify.Share and the web upload route (moved here from internal/notify).
func ShareName(base string) string {
	var b strings.Builder
	n := 0
	for _, r := range base {
		if n == 80 {
			break
		}
		switch {
		case r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-'):
			b.WriteRune(r)
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		n++
	}
	return b.String()
}
