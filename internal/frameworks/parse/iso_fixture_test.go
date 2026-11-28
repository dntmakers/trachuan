package parse

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

func syntheticSHA(pages []string) string {
	sum := sha256.Sum256([]byte(strings.Join(pages, "\f")))
	return fmt.Sprintf("%x", sum)
}

func parseSyntheticPages(name string, pages []string, code, version string, expected *int) (*Result, error) {
	copied := make([]string, len(pages))
	for i, page := range pages {
		copied[i] = strings.ReplaceAll(page, "                 Annex A", "                 Annex A\n(informative)")
	}
	sha := syntheticSHA(copied)
	return parseISO27002Pages(copied, sha, expected, iso27002Layout{sourceSHA: sha, annexColumn: 17})
}
