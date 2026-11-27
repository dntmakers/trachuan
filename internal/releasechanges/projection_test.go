package releasechanges

import (
	"io"
	"testing"
)

func TestDigest_Fields(t *testing.T) {
	key, other := "key", "other"
	header := Header{Version: Version, Country: "vn", Release: "r"}
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
	if _, versionErr := NewHasher(Header{Version: 1, Country: "vn", Release: "r"}); versionErr == nil {
		t.Fatal("unknown version")
	}
	predecessor := "p"
	identity := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, h := range []Header{{Version: Version, Country: "other", Release: "r"}, {Version: Version, Country: "vn", Release: "other"}, {Version: Version, Country: "vn", Release: "r", Predecessor: &predecessor}, {Version: Version, Country: "vn", Release: "r", PredecessorIdentityHash: &identity}} {
		got, digestErr := Digest(h, rows)
		if digestErr != nil || got == original {
			t.Fatalf("header unchanged %v %v", h, digestErr)
		}
	}
	baseline, err := Digest(Header{Version: Version, Country: "vn", Release: "r", Baseline: true}, nil)
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

func TestHasher_RefusesVersionOneAndProvisionValidity(t *testing.T) {
	for _, version := range []int{0, 1, 3} {
		if _, err := NewHasher(Header{Version: version, Country: "vn", Release: "r"}); err == nil {
			t.Fatalf("accepted version %d", version)
		}
		if _, err := NewWriter(Header{Version: version, Country: "vn", Release: "r"}, io.Discard); err == nil {
			t.Fatalf("writer accepted version %d", version)
		}
	}
	h, err := NewHasher(Header{Version: Version, Country: "vn", Release: "r"})
	if err != nil {
		t.Fatal(err)
	}
	key := "key"
	if h.Add(Row{Ordinal: 1, Kind: "provision_validity", Key: &key, Payload: `{}`}) == nil {
		t.Fatal("accepted provision_validity")
	}
	if _, ok := h.Counts()["provision_validity"]; ok || len(h.Counts()) != KindCount {
		t.Fatalf("counts %v", h.Counts())
	}
}
