package eval

import (
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/evaluation"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

func TestEvalRecipe_ClassificationRejectsOldWorker(t *testing.T) {
	s := New(Config{})
	old := stage.RecipeHash("eval", evaluation.ScorerVersion, "legacy-matcher/2", "product-matcher/1", "recall,mrr,current-law-precision,abstention,evidence,ann/1", "", "diacritics_required:Vui lòng nhập câu hỏi tiếng Việt có dấu hoặc số hiệu văn bản.", "cache/1:200:100", "nearest-rank", "gates:0.99;targets:recall20=0.90,mrr8=0.66,recall8=0.90,abstention-balanced=0.85;nonregression:recall20,mrr8,abstention-balanced", "compact/1", "corpus-text-counts/1", "filter-gate/1", "inheritance-gate/2", "warning-cases/1")
	if got := s.Recipe(stage.Snapshot{}); got != old {
		t.Fatalf("legacy recipe = %q, want %q", got, old)
	}
	if got := s.Recipe(stage.Snapshot{ClassificationSetSHA256: strings.Repeat("a", 64)}); got == old {
		t.Fatal("opted-in run uses the old worker's recipe")
	}
}

func TestEvalRecipe_ClassificationBindsFloors(t *testing.T) {
	const floorsSHA256 = "92577289c5d78d83d5b8f298ddf1c566f4edee7c574e392cee497c9f811eaf6a"
	s := New(Config{})
	legacy := s.Recipe(stage.Snapshot{})
	want := stage.RecipeHash("eval-classification-gate", "2", legacy, floorsSHA256)
	if got := s.Recipe(stage.Snapshot{ClassificationSetSHA256: strings.Repeat("a", 64)}); got != want {
		t.Fatalf("opted-in recipe = %q, want %q with the frozen floor table", got, want)
	}
}
