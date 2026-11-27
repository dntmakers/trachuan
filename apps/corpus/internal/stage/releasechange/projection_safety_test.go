package releasechange

import (
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

func TestProjectStream_UnsafeUnicodeAndKeys(t *testing.T) {
	header := `{"schema":1,"bindings":{"rel":"r","dump_sha256":"d","build_artifact_sha256":"b","report_sha256":"p","predecessor_rel":"x","predecessor_identity_sha256":"i"},"baseline":false}`
	record := `{"type":"document","key":"a","change":"added","cause":"membership"}`
	cases := []string{
		header + "\n" + strings.Replace(record, `"key":"a"`, `"key":"a\u0000b"`, 1),
		header + "\n" + strings.Replace(record, `"key":"a"`, `"key":"a\u0009b"`, 1),
		header + "\n" + strings.Replace(record, `"key":"a"`, `"key":"\ud800"`, 1),
		header + "\n" + strings.Replace(record, `"key":"a"`, "\"key\":\"\xff\"", 1),
		strings.Replace(header, `"rel":"r"`, `"rel":"r\u000ab"`, 1) + "\n" + record,
	}
	for _, input := range cases {
		n := 0
		err := ProjectStream(strings.NewReader(input+"\n"), func(releasechanges.Row) error { n++; return nil })
		if err == nil || !strings.Contains(err.Error(), "line ") || n != 0 {
			t.Fatalf("input %q err %v emitted %d", input, err, n)
		}
	}
}

func TestCompactContextValue_StoredBytes(t *testing.T) {
	raw := []byte(`{"before":"old",` + strings.Repeat(" ", 20000) + `"after":"new"}`)
	got, err := compactContextValue(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) > 4096 || cap(got) > 4096 || string(got) != `{"before":"old","after":"new"}` {
		t.Fatalf("stored %d bytes", len(got))
	}
}

func TestProjection_LongHandleOmitted(t *testing.T) {
	source := func(yield func(any) error) error {
		return yield(provisionChange{Type: changeProvision, Key: strings.Repeat("k", 300), Anchor: strings.Repeat("a", 300), Change: "added", TextSHA256: valueChange[*string]{}})
	}
	var rows []releasechanges.Row
	err := ProjectRecords(false, source, func(row releasechanges.Row) error { rows = append(rows, row); return nil })
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %d err %v", len(rows), err)
	}
}
