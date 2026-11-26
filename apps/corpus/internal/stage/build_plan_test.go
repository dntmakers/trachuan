package stage

import (
	"errors"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/store"
)

func TestBuildReplacement_RequiresCurrentBuildStage(t *testing.T) {
	build, eval := "build", "eval"
	for _, tc := range []struct {
		state   string
		next    *string
		allowed bool
	}{
		{"running", &build, true}, {"evaluated", &build, false}, {"released", &build, false}, {"running", &eval, false}, {"running", nil, false},
	} {
		err := checkBuildReplacement(store.Run{State: tc.state, NextStage: tc.next})
		var refusal *BuildReplacementError
		if tc.allowed && err != nil || !tc.allowed && !errors.As(err, &refusal) {
			t.Fatalf("state=%s next=%v err=%v", tc.state, tc.next, err)
		}
	}
}
