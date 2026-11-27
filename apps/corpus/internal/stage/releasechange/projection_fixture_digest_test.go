package releasechange

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

func TestProjectChanges_FixtureDigest(t *testing.T) {
	f, err := os.Open("testdata/changes.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("missing fixture header")
	}
	var fixtureHeader struct {
		Bindings struct {
			Rel         string `json:"rel"`
			Predecessor string `json:"predecessor_rel"`
			Identity    string `json:"predecessor_identity_sha256"`
		} `json:"bindings"`
		Baseline bool `json:"baseline"`
	}
	if err = json.Unmarshal(scanner.Bytes(), &fixtureHeader); err != nil {
		t.Fatal(err)
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	rows, err := ProjectChanges(f)
	if err != nil {
		t.Fatal(err)
	}
	previous := fixtureHeader.Bindings.Predecessor
	identity := strings.TrimPrefix(fixtureHeader.Bindings.Identity, "sha256:")
	h, err := releasechanges.NewHasher(releasechanges.Header{Version: releasechanges.Version, Country: "vn", Release: fixtureHeader.Bindings.Rel, Predecessor: &previous, PredecessorIdentityHash: &identity, Baseline: fixtureHeader.Baseline})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if err = h.Add(row); err != nil {
			t.Fatal(err)
		}
	}
	got := struct {
		SHA256 string         `json:"sha256"`
		Counts map[string]int `json:"counts"`
	}{h.Sum(), h.Counts()}
	data, err := os.ReadFile("testdata/changes.projection.json")
	if err != nil {
		t.Fatalf("read expected projection: %v; computed %+v", err, got)
	}
	var want struct {
		SHA256 string         `json:"sha256"`
		Counts map[string]int `json:"counts"`
	}
	if err = json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection got %+v, want %+v", got, want)
	}
}
