package stage

import (
	"context"
	"errors"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
)

type syntheticReuseStage struct {
	Stage
	err   error
	calls *int
}

func (s syntheticReuseStage) VerifyReuse(context.Context, Env, Run, Item, artifact.Result) error {
	*s.calls++
	return s.err
}

func TestReuseOpenedArtifactRequiresAuthority(t *testing.T) {
	calls := 0
	failure := errors.New("corrupt association")
	art := artifact.Result{Path: "cached"}
	out, err := reuseOpenedArtifact(context.Background(), Env{}, syntheticReuseStage{err: failure, calls: &calls}, Run{}, Item{}, art)
	if !errors.Is(err, failure) || out.Artifact != nil || calls != 1 {
		t.Fatalf("out=%+v err=%v calls=%d", out, err, calls)
	}
	calls = 0
	out, err = reuseOpenedArtifact(context.Background(), Env{}, syntheticReuseStage{calls: &calls}, Run{}, Item{}, art)
	if err != nil || out.Artifact == nil || out.State != "done" || calls != 1 {
		t.Fatalf("out=%+v err=%v calls=%d", out, err, calls)
	}
	out, err = reuseOpenedArtifact(context.Background(), Env{}, struct{ Stage }{}, Run{}, Item{}, art)
	if err != nil || out.Artifact == nil || out.State != "done" {
		t.Fatalf("ordinary stage out=%+v err=%v", out, err)
	}
}
