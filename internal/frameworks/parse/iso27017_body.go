package parse

import (
	"fmt"
	"strings"
)

type iso27017Body struct {
	annex                        *bool
	inTable, annexNode           bool
	child, control, hasStatement bool
}

func iso27017Words(text string, limit int) ([]string, bool) {
	words := make([]string, 0, limit)
	for word := range strings.FieldsSeq(text) {
		if len(words) == limit {
			return nil, false
		}
		words = append(words, word)
	}
	return words, true
}

func iso27017Paired(line isoLine) bool {
	if isoFlush(line) {
		return false
	}
	separator := strings.Index(line.text, "  ")
	if separator < 0 {
		return false
	}
	rightColumn := strings.TrimLeft(line.text[separator:], " ")
	if strings.Contains(rightColumn, "  ") {
		return false
	}
	left, leftOK := iso27017Words(line.text[:separator], 3)
	right, rightOK := iso27017Words(rightColumn, 3)
	return leftOK && rightOK && len(left) == 3 && len(right) == 3 && left[2] == "customer" && right[2] == "provider"
}

func iso27017Qualified(line isoLine, first, second string) bool {
	if !isoFlush(line) {
		return false
	}
	words, ok := iso27017Words(line.text, 5)
	return ok && len(words) == 5 && words[0] == first && words[1] == second && words[4] == "services"
}

func (b *iso27017Body) finish() error {
	if b.child && (!b.control || !b.hasStatement) {
		return fmt.Errorf("missing control statement")
	}
	return nil
}

func (b *iso27017Body) classify(line isoLine, next []isoLine, h isoHeading, heading bool) (isoLineAction, error) {
	if heading {
		if err := b.finish(); err != nil {
			return isoDrop, err
		}
		b.inTable = false
		b.child, b.control, b.hasStatement = false, false, false
		if *b.annex {
			b.annexNode = true
			b.child = strings.Count(h.citation, ".") == 3
		}
		return isoKeep, nil
	}
	if *b.annex && !b.annexNode {
		return isoDrop, nil
	}
	other := iso27017Qualified(line, "Other", "information")
	guidance := iso27017Qualified(line, "Implementation", "guidance")
	if (other || guidance) && b.child && !b.hasStatement {
		return isoDrop, fmt.Errorf("missing control statement")
	}
	if b.inTable {
		if other {
			b.inTable = false
			return isoKeepLabel, nil
		}
		if isoFlush(line) {
			return isoDrop, fmt.Errorf("invalid guidance table")
		}
		return isoDrop, nil
	}
	if guidance {
		if len(next) == 0 || !iso27017Paired(next[0]) {
			return isoDrop, fmt.Errorf("invalid guidance table header")
		}
		b.inTable = true
		return isoDrop, nil
	}
	if isoFlush(line) && line.text == "Control" {
		if b.child {
			b.control = true
		}
		return isoKeepLabel, nil
	}
	if b.child && b.control {
		b.hasStatement = true
	}
	if other {
		return isoKeepLabel, nil
	}
	return isoKeep, nil
}
