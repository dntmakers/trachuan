package parse

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type isoLine struct {
	raw, text  string
	page, line int
}

type isoLineAction uint8

const (
	isoKeep isoLineAction = iota
	isoDrop
	isoKeepBreak
)

type isoHeading struct {
	citation, title, kind string
	skip                  int
	table, direct         bool
}
type isoSection struct {
	label    string
	required bool
}
type isoProfile struct {
	header, footer string
	lookahead      int
	start          func(isoLine, []isoLine) bool
	stop           func(isoLine, []isoLine) bool
	heading        func(isoLine, []isoLine) (isoHeading, bool)
	parent         func(isoHeading, int, string, func(string) (int, bool)) (int, error)
	sections       []isoSection
	tableEnd       string
	attribute      func(isoLine) bool
	bodyLine       func(isoLine) isoLineAction
}

func isoFlush(line isoLine) bool {
	r, _ := utf8.DecodeRuneInString(line.raw)
	return r != utf8.RuneError && strings.HasPrefix(line.raw, line.text)
}

type isoBody struct {
	text  strings.Builder
	last  byte
	lines int
	table bool
}

func (b *isoBody) add(source isoLine, action isoLineAction) {
	line := strings.Join(strings.Fields(source.text), " ")
	row := action == isoKeepBreak
	if line == "" {
		return
	}
	marker := strings.HasPrefix(line, "—") || strings.HasPrefix(line, "•")
	if i := strings.IndexByte(line, ')'); i > 0 && i <= 3 {
		prefix := line[:i]
		marker = marker || (len(prefix) == 1 && prefix[0] >= 'a' && prefix[0] <= 'z')
		numeric := true
		for _, c := range prefix {
			if c < '0' || c > '9' {
				numeric = false
			}
		}
		marker = marker || numeric
	}
	if b.lines > 0 {
		if row || b.table || marker || b.last == '.' || b.last == ':' || b.last == ';' {
			b.text.WriteByte('\n')
		} else {
			b.text.WriteByte(' ')
		}
	}
	b.text.WriteString(line)
	b.last = line[len(line)-1]
	b.table = row
	b.lines++
}

func walkISO(pages []string, profile isoProfile) ([]Node, error) {
	reader := isoReader{pages: pages, header: profile.header, footer: profile.footer}
	var queue []isoLine
	fill := func() {
		for len(queue) < profile.lookahead+1 {
			line, ok := reader.next()
			if !ok {
				break
			}
			queue = append(queue, line)
		}
	}
	var nodes []Node
	var body strings.Builder
	var text isoBody
	active := -1
	root, rootCitation := -1, ""
	citations := make(map[string]int)
	lookup := func(citation string) (int, bool) {
		norm := strings.ToUpper(strings.ReplaceAll(citation, " ", ""))
		index, ok := citations[norm]
		return index, ok
	}
	section := -1
	seen := make([]bool, len(profile.sections))
	started, ended, table, direct := false, false, false, false
	appendBody := func(line isoLine) error {
		action := isoKeep
		if profile.bodyLine != nil {
			action = profile.bodyLine(line)
		}
		if action == isoDrop {
			return nil
		}
		if action != isoKeep && action != isoKeepBreak {
			return fmt.Errorf("invalid body line action")
		}
		text.add(line, action)
		return nil
	}
	flushSection := func() {
		if section < 0 {
			return
		}
		if body.Len() > 0 {
			body.WriteString("\n\n")
		}
		body.WriteString(profile.sections[section].label)
		if text.lines > 0 {
			body.WriteByte('\n')
			body.WriteString(text.text.String())
			seen[section] = true
		}
		text = isoBody{}
	}
	checkSection := func() error {
		if section >= 0 && text.lines == 0 {
			if profile.sections[section].required {
				return fmt.Errorf("missing required section")
			}
			return fmt.Errorf("empty section")
		}
		return nil
	}
	closeNode := func() error {
		if active < 0 {
			return nil
		}
		if direct {
			body.WriteString(text.text.String())
			text = isoBody{}
		} else {
			if err := checkSection(); err != nil {
				return err
			}
			flushSection()
		}
		if !direct {
			for i, s := range profile.sections {
				if s.required && !seen[i] {
					return fmt.Errorf("missing required section")
				}
			}
		}
		nodes[active].Body = body.String()
		body = strings.Builder{}
		section = -1
		seen = make([]bool, len(profile.sections))
		return nil
	}
	for {
		fill()
		if len(queue) == 0 {
			break
		}
		line := queue[0]
		next := queue[1:]
		if !started {
			if !profile.start(line, next) {
				queue = queue[1:]
				continue
			}
			started = true
		}
		if profile.stop(line, next) {
			ended = true
			break
		}
		heading, ok := profile.heading(line, next)
		if ok {
			norm := strings.ToUpper(strings.ReplaceAll(heading.citation, " ", ""))
			if _, exists := citations[norm]; exists {
				return nil, fmt.Errorf("invalid or duplicate citations")
			}
			if err := closeNode(); err != nil {
				return nil, err
			}
			parent, err := profile.parent(heading, root, rootCitation, lookup)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, Node{Citation: heading.citation, Kind: heading.kind, Parent: parent, Ordinal: len(nodes), PublisherTitle: heading.title, Status: "active"})
			active = len(nodes) - 1
			citations[norm] = active
			if parent == -1 {
				root, rootCitation = active, heading.citation
			}
			table, direct = heading.table, heading.direct
			if heading.skip > len(next) {
				return nil, fmt.Errorf("missing heading continuation")
			}
			queue = queue[1+heading.skip:]
			continue
		}
		if active >= 0 {
			if table {
				if isoFlush(line) && line.text == profile.tableEnd {
					table = false
					for i, candidate := range profile.sections {
						if candidate.label == line.text {
							section = i
							break
						}
					}
					if section < 0 {
						return nil, fmt.Errorf("unknown table section")
					}
				} else {
					if profile.attribute != nil && profile.attribute(line) {
						return nil, fmt.Errorf("invalid attribute table")
					}
					for _, s := range profile.sections {
						if line.text == s.label {
							return nil, fmt.Errorf("invalid attribute table")
						}
					}
				}
			} else if direct {
				if err := appendBody(line); err != nil {
					return nil, err
				}
			} else {
				sectionIndex := -1
				if isoFlush(line) {
					for i, s := range profile.sections {
						if line.text == s.label {
							sectionIndex = i
							break
						}
					}
				}
				if sectionIndex >= 0 {
					if sectionIndex <= section {
						return nil, fmt.Errorf("invalid section order")
					}
					if err := checkSection(); err != nil {
						return nil, err
					}
					flushSection()
					section = sectionIndex
				} else if section >= 0 {
					if err := appendBody(line); err != nil {
						return nil, err
					}
				}
			}
		}
		queue = queue[1:]
	}
	if !started || !ended {
		return nil, fmt.Errorf("missing required region")
	}
	if table {
		return nil, fmt.Errorf("missing control section")
	}
	if err := closeNode(); err != nil {
		return nil, err
	}
	return nodes, nil
}
