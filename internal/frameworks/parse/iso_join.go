package parse

import "strings"

type isoJoinRules struct {
	markers            []string
	letterItems        bool
	digitItems         bool
	paragraphs         []string
	label              func(isoLine) bool
	blankParagraph     bool
	pageParagraph      bool
	punctuationBreak   bool
	labelParagraph     bool
	labelParagraphEach bool
}

func isoDefaultJoinRules() isoJoinRules {
	return isoJoinRules{markers: []string{"—", "•"}, letterItems: true, digitItems: true, punctuationBreak: true}
}

func isoMarker(line string, rules isoJoinRules) bool {
	for _, marker := range rules.markers {
		if strings.HasPrefix(line, marker) {
			return true
		}
	}
	if !rules.letterItems && !rules.digitItems {
		return false
	}
	i := strings.IndexByte(line, ')')
	if i < 1 || i > 3 {
		return false
	}
	prefix := line[:i]
	if len(prefix) == 1 && prefix[0] >= 'a' && prefix[0] <= 'z' {
		return rules.letterItems
	}
	if !rules.digitItems {
		return false
	}
	for _, c := range prefix {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isoParagraph(line string, rules isoJoinRules) bool {
	for _, marker := range rules.paragraphs {
		if line == marker || strings.HasPrefix(line, marker+" ") {
			return true
		}
	}
	return false
}

func (b *isoBody) addJoined(source isoLine, action isoLineAction, rules isoJoinRules) {
	line := strings.Join(strings.Fields(source.text), " ")
	if line == "" {
		return
	}
	marker := isoMarker(line, rules)
	label := action == isoKeepLabel || (rules.label != nil && rules.label(source))
	pageBreak := source.page != b.page
	terminal := b.last == '.' || b.last == ':' || b.last == ';'
	paragraph := isoParagraph(line, rules) || (rules.blankParagraph && source.blankBefore && !source.firstContent) || (rules.pageParagraph && pageBreak && !marker && terminal)
	if b.lines > 0 {
		switch {
		case paragraph || (label && (rules.labelParagraphEach || (!b.label && rules.labelParagraph))):
			b.text.WriteString("\n\n")
		case label || b.label || marker || action == isoKeepBreak || b.table || (rules.punctuationBreak && terminal):
			b.text.WriteByte('\n')
		default:
			b.text.WriteByte(' ')
		}
	}
	b.text.WriteString(line)
	b.last = line[len(line)-1]
	b.page = source.page
	b.table = action == isoKeepBreak
	b.label = label
	b.lines++
}
