package parse

import (
	"fmt"
	"strings"
)

type cobitFieldState uint8

const (
	cobitDescription cobitFieldState = iota
	cobitPurpose
)

func cobitObjectiveBody(lines []string, start int) (string, error) {
	if start >= len(lines) || strings.TrimRight(lines[start], " ") != "Description" {
		return "", fmt.Errorf("missing description")
	}
	var description, purpose cobitBody
	section := cobitDescription
	for _, line := range lines[start+1:] {
		if cobitDomainOnly(line) {
			return "", fmt.Errorf("ambiguous domain-only field line")
		}
		if strings.TrimRight(line, " ") == "Purpose" {
			if section != cobitDescription || description.text.Len() == 0 {
				return "", fmt.Errorf("invalid purpose")
			}
			section = cobitPurpose
			continue
		}
		if cobitPurposeBoundary(line) {
			if section != cobitPurpose || purpose.text.Len() == 0 {
				return "", fmt.Errorf("missing purpose")
			}
			return "Description\n" + description.text.String() + "\n\nPurpose\n" + purpose.text.String(), nil
		}
		if section == cobitDescription {
			description.add(line)
		} else {
			purpose.add(line)
		}
	}
	return "", fmt.Errorf("missing objective boundary")
}
