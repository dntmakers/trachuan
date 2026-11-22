package taxonomy

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyAdjudicated(t *testing.T) {
	root := filepath.Join("..", "evaluation", "testdata", "vn")
	for _, slice := range []string{"multi-family", "post-2023"} {
		original, err := os.ReadFile(filepath.Join(root, "taxonomy-"+slice+".json"))
		if err != nil {
			t.Fatal(err)
		}
		derived, err := os.ReadFile(filepath.Join(root, "taxonomy-"+slice+"-anthropic.json"))
		if err != nil {
			t.Fatal(err)
		}
		gotSlice, provider, labels, err := VerifyAdjudicated("anthropic", original)
		if err != nil || gotSlice != slice || provider != "openai" {
			t.Fatalf("original %s: %s %s %v", slice, gotSlice, provider, err)
		}
		gotSlice, provider, derivedLabels, err := VerifyAdjudicated("openai", derived)
		if err != nil || gotSlice != slice || provider != "anthropic" {
			t.Fatalf("derived %s: %s %s %v", slice, gotSlice, provider, err)
		}
		originalHash, err := SourceListSHA256(labels.Articles)
		if err != nil {
			t.Fatal(err)
		}
		derivedHash, err := SourceListSHA256(derivedLabels.Articles)
		if err != nil {
			t.Fatal(err)
		}
		if originalHash != derivedHash || originalHash != derivedLabels.SourceListSHA256 {
			t.Fatalf("%s source mismatch", slice)
		}
		mismatch := bytes.Replace(derived, []byte(derivedLabels.SourceListSHA256), bytes.Repeat([]byte("0"), 64), 1)
		if _, _, _, err = VerifyAdjudicated("openai", mismatch); err == nil {
			t.Fatal("accepted derived file with changed source hash")
		}
		if _, _, _, err := VerifyAdjudicated("anthropic", derived); err == nil {
			t.Fatal("accepted same provider")
		}
		changed := append(append([]byte(nil), original...), ' ')
		if _, _, _, err := VerifyAdjudicated("anthropic", changed); err == nil {
			t.Fatal("accepted altered file")
		}
	}
	if _, _, _, err := VerifyAdjudicated("anthropic", []byte(`{}`)); err == nil {
		t.Fatal("accepted unpinned file")
	}
}

func TestVerifyScoringInputRejectsUnknownClassifierProvider(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "evaluation", "testdata", "vn", "taxonomy-multi-family.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"", "Anthropic", "OPENAI", "unknown"} {
		t.Run(provider, func(t *testing.T) {
			if _, _, _, err := VerifyScoringInput(provider, data); err == nil {
				t.Fatalf("accepted classifier provider %q", provider)
			}
		})
	}
}
