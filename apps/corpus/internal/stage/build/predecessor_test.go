package build

import (
	"context"
	"errors"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
)

func TestPlanInTransaction_RejectsChangedPredecessor(t *testing.T) {
	run := stage.Run{Snapshot: stage.Snapshot{PreviousReport: &stage.PreviousReport{SHA256: "none"}, LockedPredecessor: &stage.LockedPredecessor{ReportSHA256: "none", Release: "other", IdentitySHA256: "none"}}}
	_, err := New(Config{}).PlanInTransaction(context.Background(), stage.Env{}, nil, run)
	var refusal *releasechange.InputError
	if !errors.As(err, &refusal) {
		t.Fatalf("expected deterministic refusal, got %v", err)
	}
}

func TestPredecessorInputHash_BindsReleaseAndIdentity(t *testing.T) {
	original := stage.LockedPredecessor{ReportSHA256: "report", Release: "r1", IdentitySHA256: "identity"}
	hash := predecessorInputHash("input", original)
	changed := original
	changed.Release = "r2"
	if hash == predecessorInputHash("input", changed) {
		t.Fatal("release did not change hash")
	}
	changed = original
	changed.IdentitySHA256 = "other"
	if hash == predecessorInputHash("input", changed) {
		t.Fatal("identity did not change hash")
	}
}
