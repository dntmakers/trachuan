package lawkey

import (
	"strings"
	"unicode"
)

func PlainAnchor(anchor string) bool {
	if strings.ContainsFunc(anchor, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return false
	}
	for _, component := range strings.Split(anchor, "/") {
		kind, token, found := strings.Cut(component, "-")
		if !found || token == "" || strings.ToLower(component) != component {
			return false
		}
		for _, r := range kind {
			if r < 'a' || r > 'z' {
				return false
			}
		}
		if kind == "" {
			return false
		}
	}
	return true
}
