package retrieval

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"slices"
	"unicode/utf8"

	"danny.vn/trachuan/internal/releasechanges"
)

type ChangesRequest struct {
	Country  string   `json:"country"`
	Release  string   `json:"release,omitempty"`
	Kinds    []string `json:"kinds,omitempty"`
	Document string   `json:"document,omitempty"`
	Limit    *int     `json:"limit,omitempty"`
	Cursor   string   `json:"cursor,omitempty"`
}

func closedChangesObject(data []byte, allowed ...string) error {
	d := json.NewDecoder(bytes.NewReader(data))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return ErrInvalidRequest
	}
	seen := make(map[string]bool)
	for d.More() {
		tok, err := d.Token()
		if err != nil {
			return ErrInvalidRequest
		}
		name, ok := tok.(string)
		if !ok || !slices.Contains(allowed, name) || seen[name] {
			return ErrInvalidRequest
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalidRequest
		}
	}
	if _, err := d.Token(); err != nil {
		return ErrInvalidRequest
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrInvalidRequest
	}
	return nil
}

func (r *ChangesRequest) UnmarshalJSON(data []byte) error {
	if _, err := releasechanges.CanonicalPayload(data); err != nil {
		return ErrInvalidRequest
	}
	if closedChangesObject(data, "country", "release", "kinds", "document", "limit", "cursor") != nil {
		return ErrInvalidRequest
	}
	type plain ChangesRequest
	var v plain
	if json.Unmarshal(data, &v) != nil {
		return ErrInvalidRequest
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return ErrInvalidRequest
	}
	for _, name := range []string{"release", "document", "cursor"} {
		if raw, ok := fields[name]; ok && bytes.Equal(bytes.TrimSpace(raw), []byte(`""`)) {
			return ErrInvalidRequest
		}
	}
	*r = ChangesRequest(v)
	return nil
}

func normalizeChangesRequest(req ChangesRequest) (ChangesRequest, changesCursor, error) {
	if req.Country != "vn" || req.Release != "" && !validChangesRelease(req.Release) || req.Document != "" && (!ValidDocumentKey(req.Document) || utf8.RuneCountInString(req.Document) > 512) {
		return ChangesRequest{}, changesCursor{}, ErrInvalidRequest
	}
	if req.Kinds == nil {
		req.Kinds = []string{"document_added", "document_removed", "document_validity", "provision_validity"}
	} else {
		req.Kinds = slices.Clone(req.Kinds)
	}
	if len(req.Kinds) == 0 {
		return ChangesRequest{}, changesCursor{}, ErrInvalidRequest
	}
	slices.Sort(req.Kinds)
	for i, kind := range req.Kinds {
		if !slices.Contains([]string{"document_added", "document_removed", "document_validity", "provision_validity", "provision_text", "document_key"}, kind) || i > 0 && kind == req.Kinds[i-1] {
			return ChangesRequest{}, changesCursor{}, ErrInvalidRequest
		}
	}
	limit := 20
	if req.Limit != nil {
		limit = *req.Limit
		if limit < 1 || limit > 50 {
			return ChangesRequest{}, changesCursor{}, ErrInvalidRequest
		}
	}
	c := changesCursor{V: 1, FirstLimit: limit, Digest: changesDigest(req)}
	if req.Cursor != "" {
		var err error
		c, err = decodeChangesCursor(req.Cursor)
		if err != nil || c.Digest != changesDigest(req) || req.Release != "" && req.Release != c.Release || req.Limit != nil && *req.Limit != c.FirstLimit {
			return ChangesRequest{}, changesCursor{}, ErrInvalidRequest
		}
	}
	return req, c, nil
}

func validChangesRelease(release string) bool {
	return len(release) <= 11 && regexp.MustCompile(`^[0-9]{8}(_[1-9][0-9]*)?$`).MatchString(release)
}
