package retrieval

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func ValidDocumentKey(key string) bool {
	if !strings.HasPrefix(key, "vn:") || len(key) <= 3 || !utf8.ValidString(key) || strings.ContainsAny(key, "#/?\\") {
		return false
	}
	for _, r := range key {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
