package parse

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

func cobitFocusColumn(line string) int {
	matches := regexp.MustCompile(`Focus[ \t]+Area:`).FindAllStringIndex(line, -1)
	if len(matches) != 1 {
		return -1
	}
	m := matches[0]
	if m[0] < 2 || line[m[0]-2:m[0]] != "  " || m[1] >= len(line) || !strings.HasPrefix(line[m[1]:], " ") || strings.TrimSpace(line[m[1]:]) == "" {
		return -1
	}
	return utf8.RuneCountInString(line[:m[0]])
}
func (r cobitRow) footer() bool {
	if len(r.words) != 1 || r.y < 742 || r.y > 744 {
		return false
	}
	w := r.words[0]
	if !(w.x >= 36 && w.x <= 60 || w.x >= 550 && w.x <= 565) || w.text == "" {
		return false
	}
	for _, c := range w.text {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func cobitPurposeBoundary(line string) bool {
	line = strings.TrimRight(line, " ")
	fields := strings.Fields(line)
	return strings.HasPrefix(line, "The ") && utf8.RuneCountInString(line) == 101 && len(fields) == 15 && fields[len(fields)-1] == "goals:"
}
func cobitDomainOnly(line string) bool {
	switch strings.TrimSpace(line) {
	case "EDM", "APO", "BAI", "DSS", "MEA":
		return true
	}
	return false
}
func (r cobitRow) component() int {
	var boxes []cobitBox
	for _, w := range r.words {
		if w.x >= 39 && w.x < 590 {
			boxes = append(boxes, w)
		}
	}
	if len(boxes) == 0 || boxes[0].x > 60 {
		return 0
	}
	first, last := boxes[0].text, boxes[len(boxes)-1].text
	switch {
	case len(boxes) == 3 && first == "A." && last == "Process":
		return 1
	case len(boxes) == 4 && first == "A." && last == "(cont.)":
		return 2
	case len(boxes) == 4 && first == "B." && last == "Structures":
		return 3
	case len(boxes) == 5 && first == "B." && last == "(cont.)":
		return 4
	}
	return 0
}
func (r cobitRow) practiceHeader() (float64, float64, bool, error) {
	var left, right []cobitBox
	for _, w := range r.words {
		if w.x >= 39 && w.x < 300 {
			left = append(left, w)
		}
		if w.x >= 300 && w.x < 590 {
			right = append(right, w)
		}
	}
	if len(left) == 0 || left[0].x > 60 || (left[0].text != "Governance" && left[0].text != "Management") {
		return 0, 0, false, nil
	}
	if len(left) < 2 || left[len(left)-1].text != "Practice" {
		return 0, 0, false, nil
	}
	if len(left) != 2 || len(right) != 2 || right[0].x > 320 || right[0].text != "Example" || right[1].text != "Metrics" {
		return 0, 0, false, fmt.Errorf("invalid practice header pair")
	}
	return left[0].x, right[0].x, true, nil
}
func (r cobitRow) activity() bool {
	var left, right []cobitBox
	for _, w := range r.words {
		if w.x >= 39 && w.x < 496 {
			left = append(left, w)
		}
		if w.x >= 496 && w.x < 590 {
			right = append(right, w)
		}
	}
	return len(left) == 1 && left[0].x <= 60 && left[0].text == "Activities" && len(right) == 2 && right[0].x <= 516 && right[0].text == "Capability" && right[1].text == "Level"
}
