package retrieval

import (
	"errors"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func TestChangePayload_RejectsUnboundAndNoncanonicalRows(t *testing.T) {
	key, old, wrong := "vn:1", "vn:old", "vn:wrong"
	for _, tc := range []struct {
		name string
		row  releasechanges.Row
	}{
		{"unexpected secondary", releasechanges.Row{Kind: "document_added", Key: &key, Key2: &old, Payload: `{"cause":"membership","key":"vn:1"}`}},
		{"missing secondary", releasechanges.Row{Kind: "document_key", Key: &key, Payload: `{"before_key":"vn:old","key":"vn:1"}`}},
		{"wrong secondary", releasechanges.Row{Kind: "document_key", Key: &key, Key2: &wrong, Payload: `{"before_key":"vn:old","key":"vn:1"}`}},
		{"wrong replacement", releasechanges.Row{Kind: "document_removed", Key: &key, Key2: &wrong, Payload: `{"cause":"source","key":"vn:1","replaced_by":"vn:old"}`}},
		{"wrong context", releasechanges.Row{Kind: "document_validity", Key: &key, Key2: &wrong, Payload: `{"key":"vn:1","source_change":{"before_key":"vn:old"}}`}},
		{"duplicate", releasechanges.Row{Kind: "document_added", Key: &key, Payload: `{"cause":"membership","cause":"source","key":"vn:1"}`}},
		{"noncanonical", releasechanges.Row{Kind: "document_added", Key: &key, Payload: `{"key":"vn:1","cause":"membership"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var invalid *corpusdb.InvalidProjectionError
			_, err := validateChangePayload(tc.row)
			if !errors.Is(err, ErrUnavailable) || !errors.As(err, &invalid) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
