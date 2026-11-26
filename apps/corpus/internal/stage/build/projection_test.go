package build

import (
	"errors"
	"testing"

	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func TestProjectionReadError_InvalidVersusInfrastructure(t *testing.T) {
	bad := &corpusdb.InvalidProjectionError{Err: errors.New("bad digest")}
	if err := projectionReadError(bad); !errors.Is(err, errInvalidDatabase) || !errors.Is(err, bad) {
		t.Fatalf("invalid projection error = %v", err)
	}
	transient := errors.New("database unavailable")
	if err := projectionReadError(transient); err != transient || errors.Is(err, errInvalidDatabase) {
		t.Fatalf("infrastructure error = %v", err)
	}
}

func TestProjectionMembers_RejectsLegacyAndMismatchedInventory(t *testing.T) {
	legacy := Inventory{Version: 1}
	if _, err := ProjectionMembers(legacy, legacy.IdentityHash()); err == nil {
		t.Fatal("accepted inventory without frozen membership")
	}
	current := Inventory{Version: 2}
	if _, err := ProjectionMembers(current, "different"); err == nil {
		t.Fatal("accepted mismatched inventory hash")
	}
}

func TestProjectionRecord_RequiresExactStoredProjection(t *testing.T) {
	header := releasechanges.Header{Version: 1, Country: "vn", Release: "20990101", Baseline: true}
	stored := corpusdb.ProjectionResult{Header: header, Digest: "digest", Counts: map[string]int{"document_added": 0}}
	record := BuildRecord{ChangesProjection: ProjectionDigest{Version: 1, SHA256: "digest"}, ChangesCounts: map[string]int{"document_added": 0}}
	if err := CompareProjection(record, header, stored); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*corpusdb.ProjectionResult){
		"digest":  func(p *corpusdb.ProjectionResult) { p.Digest = "changed" },
		"version": func(p *corpusdb.ProjectionResult) { p.Header.Version = 2 },
		"release": func(p *corpusdb.ProjectionResult) { p.Header.Release = "other" },
		"counts":  func(p *corpusdb.ProjectionResult) { p.Counts = map[string]int{"document_added": 1} },
	} {
		t.Run(name, func(t *testing.T) {
			changed := stored
			mutate(&changed)
			if err := CompareProjection(record, header, changed); err == nil {
				t.Fatal("accepted changed stored projection")
			}
		})
	}
}
