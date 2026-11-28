package parse

import (
	"fmt"
	"regexp"
	"strings"

	"danny.vn/trachuan/internal/pdftext"
)

const cobitSHA = "8bbdf564a15a01a90feee58ac4a94307dd08b0ea26def4886d3864d23e6bfa01"
const cobitWrappedTitles = "|APO01.02|APO01.03|APO01.10|APO01.11|APO02.02|APO04.04|APO05.03|APO07.05|APO08.02|APO11.03|APO13.01|APO13.02|APO13.03|APO14.01|APO14.03|BAI02.01|BAI03.11|BAI03.12|BAI04.01|BAI04.05|BAI08.01|DSS02.01|DSS04.04|DSS05.07|DSS06.01|DSS06.03|MEA04.01|MEA04.06|MEA04.07|"

type cobitState uint8

const (
	cobitNoObjective cobitState = iota
	cobitObjective
	cobitProcess
	cobitActivities
	cobitAwaitPage
	cobitAwaitID
	cobitPractice
	cobitTitleContinuation
	cobitAwaitContinuation
	cobitAwaitActivity
	cobitClosed
)

func parseCOBIT(layout []string, words []pdftext.Page, replacements int, sourceSHA string, expected *int) (*Result, error) {
	if sourceSHA != cobitSHA || replacements != 1994 {
		return nil, fmt.Errorf("source fingerprint mismatch")
	}
	total := 0
	add := func(s string) bool {
		if len(s) > 50_000_000-total {
			return false
		}
		total += len(s)
		return true
	}
	for _, p := range layout {
		if !add(p) {
			return nil, fmt.Errorf("input limit exceeded")
		}
	}
	for _, p := range words {
		if !add(p.Width) || !add(p.Height) {
			return nil, fmt.Errorf("input limit exceeded")
		}
		for _, w := range p.Words {
			if !add(w.XMin) || !add(w.YMin) || !add(w.XMax) || !add(w.YMax) || !add(w.Text) {
				return nil, fmt.Errorf("input limit exceeded")
			}
		}
	}
	if len(layout) < 296 || len(words) != len(layout) {
		return nil, fmt.Errorf("missing objective region")
	}
	opener := regexp.MustCompile(`^(?i:(governance|management)[ \t]+objective): +(EDM|APO|BAI|DSS|MEA)([0-9]{2}) +(.+?) {2,}Focus[ \t]+Area: +\S`)
	nodes := []Node{}
	seen := map[string]bool{}
	domains := map[string]int{}
	objective, active := -1, -1
	state := cobitNoObjective
	processPage, processObjective := -1, -1
	var left, right, previousY float64
	var body cobitBody
	for page := 28; page < 296; page++ {
		switch state {
		case cobitProcess, cobitActivities:
			state = cobitAwaitPage
		case cobitPractice:
			state = cobitAwaitContinuation
		case cobitTitleContinuation:
			return nil, fmt.Errorf("missing title continuation")
		case cobitAwaitPage, cobitAwaitContinuation, cobitAwaitActivity, cobitAwaitID:
			return nil, fmt.Errorf("missing immediate continuation")
		}
		lines := strings.Split(layout[page], "\n")
		candidate := -1
		var match []string
		for _, index := range []int{5, 8} {
			if index >= len(lines) {
				continue
			}
			m := opener.FindStringSubmatch(lines[index])
			if m != nil {
				if candidate >= 0 {
					return nil, fmt.Errorf("ambiguous objective opener")
				}
				candidate = index
				match = m
			}
		}
		if candidate >= 0 {
			if state != cobitNoObjective && state != cobitClosed {
				return nil, fmt.Errorf("incomplete prior objective")
			}
			focus := cobitFocusColumn(lines[candidate])
			if focus < 113 || focus > 124 || candidate+1 >= len(lines) {
				return nil, fmt.Errorf("invalid objective opener")
			}
			objectiveBody, err := cobitObjectiveBody(lines, candidate+1)
			if err != nil {
				return nil, err
			}
			title := strings.TrimSpace(match[4])
			if title == "" {
				return nil, fmt.Errorf("empty objective title")
			}
			domain := strings.ToUpper(match[2])
			id := domain + match[3]
			if seen[id] {
				return nil, fmt.Errorf("duplicate objective")
			}
			seen[id] = true
			parent, ok := domains[domain]
			if !ok {
				parent = len(nodes)
				domains[domain] = parent
				nodes = append(nodes, Node{Citation: domain, Kind: "domain", Parent: -1, Ordinal: parent, Status: "active"})
			}
			objective = len(nodes)
			nodes = append(nodes, Node{Citation: id, Kind: "objective", Parent: parent, Ordinal: len(nodes), PublisherTitle: title, Body: objectiveBody, Status: "active"})
			state = cobitObjective
		}
		rows, err := cobitRows(words[page])
		if err != nil {
			return nil, err
		}
		footerSeen := false
		for _, row := range rows {
			if row.y >= 735 {
				if footerSeen || !row.footer() {
					return nil, fmt.Errorf("invalid footer")
				}
				footerSeen = true
				continue
			}
			component := row.component()
			if component == 1 {
				if state != cobitObjective {
					return nil, fmt.Errorf("invalid process start")
				}
				state = cobitProcess
				processPage = page
				processObjective = objective
				continue
			}
			if component == 2 {
				if (state != cobitAwaitPage && state != cobitAwaitContinuation) || processPage != page-1 || processObjective != objective {
					return nil, fmt.Errorf("orphan component continuation")
				}
				if state == cobitAwaitContinuation {
					state = cobitAwaitActivity
				} else {
					state = cobitProcess
				}
				processPage = page
				continue
			}
			if component == 3 || component == 4 {
				if state == cobitTitleContinuation {
					return nil, fmt.Errorf("missing title continuation")
				}
				if state == cobitPractice || state == cobitAwaitID || state == cobitAwaitContinuation || state == cobitAwaitActivity {
					return nil, fmt.Errorf("missing activity boundary")
				}
				if state == cobitProcess || state == cobitActivities || state == cobitAwaitPage {
					state = cobitClosed
				}
				continue
			}
			if state == cobitPractice || state == cobitAwaitActivity || state == cobitTitleContinuation {
				if _, _, header, err := row.practiceHeader(); err != nil || header {
					return nil, fmt.Errorf("missing activity boundary")
				}
				if row.activity() {
					if state == cobitTitleContinuation {
						return nil, fmt.Errorf("missing title continuation")
					}
					if body.text.Len() == 0 {
						return nil, fmt.Errorf("empty practice body")
					}
					nodes[active].Body = body.text.String()
					body = cobitBody{}
					active = -1
					state = cobitActivities
					continue
				}
				if state == cobitAwaitActivity {
					return nil, fmt.Errorf("missing continuation activity")
				}
				line, err := row.cellText(left, right, -1)
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(line) != "" {
					if state == cobitTitleContinuation {
						nodes[active].PublisherTitle += " " + strings.Join(strings.Fields(line), " ")
						previousY = row.y
						state = cobitPractice
						continue
					}
					if previousY > 0 && row.y-previousY > 16 {
						body.gap = true
					}
					body.add(line)
					previousY = row.y
				}
				continue
			}
			if state == cobitAwaitContinuation {
				if row.y >= 100 {
					return nil, fmt.Errorf("missing continuation header")
				}
				continue
			}
			if state == cobitAwaitPage {
				if _, _, header, err := row.practiceHeader(); err != nil || header {
					return nil, fmt.Errorf("missing page component")
				}
				continue
			}
			if state != cobitProcess && state != cobitActivities && state != cobitAwaitID {
				continue
			}
			headerLeft, headerRight, header, err := row.practiceHeader()
			if err != nil {
				return nil, err
			}
			if header {
				if state == cobitAwaitID {
					return nil, fmt.Errorf("missing practice opener")
				}
				left, right = headerLeft, headerRight
				state = cobitAwaitID
				continue
			}
			if state != cobitAwaitID {
				continue
			}
			id, idIndex, found, ok := row.practiceID(left, right)
			if !found {
				continue
			}
			if !ok {
				return nil, fmt.Errorf("invalid practice opener")
			}
			if objective < 0 || !strings.HasPrefix(id, nodes[objective].Citation+".") {
				return nil, fmt.Errorf("invalid practice parent")
			}
			if seen[id] {
				return nil, fmt.Errorf("duplicate practice")
			}
			seen[id] = true
			title, err := row.cellText(left, right, idIndex)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(title) == "" {
				return nil, fmt.Errorf("empty practice title")
			}
			active = len(nodes)
			previousY = row.y
			nodes = append(nodes, Node{Citation: id, Kind: "practice", Parent: objective, Ordinal: active, PublisherTitle: title, Status: "active"})
			state = cobitPractice
			if strings.Contains(cobitWrappedTitles, "|"+id+"|") {
				state = cobitTitleContinuation
			}
		}
	}
	if state != cobitClosed && state != cobitNoObjective {
		return nil, fmt.Errorf("incomplete practice")
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("missing objectives")
	}
	return finish(nodes, nil, expected)
}
