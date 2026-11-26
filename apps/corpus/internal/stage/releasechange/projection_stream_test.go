package releasechange

import (
	"encoding/json"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

func TestProjectStream_Validation(t *testing.T) {
	cases := []string{
		"{\"schema\":99,\"baseline\":false}\n",
		"{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n{bad}\n",
		"{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n{\"type\":\"unknown\"}\n",
		"{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n{\"type\":\"document\",\"key\":\"a\"}\n",
		"{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n{\"type\":\"document_key\",\"key\":\"b\",\"before\":\"old\"}\n{\"type\":\"document_key\",\"key\":\"a\",\"before\":\"old\"}\n",
		"{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n{\"type\":\"document_legal\",\"key\":\"a\",\"eff_from\":{\"after\":\"2099-01-01\"}}\n",
		"{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n{\"type\":\"document\",\"key\":\"" + strings.Repeat("a", 4096) + "\",\"change\":\"added\",\"cause\":\"membership\"}\n",
		"{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n{\"type\":\"document_key\",\"key\":\"a\",\"before\":\"b\"}\n{\"type\":\"document_key\",\"key\":\"a\",\"before\":\"b\"}\n",
	}
	for _, input := range cases {
		err := ProjectStream(strings.NewReader(input), func(releasechanges.Row) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "line ") {
			t.Fatalf("missing line error for %s: %v", input, err)
		}
	}
}

func TestProjectRecords_Generator(t *testing.T) {
	source := func(yield func(any) error) error {
		for _, r := range []any{keyChange{Type: changeDocumentKey, Key: "new", Before: "old"}, legalChange{Type: changeLegal, Key: "new", IsCurrent: &valueChange[bool]{Before: true, After: false}}} {
			if err := yield(r); err != nil {
				return err
			}
		}
		return nil
	}
	var rows []releasechanges.Row
	if err := ProjectRecords(false, source, func(row releasechanges.Row) error { rows = append(rows, row); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !json.Valid([]byte(rows[1].Payload)) || rows[1].Key2 == nil || *rows[1].Key2 != "old" {
		t.Fatal(rows)
	}
}
