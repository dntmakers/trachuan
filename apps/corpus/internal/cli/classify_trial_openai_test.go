package cli

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOpenAITrial_DecimalCapAndStage(t *testing.T) {
	for _, tt := range []struct {
		stage, model, effort, cap string
		micro                     int64
	}{
		{"documents", "gpt-6.2-sol", "medium", "40", 40000000},
		{"documents", "gpt-6-luna", "low", "0.000001", 1},
		{"documents", "gpt-6.1-terra", "medium", "1.25", 1250000},
		{"documents", "gpt-6.2-sol", "medium", "40.000001", 0},
		{"documents", "gpt-6.2-sol", "medium", "0.0000004", 0},
		{"documents", "gpt-6.2-sol", "medium", "1e1", 0},
		{"documents", "gpt-6.2-sol", "none", "1", 0},
		{"documents", "gpt-6.2-astra", "medium", "1", 0},
		{"articles", "gpt-6.2-sol", "medium", "1", 0},
		{"wrong", "gpt-6.2-sol", "medium", "1", 0},
	} {
		args := []string{"vn", "20261115_3", "--stage", tt.stage, "--select", "labels:labels.json", "--model", tt.model, "--effort", tt.effort, "--out", "trial", "--cap-usd", tt.cap, "--dry-run"}
		got, err := parseOpenAITrial(args)
		if tt.micro == 0 && err == nil || tt.micro != 0 && (err != nil || got.trial.CapMicroUSD != tt.micro || !got.trial.DryRun || got.labels != "labels.json") {
			t.Fatalf("flags %v: got %+v, %v", args, got, err)
		}
	}
	if code := runClassify(t.Context(), []string{"trial-openai"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("missing flags exit code = %d", code)
	}
}

func TestOpenAITrialOutput_RejectsSubmissionTree(t *testing.T) {
	root := t.TempDir()
	submissions := filepath.Join(root, "submissions")
	for _, path := range []string{submissions, filepath.Join(submissions, "trial"), root} {
		if _, err := openAITrialOutput(path, submissions); err == nil {
			t.Fatalf("allowed %s to overlap submission directory", path)
		}
	}
	if _, err := openAITrialOutput(filepath.Join(root, "trials", "one"), submissions); err != nil {
		t.Fatal(err)
	}
}

func TestParseOpenAITrial_ArticlesRequireFamilies(t *testing.T) {
	args := []string{"vn", "20261115_3", "--stage", "articles", "--select", "labels:labels.json", "--families", "families.json", "--model", "gpt-6.2-sol", "--effort", "medium", "--out", "trial", "--cap-usd", "40"}
	got, err := parseOpenAITrial(args)
	if err != nil || got.families != "families.json" || got.stage != "articles" {
		t.Fatalf("article flags: %+v, %v", got, err)
	}
}

func TestOpenAITrialCLI_RejectsProtectedTrees(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fixture-credential")
	t.Setenv("OPENAI_PROJECT_ID", "fixture_project")
	root := t.TempDir()
	for _, env := range []string{"CORPUS_CHECKPOINT_DIR", "CORPUS_ARTIFACTS"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, root)
			args := []string{"trial-openai", "vn", "20261115_3", "--stage", "documents", "--select", "labels:labels.json", "--model", "gpt-6.2-sol", "--effort", "medium", "--out", filepath.Join(root, "child"), "--cap-usd", "40"}
			if code := runClassify(t.Context(), args, io.Discard, io.Discard); code != 2 {
				t.Fatalf("protected output returned %d", code)
			}
		})
	}
}

func TestOpenAITrialCLI_RejectsNonIndependentLabelsBeforeInputs(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "fixture-credential")
	t.Setenv("OPENAI_PROJECT_ID", "fixture_project")
	t.Setenv("CORPUS_DATABASE_URL", "")
	labels := filepath.Join("..", "evaluation", "testdata", "vn", "taxonomy-multi-family.json")
	args := []string{"trial-openai", "vn", "20261115_3", "--stage", "documents", "--select", "labels:" + labels, "--model", "gpt-6.2-sol", "--effort", "medium", "--out", filepath.Join(t.TempDir(), "trial"), "--cap-usd", "40"}
	var stderr bytes.Buffer
	if code := runClassify(t.Context(), args, io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "classifier and labeler providers must differ") {
		t.Fatalf("expected provider error, got %d: %s", code, &stderr)
	}
}
