package parse

import (
	"strconv"
	"strings"

	"danny.vn/trachuan/internal/pdftext"
)

type pciLabel int

const (
	pciOther pciLabel = iota
	pciMalformed
	pciOuterRequirements
	pciOuterGuidance
	pciSubRequirements
	pciSubTesting
	pciFooterLabel
	pciCopyright
	pciDate
	pciPageNumber
	pciRoot
	pciLineBody
	pciLineGroup
	pciLineGroupPart
	pciLineHeadingPart
	pciLineRow
	pciLineCaption
	pciLineStandalone
	pciPageEndLabel
	pciLineIgnore
)

func pciMalformedLabel(line pdftext.Line) bool {
	if len(line.Words) == 0 {
		return false
	}
	first := line.Words[0]
	box, err := pciWordBox(first)
	if err != nil {
		return false
	}
	last := line.Words[len(line.Words)-1].Text
	n := len(line.Words)
	x, y := box.xmin, box.ymin
	rx := pciRound(x)
	if y >= 79 && y <= 123 && x >= 185 && x <= 205 && first.Text == "Requirements" && last == "Procedures" {
		return n != 4
	}
	if y >= 79 && y <= 123 && x >= 580 && x <= 605 && first.Text == "Guidance" {
		return n != 1
	}
	if y >= 79 && y < 530 && first.Text == "Defined" {
		if (rx == 59.8 || rx == 77.8 || rx == 78) && last == "Requirements" {
			return n != 3
		}
		if (rx == 284.8 || rx == 290.8 || (rx >= 298 && rx <= 298.3)) && last == "Procedures" {
			return n != 4
		}
	}
	if y >= 530 && x >= 71.9 && x <= 72.1 && first.Text == "Payment" && last == "v4.0.1" {
		return n != 11
	}
	if y >= 530 && x >= 71.9 && x <= 72.1 && strings.HasPrefix(first.Text, "©") && (last == "Reserved." || last == "reserved.") {
		return n != 11 && n != 12
	}
	return false
}

func pciClassifyLine(line pdftext.Line, firstLine bool) pciLabel {
	if pciMalformedLabel(line) {
		return pciMalformed
	}
	if len(line.Words) == 0 {
		return pciOther
	}
	first := line.Words[0]
	box, err := pciWordBox(first)
	if err != nil {
		return pciOther
	}
	x, y := box.xmin, box.ymin
	last := line.Words[len(line.Words)-1].Text
	n := len(line.Words)
	if y >= 79 && y <= 123 && x >= 185 && x <= 205 && n == 4 && first.Text == "Requirements" && last == "Procedures" {
		return pciOuterRequirements
	}
	if y >= 79 && y <= 123 && x >= 580 && x <= 605 && n == 1 && first.Text == "Guidance" {
		return pciOuterGuidance
	}
	if y >= 79 && y < 530 && first.Text == "Defined" {
		rx := pciRound(x)
		if (rx == 59.8 || rx == 77.8 || rx == 78) && n == 3 && last == "Requirements" {
			return pciSubRequirements
		}
		if (rx == 284.8 || rx == 290.8 || (rx >= 298 && rx <= 298.3)) && n == 4 && last == "Procedures" {
			return pciSubTesting
		}
	}
	if y >= 530 && x >= 71.9 && x <= 72.1 && n == 11 && first.Text == "Payment" && last == "v4.0.1" {
		return pciFooterLabel
	}
	if y >= 530 && x >= 71.9 && x <= 72.1 && (last == "Reserved." || last == "reserved.") {
		runes := []rune(first.Text)
		if n == 12 && first.Text == "©" {
			return pciCopyright
		}
		if n == 11 && len(runes) == 5 && runes[0] == '©' {
			year, err := strconv.Atoi(string(runes[1:]))
			if err == nil && year >= 1000 {
				return pciCopyright
			}
		}
	}
	if y >= 530 && x >= 600 && n == 2 {
		value, err := strconv.Atoi(last)
		if first.Text == "Page" && err == nil && value > 0 && strconv.Itoa(value) == last {
			return pciPageNumber
		}
		if first.Text == "June" && len(last) == 4 && err == nil && value >= 1000 && value <= 9999 {
			return pciDate
		}
	}
	if firstLine && x >= 71.9 && x <= 72.1 && y >= 71 && y <= 110 && n >= 2 && (first.Text == "Requirement" || first.Text == "Appendix") && strings.HasSuffix(line.Words[1].Text, ":") {
		return pciRoot
	}
	return pciOther
}
