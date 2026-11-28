package parse

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type isoLine struct {
	raw, text                 string
	page, line                int
	blankBefore, firstContent bool
}

type isoLineAction uint8

const (
	isoKeep isoLineAction = iota
	isoDrop
	isoKeepBreak
	isoTitle
	isoKeepLabel
)

type isoHeading struct {
	citation, title, kind string
	skip, titleColumn     int
	table, direct         bool
}
type isoSection struct {
	label    string
	required bool
}
type isoProfile struct {
	header, footer string
	headerSuffix   bool
	lookahead      int
	start          func(isoLine, []isoLine) bool
	stop           func(isoLine, []isoLine) bool
	heading        func(isoLine, []isoLine) (isoHeading, bool)
	parent         func(isoHeading, int, string, func(string) (int, bool)) (int, error)
	sections       []isoSection
	tableEnd       string
	attribute      func(isoLine) bool
	bodyLine       func(isoLine) isoLineAction
	classify       func(isoLine, []isoLine, isoHeading, bool) (isoLineAction, error)
	joinRules      func(isoHeading) isoJoinRules
	validateNode   func(Node) error
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
	label bool
	page  int
}

func walkISO(pages []string, profile isoProfile) ([]Node, error) {
	reader := isoReader{pages: pages, header: profile.header, footer: profile.footer, headerSuffix: profile.headerSuffix}
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
	var body, title strings.Builder
	var text isoBody
	var current isoHeading
	var rules isoJoinRules
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
	appendBody := func(line isoLine, next []isoLine) error {
		action := isoKeep
		if profile.bodyLine != nil {
			action = profile.bodyLine(line)
		}
		if profile.classify != nil {
			var err error
			action, err = profile.classify(line, next, current, false)
			if err != nil {
				return err
			}
		}
		if action == isoDrop {
			return nil
		}
		if action == isoTitle {
			if title.Len() > 0 {
				title.WriteByte(' ')
			}
			title.WriteString(strings.Join(strings.Fields(line.text), " "))
			return nil
		}
		if action != isoKeep && action != isoKeepBreak && action != isoKeepLabel {
			return fmt.Errorf("invalid body line action")
		}
		text.addJoined(line, action, rules)
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
		nodes[active].PublisherTitle = title.String()
		if profile.validateNode != nil {
			if err := profile.validateNode(nodes[active]); err != nil {
				return err
			}
		}
		body = strings.Builder{}
		title = strings.Builder{}
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
			if profile.classify != nil {
				action, err := profile.classify(line, next, heading, true)
				if err != nil {
					return nil, err
				}
				if action != isoKeep {
					return nil, fmt.Errorf("invalid heading line action")
				}
			}
			parent, err := profile.parent(heading, root, rootCitation, lookup)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, Node{Citation: heading.citation, Kind: heading.kind, Parent: parent, Ordinal: len(nodes), PublisherTitle: heading.title, Status: "active"})
			active = len(nodes) - 1
			current = heading
			rules = isoDefaultJoinRules()
			if profile.joinRules != nil {
				rules = profile.joinRules(heading)
			}
			title.WriteString(heading.title)
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
				if err := appendBody(line, next); err != nil {
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
					if err := appendBody(line, next); err != nil {
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
