package releasechange

import (
	"os"
	"strings"
	"testing"
)

func TestProjectChanges_Fixture(t *testing.T) {
	f, err := os.Open("testdata/changes.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	rows, err := ProjectChanges(f)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for i, row := range rows {
		if row.Ordinal != int64(i+1) {
			t.Fatal(row.Ordinal)
		}
		kinds[row.Kind]++
		if row.Kind == "document_validity" && (!strings.Contains(string(row.Payload), `"before":null`) || !strings.Contains(string(row.Payload), `"source_change"`)) {
			t.Fatal(string(row.Payload))
		}
	}
	for _, k := range []string{"document_added", "document_removed", "document_key", "document_validity", "provision_text"} {
		if kinds[k] == 0 {
			t.Fatal(k)
		}
	}
	if kinds["provision_validity"] != 0 || kinds["relation"] != 0 {
		t.Fatal(kinds)
	}
}

func TestProjectChanges_ContextAndTuples(t *testing.T) {
	input := `{"schema":1,"bindings":{"rel":"r","dump_sha256":"d","build_artifact_sha256":"b","report_sha256":"p","predecessor_rel":"x","predecessor_identity_sha256":"i"},"baseline":false}
{"type":"document_key","key":"new","before":"old"}
{"type":"document_legal","key":"new","eff_from":{"before":null,"after":"2099-01-01"}}
{"type":"document_provenance","key":"new","baseline":{"before":true,"after":false}}
{"type":"provision_validity","key":"new","anchor":"a","added":[{"status_class":"active","eff_from":null,"eff_to":null,"method":"text"}],"removed":[]}
`
	rows, err := ProjectChanges(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1].Key2 == nil || *rows[1].Key2 != "old" || !strings.Contains(string(rows[1].Payload), `"before_key":"old"`) || !strings.Contains(string(rows[1].Payload), `"baseline"`) {
		t.Fatalf("%+v", rows)
	}
	baseline, err := ProjectChanges(strings.NewReader("{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"none\",\"predecessor_identity_sha256\":\"none\"},\"baseline\":true}\n"))
	if err != nil || len(baseline) != 0 {
		t.Fatalf("%v %v", baseline, err)
	}
}
