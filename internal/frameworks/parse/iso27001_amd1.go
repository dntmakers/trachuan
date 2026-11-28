package parse

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

func parseISO27001Amd1Pages(pages []string, sourceSHA string, expected *int) (*Result, error) {
	if sourceSHA != "9a17aec8d8a1d79839bbe0833cc48202b65ed71e478fee0c78e6ba4b2c6dc6a7" {
		return nil, fmt.Errorf("source fingerprint mismatch")
	}
	total := 0
	for _, page := range pages {
		if len(page) > 50_000_000-total {
			return nil, fmt.Errorf("page text limit exceeded")
		}
		total += len(page)
	}
	var nodes []Node
	found := false
	for _, page := range pages {
		first, rest, _ := strings.Cut(page, "\n")
		if strings.TrimSpace(first) != "ISO/IEC 27001:2022/Amd. 1:2024(en)" {
			continue
		}
		candidate, selected, err := parseISO27001Amd1Page(rest)
		if !selected {
			continue
		}
		if found {
			return nil, fmt.Errorf("ambiguous amendment page")
		}
		found = true
		if err != nil {
			return nil, err
		}
		nodes = candidate
	}
	if !found {
		return nil, fmt.Errorf("missing amendment page")
	}
	return finish(nodes, nil, expected)
}

func parseISO27001Amd1Page(page string) ([]Node, bool, error) {
	targetRE := regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)+$`)
	nouns := []string{"sentence", "note"}
	note := regexp.MustCompile(`^NOTE(?:[ \t]|$)`)
	citations := []string{"4.1", "4.2"}
	var nodes []Node
	selected, started, footer, number, invalid := false, false, false, false, false
	stage := 0
	for page != "" {
		line, rest, _ := strings.Cut(page, "\n")
		page = rest
		text := strings.TrimSpace(line)
		if line == "4.1" {
			selected = true
		}
		if text == "" || text == "\uFEFF" {
			continue
		}
		if footer {
			if number || !isoPageNumber(text) {
				invalid = true
			}
			number = true
			continue
		}
		if text == "© ISO/IEC 2024 – All rights reserved" {
			footer = true
			continue
		}
		if !started {
			if line == "4.1" {
				started = true
			} else {
				if targetRE.MatchString(text) {
					invalid = true
				}
				continue
			}
		}
		if stage >= 6 {
			invalid = true
			continue
		}
		idx := stage / 3
		switch stage % 3 {
		case 0:
			if line != citations[idx] {
				invalid = true
			}
		case 1:
			if !iso27001Amd1Instruction(line, nouns[idx]) {
				invalid = true
			}
		case 2:
			first, _ := utf8.DecodeRuneInString(line)
			if line == "" || unicode.IsSpace(first) || strings.ContainsAny(line, "\u0085\u2028\u2029\r\v\f") || !strings.HasSuffix(line, ".") || (idx == 1 && !note.MatchString(line)) {
				invalid = true
			}
			body := iso27001Amd1Normalize(line)
			nodes = append(nodes, Node{Citation: citations[idx], Kind: "amendment", Parent: -1, Ordinal: idx, Body: body, Status: "active"})
		}
		stage++
	}
	if !selected {
		return nil, false, nil
	}
	if invalid || stage != 6 || !footer || !number {
		return nil, true, fmt.Errorf("invalid amendment blocks or footer")
	}
	return nodes, true, nil
}

func iso27001Amd1Instruction(line, noun string) bool {
	if line == "" || line[0] == ' ' || line[0] == '\t' || strings.ContainsAny(line, "\r\v\f\u0085  ") {
		return false
	}
	words := [10]string{}
	for i := range words {
		line = strings.TrimLeft(line, " \t")
		if line == "" {
			return false
		}
		n := strings.IndexAny(line, " \t")
		if n < 0 {
			words[i] = line
			line = ""
		} else {
			words[i], line = line[:n], line[n:]
		}
	}
	return strings.Trim(line, " \t") == "" && strings.EqualFold(words[0], "add") && strings.EqualFold(words[3], noun) && strings.EqualFold(words[6], "end") && strings.HasSuffix(words[9], ":")
}

func iso27001Amd1Normalize(line string) string {
	var out strings.Builder
	out.Grow(len(line))
	pending := false
	for i := 0; i < len(line); i++ {
		if line[i] == ' ' || line[i] == '\t' {
			pending = out.Len() > 0
			continue
		}
		if pending {
			out.WriteByte(' ')
			pending = false
		}
		out.WriteByte(line[i])
	}
	return out.String()
}
