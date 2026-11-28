package parse

import (
	"fmt"
	"strings"
)

func (r cobitRow) cellText(left, right float64, idIndex int) (string, error) {
	var values []string
	for i, w := range r.words {
		if w.x < left-1 || w.x >= right-4 {
			continue
		}
		if w.xmax > right-4 {
			return "", fmt.Errorf("word crosses cell boundary")
		}
		if strings.ContainsRune(w.text, '�') {
			return "", fmt.Errorf("invalid retained character")
		}
		if idIndex >= 0 && i <= idIndex {
			continue
		}
		values = append(values, w.text)
	}
	return strings.Join(values, " "), nil
}
