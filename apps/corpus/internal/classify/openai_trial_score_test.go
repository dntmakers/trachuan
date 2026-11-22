package classify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAITrialScore_PreservesProviderIndependence(t *testing.T) {
	root := filepath.Join("..", "evaluation", "testdata", "vn")
	for _, tt := range []struct{ file, slice string }{
		{"taxonomy-labels.json", "seeded"},
		{"taxonomy-multi-family-anthropic.json", "multi-family"},
		{"taxonomy-post-2023-anthropic.json", "post-2023"},
		{"taxonomy-multi-family.json", ""},
		{"taxonomy-post-2023.json", ""},
	} {
		t.Run(tt.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			for _, model := range []string{"gpt-6.2-sol", "gpt-6.1-terra", "gpt-6-luna"} {
				got, err := VerifyScoreLabels(Recipe{Model: model, Provider: "openai"}, raw)
				if got != tt.slice || (err == nil) != (tt.slice != "") {
					t.Errorf("%s: got %q, %v; want %q", model, got, err, tt.slice)
				}
			}
			for _, invalid := range []Recipe{{Model: "gpt-6.2-sol"}, {Model: "gpt-6.2-sol", Provider: "anthropic"}, {Model: "claude-haiku-5-5", Provider: "openai"}, {Model: "claude-haiku-5-5", Provider: "unknown"}} {
				if _, err := VerifyScoreLabels(invalid, raw); err == nil {
					t.Errorf("accepted invalid provider/model pair %+v", invalid)
				}
			}
		})
	}
}
