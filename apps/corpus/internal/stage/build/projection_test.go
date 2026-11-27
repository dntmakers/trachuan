package build

import (
	"errors"
	"maps"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
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
	header := releasechanges.Header{Version: releasechanges.Version, Country: "vn", Release: "20990101", Baseline: true}
	stored := corpusdb.ProjectionResult{Header: header, Digest: "digest", Counts: map[string]int{"document_added": 0}}
	record := BuildRecord{ChangesProjection: ProjectionDigest{Version: releasechanges.Version, SHA256: "digest"}, ChangesCounts: map[string]int{"document_added": 0}}
	if err := CompareProjection(record, header, stored); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*corpusdb.ProjectionResult){
		"digest":  func(p *corpusdb.ProjectionResult) { p.Digest = "changed" },
		"version": func(p *corpusdb.ProjectionResult) { p.Header.Version = 1 },
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

func TestValidateProjectionRecord_ExactVersionAndCounts(t *testing.T) {
	run := stage.Run{Country: "vn", Rel: "20990101", Snapshot: stage.Snapshot{LockedPredecessor: &stage.LockedPredecessor{Release: stage.NoPreviousReport, IdentitySHA256: stage.NoPreviousReport}}}
	header, err := ProjectionHeader(run)
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := releasechanges.NewHasher(header)
	if err != nil {
		t.Fatal(err)
	}
	good := BuildRecord{ChangesProjection: ProjectionDigest{Version: releasechanges.Version, SHA256: hasher.Sum()}, ChangesCounts: hasher.Counts()}
	if err := validateProjectionRecord(good, run); err != nil {
		t.Fatal(err)
	}
	if _, ok := good.ChangesCounts["provision_validity"]; ok {
		t.Fatal("counts carry provision_validity")
	}
	oldVersion := good
	oldVersion.ChangesProjection.Version = 1
	withKind := BuildRecord{ChangesProjection: good.ChangesProjection, ChangesCounts: maps.Clone(good.ChangesCounts)}
	withKind.ChangesCounts["provision_validity"] = 0
	for name, record := range map[string]BuildRecord{"version 1": oldVersion, "provision_validity count": withKind} {
		if err := validateProjectionRecord(record, run); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
}
