package parse

import (
	"regexp"
	"strings"
)

type cobitBody struct {
	text strings.Builder
	gap  bool
}

func (b *cobitBody) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		b.gap = true
		return
	}
	list := strings.HasPrefix(line, "•") || strings.HasPrefix(line, "-") || regexp.MustCompile(`^(?:[0-9]+[.)]|[A-Za-z]\))\s`).MatchString(line)
	if b.text.Len() > 0 {
		if b.gap {
			b.text.WriteString("\n\n")
		} else if list {
			b.text.WriteByte('\n')
		} else {
			b.text.WriteByte(' ')
		}
	}
	b.text.WriteString(strings.Join(strings.Fields(line), " "))
	b.gap = false
}
