package releasechanges

import "testing"

func TestHasher_Stream(t *testing.T) {
	h := Header{Version: Version, Country: "vn", Release: "r"}
	key := "a"
	rows := []Row{{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{"key":"a"}`}}
	expected, err := Digest(h, rows)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := NewHasher(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Add(rows[0]); err != nil {
		t.Fatal(err)
	}
	if got := stream.Sum(); got != expected {
		t.Fatalf("%s != %s", got, expected)
	}
}
