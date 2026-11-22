package eval

import (
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestInputHash_WarningFixture(t *testing.T) {
	s := New(Config{Suite: evaluation.Suite{WarningHash: "first"}})
	first, err := s.inputHash(artifact.Result{SHA256: "build"}, stage.Run{Rel: "20990101"})
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.Suite.WarningHash = "second"
	second, err := s.inputHash(artifact.Result{SHA256: "build"}, stage.Run{Rel: "20990101"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("eval input omitted warning fixture")
	}
}
