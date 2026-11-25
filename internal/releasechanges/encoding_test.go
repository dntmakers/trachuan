package releasechanges

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestEncoding_Golden(t *testing.T) {
	key := "vn:key"
	payload, err := CanonicalPayload([]byte(`{"z":null,"s":"\"\\\u0001Việt","n":-2,"zero":0}`))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	h, err := NewWriter(Header{Version: 1, Country: "vn", Release: "r"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Add(Row{Ordinal: 1, Kind: "document_added", Key: &key, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("testdata/projection.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), expected) {
		t.Fatalf("bytes %q, want %q", output.Bytes(), expected)
	}
	digest, err := os.ReadFile("testdata/projection.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if h.Sum() != strings.TrimSpace(string(digest)) {
		t.Fatalf("digest %s", h.Sum())
	}
}

func TestCanonicalPayload_Values(t *testing.T) {
	got, err := CanonicalPayload([]byte(`{"z":[-0,true,{"β":"<>&","a":"\n"}],"a":1}`))
	if err != nil || got != `{"a":1,"z":[0,true,{"a":"\u000a","β":"<>&"}]}` {
		t.Fatalf("%s %v", got, err)
	}
	backslash := string(rune(92))
	valid, pairErr := CanonicalPayload([]byte(`{"a":"` + backslash + `ud83d` + backslash + `ude00"}`))
	if pairErr != nil || valid != `{"a":"😀"}` {
		t.Fatalf("valid pair: %s %v", valid, pairErr)
	}
	for _, input := range []string{`{"a":1,"a":2}`, `{"a":1.5}`, `{"a":1e2}`, `[]`, `null`, `{"a":"\ud800"}`, `{"a":"\udc00"}`, `{"a":"\ud800A"}`} {
		if _, err := CanonicalPayload([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestHasher_InvalidRows(t *testing.T) {
	key, empty := "key", ""
	for _, tc := range []struct {
		header Header
		row    Row
	}{
		{Header{Version: 1, Country: "vn", Release: "r", Baseline: true}, Row{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{}`}},
		{Header{Version: 1, Country: "vn", Release: "r"}, Row{Ordinal: 2, Kind: "document_added", Key: &key, Payload: `{}`}},
		{Header{Version: 1, Country: "vn", Release: "r"}, Row{Ordinal: 1, Kind: "unknown", Key: &key, Payload: `{}`}},
		{Header{Version: 1, Country: "vn", Release: "r"}, Row{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `null`}},
		{Header{Version: 1, Country: "vn", Release: "r"}, Row{Ordinal: 1, Kind: "document_added", Key: &empty, Payload: `{}`}},
	} {
		h, err := NewHasher(tc.header)
		if err != nil {
			t.Fatal(err)
		}
		if h.Add(tc.row) == nil {
			t.Fatalf("accepted %+v", tc.row)
		}
	}
	h, err := NewHasher(Header{Version: 1, Country: "vn", Release: "r"})
	if err != nil {
		t.Fatal(err)
	}
	row := Row{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{}`}
	if err := h.Add(row); err != nil {
		t.Fatal(err)
	}
	if h.Add(row) == nil {
		t.Fatal("accepted duplicate")
	}
}
