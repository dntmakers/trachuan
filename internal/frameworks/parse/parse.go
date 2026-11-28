package parse

import (
	"fmt"
	"strings"

	"danny.vn/trachuan/internal/pdftext"
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
	return Table(nil).ParseCatalog(name, data, code, version, expected)
}

type PDFInputs struct {
	Layout []string
	BBox   []pdftext.Page
}

type PDFParser struct {
	Revision string
	Legacy   bool
	Catalog  func([]byte, string, string, *int) (*Result, error)
	Role     string
	Format   string
	Layout   bool
	BBox     bool
	Parse    func(PDFInputs, string, *int) (*Result, error)
}

type Table map[string]PDFParser

func Default() Table {
	return Table{
		"nist-oscal-800-53": {Revision: "1", Legacy: true, Role: "catalog", Catalog: func(data []byte, code, version string, expected *int) (*Result, error) {
			nodes, err := parseOSCAL(data, code, version)
			return finish(nodes, err, expected)
		}},
		"nist-csf-2-xlsx": {Revision: "1", Legacy: true, Role: "catalog", Catalog: func(data []byte, code, version string, expected *int) (*Result, error) {
			nodes, err := parseCSF(data, code, version)
			return finish(nodes, err, expected)
		}},
		"cis-controls-8-xlsx": {Revision: "1", Legacy: true, Role: "catalog", Catalog: func(data []byte, code, version string, expected *int) (*Result, error) {
			nodes, err := parseCIS(data)
			return finish(nodes, err, expected)
		}},
		"iso27018-2019-pdf": {Revision: "1", Role: "text", Format: "pdf", Layout: true, Parse: func(in PDFInputs, sha string, expected *int) (*Result, error) {
			return parseISO27018Pages(in.Layout, sha, expected)
		}},
		"iso27017-2015-pdf": {Revision: "1", Role: "text", Format: "pdf", Layout: true, Parse: func(in PDFInputs, sha string, expected *int) (*Result, error) {
			return parseISO27017Pages(in.Layout, sha, expected)
		}},
		"aicpa-tsc-2017-pdf": {Revision: "1", Role: "text", Format: "pdf", Layout: true, Parse: func(in PDFInputs, sha string, expected *int) (*Result, error) {
			return parseAICPAPages(in.Layout, sha, expected)
		}},
		"iso27001-2022-pdf": {Revision: "1", Role: "text", Format: "pdf", Layout: true, Parse: func(in PDFInputs, sha string, expected *int) (*Result, error) {
			return parseISO27001Pages(in.Layout, sha, expected)
		}},
		"tcvn11930-2017-pdf": {Revision: "1", Role: "text", Format: "pdf", Layout: true, Parse: func(in PDFInputs, sha string, expected *int) (*Result, error) {
			return parseTCVN11930Pages(in.Layout, sha, expected)
		}},
		"iso27002-2022-pdf": {Revision: "1", Role: "text", Format: "pdf", Layout: true, Parse: func(in PDFInputs, sha string, expected *int) (*Result, error) {
			return parseISO27002Pages(in.Layout, sha, expected, iso27002Layout{sourceSHA: "1da41b305cce13c7d9150e8ceb520eb13d082d8a80080f9768a5dc9f2900b7e2", annexColumn: 49, tables: []isoTableRange{{page: 21, first: 31, last: 35}}})
		}},
		"csa-ccm-4-1-xlsx": {Revision: "2", Role: "catalog", Format: "xlsx", Catalog: func(data []byte, _, _ string, expected *int) (*Result, error) {
			nodes, err := parseCSA(data)
			return finish(nodes, err, expected)
		}},
	}
}

func (t Table) Clone() Table {
	if t == nil {
		return Default()
	}
	copy := make(Table, len(t))
	for name, entry := range t {
		copy[name] = entry
	}
	return copy
}

func (p PDFParser) Validate() error {
	if p.Revision == "" || (p.Parse == nil) == (p.Catalog == nil) {
		return fmt.Errorf("invalid parser configuration")
	}
	if p.Parse != nil {
		if p.Legacy || p.Role != "text" || p.Format != "pdf" || (!p.Layout && !p.BBox) {
			return fmt.Errorf("invalid parser configuration")
		}
	} else if p.Layout || p.BBox || p.Role != "catalog" || (p.Legacy && p.Format != "") || (!p.Legacy && p.Format == "") {
		return fmt.Errorf("invalid parser configuration")
	}
	return nil
}

func (t Table) Validate() error {
	if t == nil {
		t = Default()
	}
	for name, entry := range t {
		if name == "" {
			return fmt.Errorf("invalid parser configuration")
		}
		if err := entry.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (t Table) Requirements(name string) (PDFParser, bool) {
	if t == nil {
		t = Default()
	}
	p, ok := t[name]
	if ok && p.Validate() != nil {
		return PDFParser{}, false
	}
	return p, ok
}

func (t Table) NeedsPDF(name string) bool {
	p, ok := t.Requirements(name)
	return ok && p.Parse != nil
}

func (t Table) ParseCatalog(name string, data []byte, code, version string, expected *int) (*Result, error) {
	p, ok := t.Requirements(name)
	if !ok || p.Catalog == nil {
		return nil, fmt.Errorf("unknown parser")
	}
	return p.Catalog(data, code, version, expected)
}

func ValidateCount(result *Result, expected *int) error {
	if expected == nil {
		return fmt.Errorf("expected control count unset")
	}
	if *expected <= 0 || result == nil || len(result.Nodes) != *expected {
		return fmt.Errorf("control count mismatch")
	}
	return nil
}

func (t Table) ParsePDF(name string, inputs PDFInputs, sourceSHA string, expected *int) (*Result, error) {
	p, ok := t.Requirements(name)
	if !ok || p.Parse == nil {
		return nil, fmt.Errorf("unknown parser")
	}
	return p.Parse(inputs, sourceSHA, expected)
}

func ParsePages(name string, pages []string, code, version, sourceSHA string, expected *int) (*Result, error) {
	return Table(nil).ParsePDF(name, PDFInputs{Layout: pages}, sourceSHA, expected)
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
