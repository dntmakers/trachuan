package releasechanges

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestWriter_RejectsNonCanonicalStoredReaderHashesRaw(t *testing.T) {
	key := "a"
	h := Header{Version: Version, Country: "vn", Release: "r"}
	row := Row{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{"z":-0, "a":1}`}
	var out bytes.Buffer
	writer, err := NewWriter(h, &out)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := writer.Add(row); writeErr == nil {
		t.Fatal("writer accepted noncanonical payload")
	}
	reader, err := NewHasher(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Add(row); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte("v2\tvn\tr\t\t\t0\n1\tdocument_added\ta\t\t{\"z\":-0, \"a\":1}\n"))
	if reader.Sum() != hex.EncodeToString(expected[:]) {
		t.Fatal(reader.Sum())
	}
}
