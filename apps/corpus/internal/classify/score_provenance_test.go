package classify

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestScoreLabelProvenance(t *testing.T) {
	root := filepath.Join("..", "evaluation", "testdata", "vn")
	for _, slice := range []string{"multi-family", "post-2023"} {
		b, err := os.ReadFile(filepath.Join(root, "taxonomy-"+slice+".json"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := VerifyScoreLabels(Recipe{Model: "claude-haiku-5-5"}, b)
		if err != nil || got != slice {
			t.Fatalf("%s: %s %v", slice, got, err)
		}
		if _, err = VerifyScoreLabels(Recipe{Model: "claude-haiku-5-5"}, append(b, ' ')); err == nil {
			t.Fatal("accepted changed labels")
		}
		relabeled := bytes.Replace(b, []byte("model-adjudicated/1"), []byte("seeded-articles/2"), 1)
		if _, err = VerifyScoreLabels(Recipe{Model: "claude-haiku-5-5"}, relabeled); err == nil {
			t.Fatal("accepted adjudicated labels relabeled as seeded")
		}
		b, err = os.ReadFile(filepath.Join(root, "taxonomy-"+slice+"-anthropic.json"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = VerifyScoreLabels(Recipe{Model: "claude-haiku-5-5"}, b); err == nil {
			t.Fatal("accepted same provider")
		}
	}
	seeded, err := os.ReadFile(filepath.Join(root, "taxonomy-labels.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := VerifyScoreLabels(Recipe{Model: "claude-haiku-5-5"}, seeded); err != nil || got != "seeded" {
		t.Fatalf("seeded: %s %v", got, err)
	}
	if _, err := VerifyScoreLabels(Recipe{Model: "claude-haiku-5-5"}, []byte(`{}`)); err == nil {
		t.Fatal("accepted unknown draw rule")
	}
}
