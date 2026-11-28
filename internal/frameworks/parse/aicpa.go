package parse

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const aicpaSHA = "23a099dd1dc273b7a45fda2351f376344c33fcb2911d586036c0e0d631b8ad69"

func parseAICPAPages(pages []string, sha string, expected *int) (*Result, error) {
	if sha != aicpaSHA {
		return nil, fmt.Errorf("source fingerprint mismatch")
	}
	total := 0
	for _, page := range pages {
		if len(page) > 50_000_000-total {
			return nil, fmt.Errorf("page text limit exceeded")
		}
		total += len(page)
	}
	nodes, err := walkAICPA(pages)
	return finish(nodes, err, expected)
}

func walkAICPA(pages []string) ([]Node, error) {
	if len(pages) < 67 {
		return nil, fmt.Errorf("missing table region")
	}
	header := regexp.MustCompile(`^( *)([A-Z]{3})( {6,10})(.{43})$`)
	second := regexp.MustCompile(`^[A-Z][a-z]{2}\. #$`)
	row := regexp.MustCompile(`^(CC|PI|A|C|P)[1-9][0-9]*\.[0-9]+$`)
	rowPrefix := regexp.MustCompile(`^(CC|PI|A|C|P)[1-9][0-9]*\.[0-9]+`)
	footer := regexp.MustCompile(`^Page [0-9]+$`)
	var nodes []Node
	var body, bulletBody strings.Builder
	seen := map[string]bool{}
	active, bullet := -1, -1
	focus, blank, lead := false, false, false
	children := 0
	closeBullet := func() error {
		if bullet < 0 {
			return nil
		}
		text := bulletBody.String()
		if text == "" {
			return fmt.Errorf("empty point of focus")
		}
		nodes[bullet].Body = text
		paragraph, _, _ := strings.Cut(text, "\n")
		if title, _, ok := strings.Cut(paragraph, " — "); ok {
			title = strings.TrimSpace(title)
			if strings.HasSuffix(title, "]") {
				if i := strings.LastIndex(title, " ["); i >= 0 {
					title = strings.TrimSpace(title[:i])
				}
			}
			nodes[bullet].PublisherTitle = title
		}
		bulletBody.Reset()
		bullet = -1
		return nil
	}
	closeNode := func() error {
		if err := closeBullet(); err != nil {
			return err
		}
		if active >= 0 {
			if body.Len() == 0 {
				return fmt.Errorf("empty statement")
			}
			nodes[active].Body = body.String()
			body.Reset()
		}
		return nil
	}
	for page := 13; page < 67; page++ {
		lines := strings.Split(pages[page], "\n")
		headerLine := 0
		if page == 13 {
			headerLine = 24
		}
		if len(lines) <= headerLine+1 {
			return nil, fmt.Errorf("missing table header")
		}
		m := header.FindStringSubmatch(lines[headerLine])
		if m == nil {
			return nil, fmt.Errorf("missing table header")
		}
		ref := len(m[1])
		statement := ref + 3 + len(m[3])
		if !strings.HasPrefix(lines[headerLine+1], strings.Repeat(" ", ref)) || !second.MatchString(strings.TrimPrefix(lines[headerLine+1], strings.Repeat(" ", ref))) {
			return nil, fmt.Errorf("invalid table header")
		}
		start := headerLine + 2
		end := len(lines)
		for start < end && strings.TrimSpace(lines[start]) == "" {
			start++
		}
		for end > start && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		if end > start && footer.MatchString(strings.TrimSpace(lines[end-1])) {
			end--
		}
		for end > start && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		bulletColumn := -1
		blank = false
		for _, line := range lines[start:end] {
			text := strings.TrimSpace(line)
			if text == "" {
				if bullet >= 0 {
					blank = true
				}
				continue
			}
			col := len(line) - len(strings.TrimLeft(line, " "))
			if col < statement {
				if lead {
					return nil, fmt.Errorf("incomplete focus introduction")
				}
				if col != ref || len(line) <= statement {
					if col == ref && rowPrefix.MatchString(line[ref:]) {
						return nil, fmt.Errorf("invalid criterion row")
					}
					break
				}
				field := line[ref:statement]
				id := strings.TrimRight(field, " ")
				if !row.MatchString(id) || len(field)-len(id) < 2 || strings.TrimLeftFunc(line[statement:], unicode.IsSpace) != line[statement:] {
					if rowPrefix.MatchString(line[ref:]) {
						return nil, fmt.Errorf("invalid criterion row")
					}
					break
				}
				if err := closeNode(); err != nil {
					return nil, err
				}
				active = -1
				focus = false
				children = 0
				blank = false
				if strings.HasPrefix(id, "P") && strings.HasSuffix(id, ".0") {
					continue
				}
				if seen[id] {
					return nil, fmt.Errorf("duplicate criterion")
				}
				seen[id] = true
				nodes = append(nodes, Node{Citation: id, Kind: "criterion", Parent: -1, Ordinal: len(nodes), Status: "active"})
				active = len(nodes) - 1
				body.WriteString(strings.Join(strings.Fields(line[statement:]), " "))
				continue
			}
			if col == statement && upperAICPA(text) {
				if err := closeNode(); err != nil {
					return nil, err
				}
				active = -1
				continue
			}
			if active < 0 {
				if strings.HasPrefix(text, "•") && col >= statement+9 && col <= statement+14 {
					return nil, fmt.Errorf("orphan point of focus")
				}
				continue
			}
			if lead || (col == statement && strings.Contains(strings.ToLower(text), "points of focus")) {
				if col != statement {
					return nil, fmt.Errorf("invalid focus introduction")
				}
				if !lead {
					if err := closeBullet(); err != nil {
						return nil, err
					}
				}
				focus = true
				lead = !strings.HasSuffix(text, ":")
				blank = false
				continue
			}
			if strings.HasPrefix(text, "•") && col >= statement+9 && col <= statement+14 {
				if bulletColumn >= 0 && bulletColumn != col {
					return nil, fmt.Errorf("inconsistent bullet column")
				}
				bulletColumn = col
				if err := closeBullet(); err != nil {
					return nil, err
				}
				focus = true
				children++
				citation := fmt.Sprintf("%s-pof-%02d", nodes[active].Citation, children)
				nodes = append(nodes, Node{Citation: citation, Kind: "point-of-focus", Parent: active, Ordinal: len(nodes), Status: "active"})
				bullet = len(nodes) - 1
				bulletBody.WriteString(strings.TrimSpace(strings.TrimPrefix(text, "•")))
				blank = false
				continue
			}
			if bullet >= 0 {
				if bulletBody.Len() > 0 {
					if strings.HasPrefix(text, "•") {
						bulletBody.WriteByte('\n')
					} else if blank {
						bulletBody.WriteString("\n\n")
					} else {
						bulletBody.WriteByte(' ')
					}
				}
				bulletBody.WriteString(strings.Join(strings.Fields(text), " "))
				blank = false
				continue
			}
			if focus {
				continue
			}
			if body.Len() > 0 {
				body.WriteByte(' ')
			}
			body.WriteString(strings.Join(strings.Fields(text), " "))
		}
	}
	if lead {
		return nil, fmt.Errorf("incomplete focus introduction")
	}
	if err := closeNode(); err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("missing criteria")
	}
	return nodes, nil
}

func upperAICPA(text string) bool {
	hasLetter := false
	for _, r := range text {
		if unicode.IsLetter(r) {
			hasLetter = true
			if unicode.IsLower(r) {
				return false
			}
		}
	}
	return hasLetter
}
