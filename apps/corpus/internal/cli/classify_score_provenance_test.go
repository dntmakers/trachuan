package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/classify"
	"danny.vn/trachuan/apps/corpus/internal/taxonomy"
)

func TestClassifyScoreRejectsUntrustedLabels(t *testing.T) {
	root := filepath.Join("..", "evaluation", "testdata", "vn")
	for _, tc := range []struct {
		name, file, want string
		alter            bool
	}{
		{"unpinned", "taxonomy-multi-family.json", "unpinned adjudicated labels", true},
		{"same-provider", "taxonomy-multi-family-anthropic.json", "classifier and labeler providers must differ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, tc.file))
			if err != nil {
				t.Fatal(err)
			}
			if tc.alter {
				data = append(data, ' ')
			}
			dir := t.TempDir()
			labels := filepath.Join(dir, "labels.json")
			if err = os.WriteFile(labels, data, 0600); err != nil {
				t.Fatal(err)
			}
			taxonomyHash := strings.Repeat("a", 64)
			recipe, err := classify.NewRecipe("claude-haiku-5-5", taxonomyHash)
			if err != nil {
				t.Fatal(err)
			}
			writeArtifact := func(name string, value any) (string, string) {
				t.Helper()
				artifactDir := filepath.Join(dir, name)
				if writeErr := os.Mkdir(artifactDir, 0700); writeErr != nil {
					t.Fatal(writeErr)
				}
				b, writeErr := json.Marshal(value)
				if writeErr != nil {
					t.Fatal(writeErr)
				}
				path := filepath.Join(artifactDir, name+".json")
				if writeErr = os.WriteFile(path, b, 0600); writeErr != nil {
					t.Fatal(writeErr)
				}
				hash := fmt.Sprintf("%x", sha256.Sum256(b))
				if writeErr = os.WriteFile(filepath.Join(artifactDir, "MANIFEST.sha256"), []byte(hash+"  "+name+".json\n"), 0600); writeErr != nil {
					t.Fatal(writeErr)
				}
				return path, hash
			}
			families, familyHash := writeArtifact("families", classify.FamilyArtifact{Version: 1, Recipe: recipe, RecipeHash: recipe.Hash(), TaxonomyHash: taxonomyHash, Documents: map[string]classify.Outcome{"vn:vbpl:1": {Outcome: "ok", Codes: []string{"1.1"}}}})
			articleRecipe, err := classify.NewArticleRecipe(recipe, familyHash)
			if err != nil {
				t.Fatal(err)
			}
			articles, _ := writeArtifact("articles", classify.ArticleArtifact{Version: 1, Country: "vn", Release: "20261115_3", Recipe: articleRecipe, RecipeHash: articleRecipe.Hash(), TaxonomyHash: taxonomyHash, FamiliesHash: familyHash, LabelsHash: fmt.Sprintf("%x", sha256.Sum256(data))})
			args := []string{"vn", "20261115_3", "--labels", labels, "--families", families, "--articles", articles, "--out", filepath.Join(dir, "out")}
			var stdout, stderr bytes.Buffer
			code := runClassifyScoreWith(context.Background(), args, &stdout, &stderr, func(context.Context, string, string, taxonomy.Labels) (classificationEvidence, error) {
				return classificationEvidence{taxonomyHash: taxonomyHash, nodes: []classify.Node{{Code: "1.1", Level: 2, Active: true, Origin: "phapdien"}}}, nil
			})
			if code != 1 || !strings.Contains(stderr.String(), tc.want) || stdout.Len() != 0 {
				t.Fatalf("exit %d stdout %s stderr %s", code, &stdout, &stderr)
			}
			if _, err = os.Stat(filepath.Join(dir, "out")); !os.IsNotExist(err) {
				t.Fatalf("output created: %v", err)
			}
		})
	}
}
