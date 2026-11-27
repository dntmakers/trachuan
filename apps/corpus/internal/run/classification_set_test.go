package run

import (
	"context"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestFreezeClassificationSet_NewRunRequiresChoice(t *testing.T) {
	snapshot := stage.Snapshot{}
	err := freezeClassificationSet(context.Background(), Deps{}, "vn", &snapshot, false)
	if err == nil || !strings.Contains(err.Error(), "--no-classification-set") {
		t.Fatalf("missing choice error = %v", err)
	}
}

func TestFreezeClassificationSet_ChoiceAndRestart(t *testing.T) {
	for _, tc := range []struct {
		name      string
		deps      Deps
		snapshot  stage.Snapshot
		existing  bool
		wantError bool
	}{
		{name: "both", deps: Deps{ClassificationSetSHA256: strings.Repeat("a", 64), NoClassificationSet: true}, wantError: true},
		{name: "none", deps: Deps{NoClassificationSet: true}},
		{name: "restart without choice", existing: true},
		{name: "restart cannot remove set", deps: Deps{NoClassificationSet: true}, snapshot: stage.Snapshot{ClassificationSetSHA256: strings.Repeat("a", 64)}, existing: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := freezeClassificationSet(context.Background(), tc.deps, "vn", &tc.snapshot, tc.existing)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, want error %v", err, tc.wantError)
			}
		})
	}
}
