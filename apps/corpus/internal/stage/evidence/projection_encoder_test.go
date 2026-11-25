package evidence

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

func TestEncodeLine_WriterMatchesAtLimit(t *testing.T) {
	record := metadataChange{Type: changeMetadata, Key: "a", NumberNorm: valueChange[string]{Before: "<", After: ""}}
	initial, err := encodeLine(record)
	if err != nil {
		t.Fatal(err)
	}
	record.NumberNorm.After = strings.Repeat("x", lineLimit-len(initial))
	line, err := encodeLine(record)
	if err != nil || len(line) != lineLimit {
		t.Fatalf("length %d: %v", len(line), err)
	}
	var out bytes.Buffer
	writer := newLineWriter(&out)
	if err = writer.write(record); err != nil {
		t.Fatal(err)
	}
	if err = writer.flush(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), append(line, '\n')) {
		t.Fatal("writer bytes differ")
	}
}

func TestProjectRecords_InvalidUTF8AndLineLimit(t *testing.T) {
	cases := []struct {
		name   string
		record metadataChange
	}{
		{"invalid UTF-8", metadataChange{Type: changeMetadata, Key: "a", NumberNorm: valueChange[string]{Before: "old", After: string([]byte{0xff})}}},
		{"oversized", metadataChange{Type: changeMetadata, Key: "a", NumberNorm: valueChange[string]{Before: "old", After: strings.Repeat("x", lineLimit)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := func(yield func(any) error) error { return yield(tc.record) }
			count := 0
			err := ProjectRecords(false, source, func(releasechanges.Row) error { count++; return nil })
			if err == nil || !strings.Contains(err.Error(), "record 1") || count != 0 {
				t.Fatalf("projection: %v, emitted %d", err, count)
			}
			writer := newLineWriter(io.Discard)
			if err := writer.write(tc.record); err == nil {
				t.Fatal("sidecar writer accepted invalid record")
			}
		})
	}
}
