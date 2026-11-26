package retrieval

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type queryRoute string

const (
	routeNumber      queryRoute = "number"
	routeMarked      queryRoute = "marked"
	routeRestore     queryRoute = "restore"
	routeUnsupported queryRoute = "unsupported"
)

type Validation struct {
	Route      queryRoute
	Accepted   bool
	Routed     bool
	References []string
	Reason     string
	Message    string
}

func (v Validation) RestorationRoute() bool { return v.Route == routeRestore }

// ValidateQuery applies the deterministic query gate.
func ValidateQuery(q string) (Validation, error) {
	if !utf8.ValidString(q) || utf8.RuneCountInString(q) > 2000 || strings.TrimSpace(q) == "" || strings.ContainsRune(q, 0) {
		return Validation{}, errors.New("invalid_request")
	}
	refs, err := ParseReferences(norm.NFC.String(q))
	if err != nil {
		return Validation{}, errors.New("invalid_request")
	}
	v := Validation{References: refs, Routed: len(refs) > 0}
	var latin, marked bool
	for _, r := range norm.NFC.String(q) {
		if unicode.In(r, unicode.Latin) && unicode.IsLetter(r) {
			latin = true
		}
		if r == 'đ' || r == 'Đ' {
			marked = true
			continue
		}
		d := []rune(norm.NFD.String(string(r)))
		if len(d) < 2 || !strings.ContainsRune("aeiouyAEIOUY", d[0]) {
			continue
		}
		for _, m := range d[1:] {
			if strings.ContainsRune("\u0306\u0302\u031b\u0301\u0300\u0309\u0303\u0323", m) {
				marked = true
			}
		}
	}
	if v.Routed {
		v.Route = routeNumber
		v.Accepted = true
		return v, nil
	}
	if marked {
		v.Route = routeMarked
		v.Accepted = true
		return v, nil
	}
	v.Route = routeRestore
	v.Reason = "diacritics_required"
	if !latin {
		v.Route = routeUnsupported
		v.Reason = "unsupported_query"
	}
	v.Message = "Vui lòng nhập câu hỏi tiếng Việt có dấu hoặc số hiệu văn bản."
	return v, nil
}

func resolveValidation(v Validation, recipe Recipe, legacy bool) Validation {
	if v.Route == routeRestore && (recipe.Version == 4 || legacy) {
		v.Accepted = true
		v.Reason = ""
		v.Message = ""
	}
	if v.Route == routeUnsupported && recipe.Version == 4 {
		v.Message = "Vui lòng nhập câu hỏi tiếng Việt hoặc số hiệu văn bản."
	}
	return v
}
