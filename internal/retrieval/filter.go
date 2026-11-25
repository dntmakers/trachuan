package retrieval

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

func normalizeFilters(req Request) (Request, error) {
	for _, d := range []*Date{req.IssuedFrom, req.IssuedTo} {
		if d != nil {
			t := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
			if d.Year < 1 || d.Year > 9999 || t.Year() != d.Year || t.Month() != d.Month || t.Day() != d.Day {
				return Request{}, ErrInvalidRequest
			}
		}
	}
	if req.IssuedFrom != nil && req.IssuedTo != nil && dateOrder(*req.IssuedFrom) > dateOrder(*req.IssuedTo) {
		return Request{}, ErrInvalidRequest
	}
	fields := []*[]string{&req.Nodes, &req.DocTypes, &req.Issuers, &req.Documents}
	for i, field := range fields {
		budget := 20
		if i == 1 || i == 2 {
			budget = 10
		}
		if len(*field) > budget {
			return Request{}, ErrInvalidRequest
		}
		values := make([]string, 0, len(*field))
		for _, value := range *field {
			if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
				return Request{}, ErrInvalidRequest
			}
			value = strings.TrimSpace(value)
			if i == 1 || i == 2 {
				value = norm.NFC.String(value)
			}
			if value == "" || i == 0 && !validTaxonomyCode(value) {
				return Request{}, ErrInvalidRequest
			}
			values = append(values, value)
		}
		slices.Sort(values)
		*field = slices.Compact(values)
	}
	return req, nil
}

func hasFilters(req Request) bool {
	return len(req.Nodes)+len(req.DocTypes)+len(req.Issuers)+len(req.Documents) > 0 || req.IssuedFrom != nil || req.IssuedTo != nil
}

func dateOrder(d Date) int {
	return d.Year*10000 + int(d.Month)*100 + d.Day
}

func nodeCodePredicateSQL(code string) string {
	return fmt.Sprintf("(%s=$4 OR left(%s,length($4)+1)=$4||'.')", code, code)
}

func matchesFilter(req Request, id Identity, nodes []string, doc DocumentEntry) bool {
	if req.IssuedFrom != nil || req.IssuedTo != nil {
		if doc.Issued == nil || req.IssuedFrom != nil && dateOrder(*doc.Issued) < dateOrder(*req.IssuedFrom) || req.IssuedTo != nil && dateOrder(*doc.Issued) > dateOrder(*req.IssuedTo) {
			return false
		}
	}
	if len(req.Documents) > 0 && !slices.Contains(req.Documents, id.DocumentKey) {
		return false
	}
	if len(req.DocTypes) > 0 && !slices.Contains(req.DocTypes, norm.NFC.String(strings.TrimSpace(doc.Type))) {
		return false
	}
	if len(req.Issuers) > 0 && !slices.Contains(req.Issuers, norm.NFC.String(strings.TrimSpace(doc.Issuer))) {
		return false
	}
	if len(req.Nodes) > 0 {
		for _, filter := range req.Nodes {
			for _, code := range nodes {
				if code == filter || strings.HasPrefix(code, filter+".") {
					return true
				}
			}
		}
		return false
	}
	return true
}

func validateServedFilters(req Request, refs map[string]NodeRef) error {
	for _, code := range req.Nodes {
		if _, ok := refs[code]; !ok {
			return ErrInvalidRequest
		}
	}
	return nil
}
