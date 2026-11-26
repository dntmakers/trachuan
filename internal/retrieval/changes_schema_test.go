package retrieval

import (
	"encoding/json"
	"errors"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func TestChangePayload_ClosedTypedSchemas(t *testing.T) {
	cases := []struct{ kind, valid, missing, wrong, enum string }{
		{"document_added", `{"cause":"membership","key":"vn:1"}`, "cause", `{"cause":true,"key":"vn:1"}`, `{"cause":"bad","key":"vn:1"}`},
		{"document_removed", `{"cause":"source","key":"vn:1"}`, "cause", `{"cause":2,"key":"vn:1"}`, `{"cause":"bad","key":"vn:1"}`},
		{"document_key", `{"before_key":"vn:old","key":"vn:1"}`, "before_key", `{"before_key":false,"key":"vn:1"}`, ""},
		{"document_validity", `{"key":"vn:1","status_class":{"before":"unknown","after":"in_force"}}`, "key", `{"key":"vn:1","is_current":{"before":"yes","after":true}}`, `{"key":"vn:1","status_class":{"before":"bad","after":"in_force"}}`},
		{"provision_validity", `{"anchor":"p1","change":"added","key":"vn:1","validity":{"status_class":"in_force","eff_from":null,"eff_to":"2020-01-01","method":"text"}}`, "validity", `{"anchor":"p1","change":"added","key":"vn:1","validity":{"status_class":"in_force","eff_from":false,"eff_to":null,"method":"text"}}`, `{"anchor":"p1","change":"added","key":"vn:1","validity":{"status_class":"bad","eff_from":null,"eff_to":null,"method":"text"}}`},
		{"provision_text", `{"ambiguous":false,"anchor":"p1","change":"changed","key":"vn:1","provenance":false}`, "provenance", `{"ambiguous":"no","anchor":"p1","change":"changed","key":"vn:1","provenance":false}`, `{"ambiguous":false,"anchor":"p1","change":"bad","key":"vn:1","provenance":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			key, old := "vn:1", "vn:old"
			canonical, err := releasechanges.CanonicalPayload([]byte(tc.valid))
			if err != nil {
				t.Fatal(err)
			}
			row := releasechanges.Row{Kind: tc.kind, Key: &key, Payload: canonical}
			if tc.kind == "document_key" {
				row.Key2 = &old
			}
			if _, err := validateChangePayload(row); err != nil {
				t.Fatalf("valid: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.valid), &fields); err != nil {
				t.Fatal(err)
			}
			delete(fields, tc.missing)
			missing, _ := json.Marshal(fields)
			if err := json.Unmarshal([]byte(tc.valid), &fields); err != nil {
				t.Fatal(err)
			}
			fields["extra"] = json.RawMessage(`true`)
			extra, _ := json.Marshal(fields)
			for name, raw := range map[string]string{"missing": string(missing), "extra": string(extra), "wrong type": tc.wrong, "enum": tc.enum} {
				if raw == "" {
					continue
				}
				t.Run(name, func(t *testing.T) {
					encoded, err := releasechanges.CanonicalPayload([]byte(raw))
					if err != nil {
						t.Fatal(err)
					}
					if validSchemaValue(json.RawMessage(encoded), ChangePayloadSchemas()[tc.kind]) {
						t.Fatalf("schema accepted %s: %s", name, encoded)
					}
					row.Payload = encoded
					var invalid *corpusdb.InvalidProjectionError
					if _, err := validateChangePayload(row); !errors.Is(err, ErrUnavailable) || !errors.As(err, &invalid) {
						t.Fatalf("accepted %s: %v", raw, err)
					}
				})
			}
		})
	}
}
