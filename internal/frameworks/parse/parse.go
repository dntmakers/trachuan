package parse

import (
	"fmt"
	"strings"
)

type Mapping struct {
	TargetFramework string
	TargetVersion   string
	TargetCitation  string
	Relation        string
	Unresolved      bool
}
type Attributes struct {
	AssetType            string
	SecurityFunction     string
	ImplementationGroups []string
}
type Node struct {
	Citation        string
	CitationNorm    string
	Kind            string
	Parent          int
	Ordinal         int
	Title           string
	PublisherTitle  string
	Body            string
	Status          string
	Mappings        []Mapping
	DroppedMappings int
	Attributes      Attributes
}
type Result struct {
	Nodes            []Node
	InvalidCitations int
	Duplicates       int
}

func Parse(name string, data []byte, code, version string, expected *int) (*Result, error) {
	var nodes []Node
	var err error
	switch name {
	case "nist-oscal-800-53":
		nodes, err = parseOSCAL(data, code, version)
	case "nist-csf-2-xlsx":
		nodes, err = parseCSF(data, code, version)
	case "cis-controls-8-xlsx":
		nodes, err = parseCIS(data)
	case "csa-ccm-4-1-xlsx":
		nodes, err = parseCSA(data)
	default:
		return nil, fmt.Errorf("unknown parser")
	}
	return finish(nodes, err, expected)
}

func ParsePages(name string, pages []string, code, version, sourceSHA string, expected *int) (*Result, error) {
	if name == "aicpa-tsc-2017-pdf" {
		return parseAICPAPages(pages, sourceSHA, expected)
	}
	if name == "iso27001-2022-pdf" {
		return parseISO27001Pages(pages, sourceSHA, expected)
	}
	if name == "tcvn11930-2017-pdf" {
		return parseTCVN11930Pages(pages, sourceSHA, expected)
	}
	if name != "iso27002-2022-pdf" {
		return nil, fmt.Errorf("unknown parser")
	}
	return parseISO27002Pages(pages, sourceSHA, expected, iso27002Layout{
		sourceSHA:   "1da41b305cce13c7d9150e8ceb520eb13d082d8a80080f9768a5dc9f2900b7e2",
		annexColumn: 49,
		tables:      []isoTableRange{{page: 21, first: 31, last: 35}},
	})
}

func parseISO27002Pages(pages []string, sourceSHA string, expected *int, layout iso27002Layout) (*Result, error) {
	if sourceSHA != layout.sourceSHA {
		return nil, fmt.Errorf("source fingerprint mismatch")
	}
	const maxPageBytes = 50_000_000
	total := 0
	for _, page := range pages {
		if len(page) > maxPageBytes-total {
			return nil, fmt.Errorf("page text limit exceeded")
		}
		total += len(page)
	}
	nodes, err := parseISO27002(pages, layout)
	return finish(nodes, err, expected)
}

func finish(nodes []Node, err error, expected *int) (*Result, error) {
	if err != nil && len(nodes) == 0 {
		return nil, err
	}
	result := &Result{Nodes: nodes}
	seen := map[string]bool{}
	for i := range nodes {
		n := &nodes[i]
		n.CitationNorm = strings.ToUpper(strings.ReplaceAll(n.Citation, " ", ""))
		if !validCitation(n.Citation) {
			result.InvalidCitations++
		}
		if seen[n.CitationNorm] {
			result.Duplicates++
		}
		seen[n.CitationNorm] = true
	}
	if err != nil {
		return result, err
	}
	if err = validateResult(result, expected); err != nil {
		return result, err
	}
	return result, nil
}
func validCitation(s string) bool {
	if len(s) < 1 || len(s) > 64 || strings.HasPrefix(s, " ") || strings.HasSuffix(s, " ") || strings.Contains(s, "  ") {
		return false
	}
	for _, c := range []byte(s) {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}
func validate(nodes []Node, expected *int) error {
	r := &Result{Nodes: nodes}
	seen := map[string]bool{}
	for _, n := range nodes {
		norm := strings.ToUpper(strings.ReplaceAll(n.Citation, " ", ""))
		if !validCitation(n.Citation) {
			r.InvalidCitations++
		}
		if seen[norm] {
			r.Duplicates++
		}
		seen[norm] = true
	}
	return validateResult(r, expected)
}
func validateResult(r *Result, expected *int) error {
	if r.InvalidCitations > 0 || r.Duplicates > 0 {
		return fmt.Errorf("invalid or duplicate citations")
	}
	if expected == nil {
		return fmt.Errorf("expected control count unset")
	}
	if *expected <= 0 {
		return fmt.Errorf("invalid expected control count")
	}
	if len(r.Nodes) != *expected {
		return fmt.Errorf("control count mismatch")
	}
	return nil
}
