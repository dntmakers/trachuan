package relate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"danny.vn/trachuan/internal/frameworks/businesskey"
	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
)

type ReviewedKey struct {
	From     Key    `json:"from"`
	To       Key    `json:"to"`
	Relation string `json:"relation"`
	Source   string `json:"source"`
}

func ReadReviewedKeys(reg *registry.Registry, catalogs map[string]*parse.Result, data []byte) ([]ReviewedKey, error) {
	var out []ReviewedKey
	seen := map[ReviewedKey]bool{}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if err := uniqueJSON(line); err != nil {
			return nil, err
		}
		if err := reviewedFields(line); err != nil {
			return nil, err
		}
		dec := json.NewDecoder(bytes.NewReader(line))
		dec.DisallowUnknownFields()
		var key ReviewedKey
		if err := dec.Decode(&key); err != nil {
			return nil, fmt.Errorf("decode reviewed key: %w", err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("invalid reviewed key trailing data")
		}
		switch key.Source {
		case "nist-oscal", "nist-csf-withdrawals", "nist-csf-references", "nist-olir", "cis-v8.1-mappings":
		default:
			return nil, fmt.Errorf("invalid reviewed source")
		}
		if !reviewedOwner(reg, catalogs, key) {
			return nil, fmt.Errorf("invalid reviewed source owner")
		}
		if !canonicalReviewed(reg, catalogs, key.From) || !canonicalReviewed(reg, catalogs, key.To) || key.From == key.To {
			return nil, fmt.Errorf("invalid reviewed identity")
		}
		same := key.From.Code == key.To.Code && key.From.Version == key.To.Version
		if same {
			if key.Relation != "related" && key.Relation != "moved-to" && key.Relation != "incorporated-into" {
				return nil, fmt.Errorf("invalid reviewed relation")
			}
		} else if key.Relation != "related" && key.Relation != "equivalent" && key.Relation != "subset" && key.Relation != "superset" && key.Relation != "intersects" {
			return nil, fmt.Errorf("invalid reviewed relation")
		}
		if key.Source != "cis-v8.1-mappings" && !directRelation(key.Source, key.Relation) {
			return nil, fmt.Errorf("invalid source relation")
		}
		if key.Source == "nist-oscal" && !same || key.Source == "nist-csf-withdrawals" && !same {
			return nil, fmt.Errorf("invalid source target")
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate reviewed key")
		}
		seen[key] = true
		out = append(out, key)
	}
	return out, nil
}

func canonicalReviewed(reg *registry.Registry, catalogs map[string]*parse.Result, key Key) bool {
	if !businesskey.New().Valid(key.Code, key.Version, key.Citation) {
		return false
	}
	registered := false
	for _, v := range reg.Frameworks {
		if v.Code == key.Code && v.Version == key.Version {
			registered = true
			break
		}
	}
	if !registered {
		return false
	}
	if result := catalogs[key.Code+":"+key.Version]; result != nil {
		for _, n := range result.Nodes {
			if norm(lookupCitation(reg, Key{key.Code, key.Version, n.Citation})) == norm(lookupCitation(reg, key)) {
				return n.Citation == key.Citation
			}
		}
	}
	return false
}

func uniqueJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("decode reviewed key: %w", err)
		}
		d, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch d {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				if err != nil {
					return fmt.Errorf("decode reviewed key: %w", err)
				}
				s, ok := key.(string)
				if !ok || seen[s] {
					return fmt.Errorf("duplicate or invalid field")
				}
				seen[s] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid JSON")
		}
		if _, err := dec.Token(); err != nil {
			return fmt.Errorf("decode reviewed key: %w", err)
		}
		return nil
	}
	return walk()
}

func reviewedFields(data []byte) error {
	var row map[string]json.RawMessage
	if err := json.Unmarshal(data, &row); err != nil {
		return fmt.Errorf("decode reviewed fields: %w", err)
	}
	if len(row) != 4 || row["from"] == nil || row["to"] == nil || row["relation"] == nil || row["source"] == nil {
		return fmt.Errorf("invalid reviewed fields")
	}
	for _, field := range []string{"from", "to"} {
		var identity map[string]json.RawMessage
		if err := json.Unmarshal(row[field], &identity); err != nil {
			return fmt.Errorf("decode reviewed identity: %w", err)
		}
		if len(identity) != 3 || identity["code"] == nil || identity["version"] == nil || identity["citation"] == nil {
			return fmt.Errorf("invalid reviewed identity fields")
		}
	}
	return nil
}

func reviewedOwner(reg *registry.Registry, catalogs map[string]*parse.Result, key ReviewedKey) bool {
	for _, v := range reg.Frameworks {
		if v.Code != key.From.Code || v.Version != key.From.Version {
			continue
		}
		switch key.Source {
		case "nist-oscal":
			file, err := v.SelectedSource()
			return err == nil && file.Role == "catalog" && file.Format == "oscal-json" && v.Parser == "nist-oscal-800-53" && key.From.Code == key.To.Code && key.From.Version == key.To.Version
		case "nist-csf-withdrawals", "nist-csf-references":
			file, err := v.SelectedSource()
			if err != nil || file.Role != "catalog" || file.Format != "xlsx" || v.Parser != "nist-csf-2-xlsx" {
				return false
			}
			if key.Source == "nist-csf-references" {
				return true
			}
			if key.From.Code != key.To.Code || key.From.Version != key.To.Version {
				return false
			}
			if catalog := catalogs[v.Code+":"+v.Version]; catalog != nil {
				for _, node := range catalog.Nodes {
					if node.Citation == key.From.Citation {
						return node.Kind == "category" || node.Kind == "subcategory"
					}
				}
			}
			return false
		case "nist-olir":
			return v.Code == "nist80053" && v.Version == "r5" && key.To.Code == "iso27001" && key.To.Version == "2022" && registeredWorkbook(v, "nist-olir")
		case "cis-v8.1-mappings":
			if v.Code != "ciscontrols" || v.Version != "v8.1" {
				return false
			}
			switch {
			case key.To.Code == "iso27001" && key.To.Version == "2022":
				return registeredWorkbook(v, "cis-iso27001")
			case key.To.Code == "nistcsf" && key.To.Version == "2.0":
				return registeredWorkbook(v, "cis-nistcsf")
			case key.To.Code == "nist80053" && key.To.Version == "r5":
				return registeredWorkbook(v, "cis-nist80053")
			}
		}
	}
	return false
}

func registeredWorkbook(version registry.Framework, parser string) bool {
	for _, file := range version.Files {
		if file.Role == "mapping workbook" && file.Format == "xlsx" && file.Parser == parser {
			return true
		}
	}
	return false
}
