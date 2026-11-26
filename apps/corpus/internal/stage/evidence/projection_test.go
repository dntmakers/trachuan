package evidence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/build"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
	"danny.vn/trachuan/internal/corpusdb"
	"danny.vn/trachuan/internal/releasechanges"
)

func TestProjectionReadError_InvalidVersusInfrastructure(t *testing.T) {
	bad := &corpusdb.InvalidProjectionError{Err: errors.New("bad digest")}
	var refusal *releasechange.InputError
	if err := projectionReadError(bad); !errors.As(err, &refusal) {
		t.Fatalf("invalid projection error = %v", err)
	}
	transient := errors.New("database unavailable")
	if err := projectionReadError(transient); err != transient || errors.As(err, &refusal) {
		t.Fatalf("infrastructure error = %v", err)
	}
}

func TestCheckProjection_RejectsMismatchedRecordBeforeDatabase(t *testing.T) {
	dir := t.TempDir()
	run := stage.Run{Country: "vn", Rel: "20990101", Snapshot: stage.Snapshot{LockedPredecessor: &stage.LockedPredecessor{ReportSHA256: "none", Release: "none", IdentitySHA256: "none"}}}
	header, err := build.ProjectionHeader(run)
	if err != nil {
		t.Fatal(err)
	}
	h, err := releasechanges.NewHasher(header)
	if err != nil {
		t.Fatal(err)
	}
	record := build.BuildRecord{SchemaVersion: 9, ChangesProjection: build.ProjectionDigest{Version: 1, SHA256: strings.Repeat("0", 64)}, ChangesCounts: h.Counts()}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "build.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	change := releasechange.ChangesHeader{Schema: 1, Baseline: true}
	raw, err = json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, changesFile), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	err = New(Config{}).checkProjection(context.Background(), stage.Env{}, run, frozenInputs{buildDir: dir}, dir)
	var refusal *releasechange.InputError
	if !errors.As(err, &refusal) || !strings.Contains(err.Error(), "sidecar projection") {
		t.Fatalf("error=%v", err)
	}
}
