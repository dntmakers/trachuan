package releasechanges

import "testing"

func TestDigest_Fields(t *testing.T) {
	key, other := "key", "other"
	header := Header{Version: 1, Country: "vn", Release: "r"}
	rows := []Row{{Ordinal: 1, Kind: "document_added", Key: &key, Payload: `{"key":"key"}`}}
	original, err := Digest(header, rows)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := Digest(header, rows)
	if err != nil || repeated != original {
		t.Fatalf("unstable %s %v", repeated, err)
	}
	for _, row := range []Row{{Ordinal: 1, Kind: "document_removed", Key: &key, Payload: rows[0].Payload}, {Ordinal: 1, Kind: rows[0].Kind, Key: &other, Payload: rows[0].Payload}, {Ordinal: 1, Kind: rows[0].Kind, Key: &key, Key2: &other, Payload: rows[0].Payload}, {Ordinal: 1, Kind: rows[0].Kind, Key: &key, Payload: `{}`}} {
		got, digestErr := Digest(header, []Row{row})
		if digestErr != nil || got == original {
			t.Fatalf("unchanged %v %v", row, digestErr)
		}
	}
	if _, versionErr := NewHasher(Header{Version: 2}); versionErr == nil {
		t.Fatal("unknown version")
	}
	predecessor := "p"
	identity := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, h := range []Header{{Version: 1, Country: "other", Release: "r"}, {Version: 1, Country: "vn", Release: "other"}, {Version: 1, Country: "vn", Release: "r", Predecessor: &predecessor}, {Version: 1, Country: "vn", Release: "r", PredecessorIdentityHash: &identity}} {
		got, digestErr := Digest(h, rows)
		if digestErr != nil || got == original {
			t.Fatalf("header unchanged %v %v", h, digestErr)
		}
	}
	baseline, err := Digest(Header{Version: 1, Country: "vn", Release: "r", Baseline: true}, nil)
	if err != nil || baseline == original {
		t.Fatal(baseline, err)
	}
	h, err := NewHasher(header)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Add(rows[0]); err != nil {
		t.Fatal(err)
	}
	if h.Counts()["document_added"] != 1 || h.Counts()["document_removed"] != 0 {
		t.Fatal(h.Counts())
	}
}
