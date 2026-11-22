package evaluation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type WarningCase struct {
	EntryID      string `json:"entry_id"`
	Question     string `json:"question"`
	Target       string `json:"target"`
	Acting       string `json:"acting"`
	RelationKind string `json:"relation_kind"`
}

var warningEntryID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}$`)

func (s Suite) PinnedWarningCases(country string) ([]WarningCase, error) {
	sum := sha256.Sum256(s.WarningBytes)
	if len(s.WarningBytes) == 0 || hex.EncodeToString(sum[:]) != s.WarningHash {
		return nil, fmt.Errorf("warning fixture bytes changed")
	}
	return LoadWarningCases(s.WarningBytes, country)
}

func LoadWarningCases(data []byte, country string) ([]WarningCase, error) {
	if len(data) > 64*1024 || !utf8.Valid(data) {
		return nil, fmt.Errorf("invalid warning fixture size or encoding")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('[') {
		return nil, fmt.Errorf("warning fixture must be an array")
	}
	cases := []WarningCase{}
	for d.More() {
		if len(cases) == 20 {
			return nil, fmt.Errorf("too many warning cases")
		}
		c, decodeErr := decodeWarningCase(d, country)
		if decodeErr != nil {
			return nil, decodeErr
		}
		if len(cases) > 0 && cases[len(cases)-1].EntryID >= c.EntryID {
			return nil, fmt.Errorf("warning entries must be unique and sorted")
		}
		cases = append(cases, c)
	}
	if token, err = d.Token(); err != nil || token != json.Delim(']') {
		return nil, fmt.Errorf("invalid warning fixture array")
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing warning fixture data")
	}
	return cases, nil
}

func decodeWarningCase(d *json.Decoder, country string) (WarningCase, error) {
	var c WarningCase
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return c, fmt.Errorf("warning case must be an object")
	}
	values := map[string]*string{"entry_id": &c.EntryID, "question": &c.Question, "target": &c.Target, "acting": &c.Acting, "relation_kind": &c.RelationKind}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok || values[name] == nil || seen[name] {
			return c, fmt.Errorf("invalid or duplicate warning case field")
		}
		seen[name] = true
		var value any
		if err = d.Decode(&value); err != nil {
			return c, fmt.Errorf("decode warning case field: %w", err)
		}
		text, ok := value.(string)
		if !ok {
			return c, fmt.Errorf("warning case field %s must be a string", name)
		}
		*values[name] = text
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || len(seen) != len(values) {
		return c, fmt.Errorf("invalid warning case object")
	}
	target, okTarget := warningHandle(c.Target, country)
	acting, okActing := warningHandle(c.Acting, country)
	if !warningEntryID.MatchString(c.EntryID) || strings.TrimSpace(c.Question) == "" || utf8.RuneCountInString(c.Question) > 2000 || strings.ContainsRune(c.Question, 0) || !okTarget || !okActing || target == acting || !slices.Contains([]string{"repeals", "amends_supplements", "replaces", "suspends"}, c.RelationKind) {
		return c, fmt.Errorf("invalid warning case %q", c.EntryID)
	}
	return c, nil
}

func warningHandle(handle, country string) (string, bool) {
	key, anchor, found := strings.Cut(handle, "#")
	if !found || strings.ContainsFunc(handle, unicode.IsControl) || !strings.HasPrefix(key, country+":") || len(key) == len(country)+1 || strings.ContainsFunc(key, unicode.IsSpace) || strings.ContainsAny(anchor, "~#") {
		return "", false
	}
	for _, part := range strings.Split(anchor, "/") {
		kind, value, ok := strings.Cut(part, "-")
		if !ok || kind == "" || value == "" || strings.ToLower(part) != part || strings.ContainsFunc(part, unicode.IsSpace) {
			return "", false
		}
		for _, r := range kind {
			if r < 'a' || r > 'z' {
				return "", false
			}
		}
	}
	return key, true
}
