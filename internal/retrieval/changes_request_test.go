package retrieval

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestChangesRequest_Validation(t *testing.T) {
	cases := []struct {
		name, input string
		valid       bool
	}{
		{"default", `{"country":"vn"}`, true},
		{"all kinds", `{"country":"vn","kinds":["document_added","document_removed","document_validity","provision_text","document_key"],"document":"vn:vbpl:123","limit":50}`, true},
		{"minimum", `{"country":"vn","limit":1}`, true},
		{"missing country", `{}`, false}, {"wrong country", `{"country":"us"}`, false},
		{"unknown field", `{"country":"vn","extra":1}`, false}, {"duplicate field", `{"country":"vn","country":"vn"}`, false},
		{"null country", `{"country":null}`, false}, {"null release", `{"country":"vn","release":null}`, false},
		{"null kinds", `{"country":"vn","kinds":null}`, false}, {"null document", `{"country":"vn","document":null}`, false},
		{"null limit", `{"country":"vn","limit":null}`, false}, {"null cursor", `{"country":"vn","cursor":null}`, false},
		{"wrong type", `{"country":"vn","limit":"20"}`, false}, {"empty kinds", `{"country":"vn","kinds":[]}`, false},
		{"duplicate kinds", `{"country":"vn","kinds":["document_added","document_added"]}`, false},
		{"provision_validity", `{"country":"vn","kinds":["provision_validity"]}`, false}, {"unknown kind", `{"country":"vn","kinds":["other"]}`, false}, {"relation", `{"country":"vn","kinds":["relation"]}`, false},
		{"invalid document", `{"country":"vn","document":"vn:a#b"}`, false}, {"empty document", `{"country":"vn","document":""}`, false},
		{"zero limit", `{"country":"vn","limit":0}`, false}, {"large limit", `{"country":"vn","limit":51}`, false},
		{"empty release", `{"country":"vn","release":""}`, false}, {"empty cursor", `{"country":"vn","cursor":""}`, false},
		{"oversized cursor", fmt.Sprintf(`{"country":"vn","cursor":%q}`, strings.Repeat("x", 513)), false},
		{"not object", `[]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req ChangesRequest
			err := json.Unmarshal([]byte(tc.input), &req)
			if err == nil {
				_, _, err = normalizeChangesRequest(req)
			}
			if (err == nil) != tc.valid {
				t.Fatalf("error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestChangesRequest_UnicodeSurrogates(t *testing.T) {
	for _, tc := range []struct {
		name, document, want string
		valid                bool
	}{
		{"lone high", `vn:\ud800`, "", false},
		{"lone low", `vn:\udc00`, "", false},
		{"reversed", `vn:\udc00\ud800`, "", false},
		{"valid pair", "vn:" + "\\" + "ud83d" + "\\" + "ude00", "vn:😀", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req ChangesRequest
			err := json.Unmarshal([]byte(`{"country":"vn","document":"`+tc.document+`"}`), &req)
			if err == nil {
				_, _, err = normalizeChangesRequest(req)
			}
			if tc.valid && (err != nil || req.Document != tc.want) {
				t.Fatalf("document=%q err=%v", req.Document, err)
			}
			if !tc.valid && !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestChangesRequest_Cursor(t *testing.T) {
	var req ChangesRequest
	if err := json.Unmarshal([]byte(`{"country":"vn","kinds":["document_removed","document_added"],"document":"vn:abc","limit":3}`), &req); err != nil {
		t.Fatal(err)
	}
	normalized, cursor, err := normalizeChangesRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(normalized.Kinds, []string{"document_added", "document_removed"}) || cursor.FirstLimit != 3 {
		t.Fatalf("normalized = %+v cursor = %+v", normalized, cursor)
	}
	cursor.Release = "20990101"
	cursor.LastOrdinal = 7
	token := encodeChangesCursor(cursor)
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"same filters reordered", fmt.Sprintf(`{"country":"vn","kinds":["document_added","document_removed"],"document":"vn:abc","cursor":%q}`, token), true},
		{"matching release and limit", fmt.Sprintf(`{"country":"vn","kinds":["document_removed","document_added"],"document":"vn:abc","release":"20990101","limit":3,"cursor":%q}`, token), true},
		{"changed limit", fmt.Sprintf(`{"country":"vn","kinds":["document_added","document_removed"],"document":"vn:abc","limit":4,"cursor":%q}`, token), false},
		{"changed key", fmt.Sprintf(`{"country":"vn","kinds":["document_added","document_removed"],"document":"vn:other","cursor":%q}`, token), false},
		{"changed kinds", fmt.Sprintf(`{"country":"vn","document":"vn:abc","cursor":%q}`, token), false},
		{"changed release", fmt.Sprintf(`{"country":"vn","kinds":["document_added","document_removed"],"document":"vn:abc","release":"20990102","cursor":%q}`, token), false},
		{"bad token", `{"country":"vn","cursor":"!"}`, false},
		{"forged limit", fmt.Sprintf(`{"country":"vn","cursor":%q}`, base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"release":"20990101","last_ordinal":1,"first_limit":9223372036854775807,"digest":"0000000000000000000000000000000000000000000000000000000000000000"}`))), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r ChangesRequest
			err := json.Unmarshal([]byte(tc.body), &r)
			if err == nil {
				_, _, err = normalizeChangesRequest(r)
			}
			if (err == nil) != tc.valid || err != nil && !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestChangesCursor_RejectsMalformedFields(t *testing.T) {
	valid := changesCursor{V: 1, Release: "20990101", LastOrdinal: 1, FirstLimit: 20, Digest: strings.Repeat("0", 64)}
	for _, tc := range []struct {
		name   string
		mutate func(*changesCursor)
	}{
		{"version", func(c *changesCursor) { c.V = 2 }},
		{"release", func(c *changesCursor) { c.Release = "bad" }},
		{"ordinal zero", func(c *changesCursor) { c.LastOrdinal = 0 }},
		{"ordinal negative", func(c *changesCursor) { c.LastOrdinal = -1 }},
		{"limit zero", func(c *changesCursor) { c.FirstLimit = 0 }},
		{"limit too large", func(c *changesCursor) { c.FirstLimit = 51 }},
		{"digest", func(c *changesCursor) { c.Digest = "invalid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := valid
			tc.mutate(&c)
			if _, err := decodeChangesCursor(encodeChangesCursor(c)); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestValidDocumentKey_Grammar(t *testing.T) {
	for _, tc := range []struct {
		key   string
		valid bool
	}{{"vn:abc", true}, {"vn:đ", true}, {"vn:", false}, {"us:abc", false}, {"vn:a/b", false}, {"vn:a?b", false}, {"vn:a#b", false}, {"vn:a\\b", false}, {"vn:a b", false}, {"vn:a\nb", false}, {"vn:\xff", false}} {
		if got := ValidDocumentKey(tc.key); got != tc.valid {
			t.Errorf("ValidDocumentKey(%q) = %v", tc.key, got)
		}
	}
}
