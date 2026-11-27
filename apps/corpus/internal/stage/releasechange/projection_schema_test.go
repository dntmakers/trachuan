package releasechange

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

func TestProjection_ClosedSchema(t *testing.T) {
	header := `{"schema":1,"bindings":{"rel":"r","dump_sha256":"d","build_artifact_sha256":"b","report_sha256":"p","predecessor_rel":"x","predecessor_identity_sha256":"i"},"baseline":false}`
	cases := []string{
		`{"schema":1,"baseline":false}`,
		strings.Replace(header, `"rel":"r"`, `"rel":"r","rel":"s"`, 1),
		strings.Replace(header, `"baseline":false`, `"baseline":"false"`, 1),
		header + "\n" + `{"type":"document","key":"a","change":"added","cause":"membership","extra":true}`,
		header + "\n" + `{"type":"document_legal","key":"a","eff_from":{"before":null,"before":"x","after":"d"}}`,
		header + "\n" + `{"type":"provision_validity","key":"a","anchor":"p","added":[{"status_class":"x","eff_from":null,"eff_to":null}],"removed":[]}`,
		header + "\n" + `{"type":"relation","kind":"x","method":"m","source":{"key":null,"number":"","in_release":false,"anchor":null},"target":{"key":null,"number":"","in_release":false,"anchor":null},"change":"added","count":0}`,
		header + "\n" + `{"type":"document","key":"a","key":"b","change":"added","cause":"membership"}`,
		header + "\n" + `{"type":"document","key":"a","change":"added","cause":"membership","replaced_by":"b"}`,
		header + "\n" + `{"type":"document_legal","key":"a","eff_from":{"before":null,"after":"d","extra":true}}`,
		header + "\n" + `{"type":"provision_validity","key":"a","anchor":"p","added":[{"status_class":"x","eff_from":null,"eff_to":null,"method":"text","extra":1}],"removed":[]}`,
		header + "\n" + `{"type":"relation","kind":"x","method":"m","source":{},"target":{},"change":"added","count":0}`,
	}
	for _, input := range cases {
		err := ProjectStream(strings.NewReader(input+"\n"), func(releasechanges.Row) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "line ") {
			t.Fatalf("accepted %s: %v", input, err)
		}
	}
}

func TestProjection_ExactPayloads(t *testing.T) {
	source := func(yield func(any) error) error {
		records := []any{
			documentChange{Type: changeDocument, Key: "a", Change: "added", Cause: "membership"},
			documentChange{Type: changeDocument, Key: "b", Change: "removed", Cause: "coverage"},
			keyChange{Type: changeDocumentKey, Key: "c", Before: "old"},
			legalChange{Type: changeLegal, Key: "c", IsCurrent: &valueChange[bool]{Before: true, After: false}},
			provisionChange{Type: changeProvision, Key: "c", Anchor: "x", Change: "changed", TextSHA256: valueChange[*string]{}},
			validityChange{Type: changeValidity, Key: "c", Anchor: "x", Added: []ValidityRow{{StatusClass: "in_force", Method: "text"}}, Removed: []ValidityRow{{StatusClass: "expired", Method: "text"}}},
		}
		for _, r := range records {
			if err := yield(r); err != nil {
				return err
			}
		}
		return nil
	}
	var got []string
	err := ProjectRecords(false, source, func(row releasechanges.Row) error {
		got = append(got, fmt.Sprintf("%s %s", row.Kind, row.Payload))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`document_added {"cause":"membership","key":"a"}`,
		`document_removed {"cause":"coverage","key":"b"}`,
		`document_key {"before_key":"old","key":"c"}`,
		`document_validity {"is_current":{"after":false,"before":true},"key":"c","source_change":{"before_key":"old"}}`,
		`provision_text {"ambiguous":false,"anchor":"x","change":"changed","key":"c","provenance":false}`,
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatalf("got %s want %s", a, b)
	}
}

func TestProjection_ProvisionValidityYieldsNoRow(t *testing.T) {
	for _, flags := range []struct{ provenance, ambiguous bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		source := func(yield func(any) error) error {
			return yield(validityChange{Type: changeValidity, Key: "c", Anchor: "x", Added: []ValidityRow{{StatusClass: "in_force", Method: "text"}}, Removed: []ValidityRow{{StatusClass: "expired", Method: "text"}}, Provenance: flags.provenance, Ambiguous: flags.ambiguous})
		}
		var rows []releasechanges.Row
		if err := ProjectRecords(false, source, func(row releasechanges.Row) error { rows = append(rows, row); return nil }); err != nil || len(rows) != 0 {
			t.Fatalf("flags %+v rows %v err %v", flags, rows, err)
		}
	}
}
