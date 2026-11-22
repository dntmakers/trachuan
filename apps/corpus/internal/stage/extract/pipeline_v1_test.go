package extract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func TestV1PipelineRetainedTreeAndLegacy(t *testing.T) {
	ctx := context.Background()
	tree := `{"success":true,"data":[{"id":"a","level":"Article","title":"Điều 1. Phạm vi","content":{"content":"Điều 1. Phạm vi<br>Nội dung đầy đủ."}}]}`
	bundle := headingBundle(t, tree, false)
	artifacts := artifact.Store{Root: t.TempDir()}
	fetched, err := artifacts.PublishContent(ctx, "fetch", strings.Repeat("a", 64), func(dir string) error { return os.CopyFS(dir, os.DirFS(bundle)) })
	if err != nil {
		t.Fatal(err)
	}
	db := processFixtureDB(t, fetched)
	env := stage.Env{DB: db, Artifacts: artifacts}
	s := New()
	var outputs []stage.Output
	for _, contract := range []string{"", stage.ExtractRecordLegacy, stage.ExtractRecordV1} {
		run := stage.Run{ID: 1, Snapshot: stage.Snapshot{SourceList: []byte(`{}`), ExtractRecordContract: contract}}
		items, planErr := s.Plan(ctx, env, run)
		if planErr != nil || len(items) != 1 || items[0].InputHash != fetched.SHA256 {
			t.Fatalf("contract=%q items=%+v err=%v", contract, items, planErr)
		}
		out, processErr := s.Process(ctx, env, run, items[0])
		if processErr != nil || out.Artifact == nil || out.State != "done" {
			t.Fatalf("contract=%q out=%+v err=%v", contract, out, processErr)
		}
		path := filepath.Join(artifacts.Abs(out.Artifact.Path), "text.json")
		data, readErr := os.ReadFile(path)
		if contract != stage.ExtractRecordV1 {
			if !os.IsNotExist(readErr) {
				t.Fatalf("legacy text=%q err=%v", data, readErr)
			}
		} else {
			var record struct {
				Source   string `json:"text_source"`
				Detector struct {
					Version int `json:"version"`
					OCR     struct {
						State string `json:"state"`
					} `json:"ocr_check"`
					Tree struct {
						State string `json:"state"`
					} `json:"tree_transcription"`
				} `json:"detector_inputs"`
			}
			if readErr != nil || json.Unmarshal(data, &record) != nil || record.Source != "source_tree" || record.Detector.Version != 1 || record.Detector.OCR.State != "not_selected" || record.Detector.Tree.State != "no_pair" {
				t.Fatalf("v1 text=%s err=%v", data, readErr)
			}
		}
		outputs = append(outputs, out)
	}
	for _, name := range []string{"provisions.json", "report.json"} {
		before, readErr := os.ReadFile(filepath.Join(artifacts.Abs(outputs[0].Artifact.Path), name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		after, readErr := os.ReadFile(filepath.Join(artifacts.Abs(outputs[2].Artifact.Path), name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(before) != string(after) {
			t.Fatalf("%s changed", name)
		}
	}
}

func TestV1PipelineCheckedWithheld(t *testing.T) {
	ctx := context.Background()
	bundle, ocr, row, _, _ := processV1Fixture(t)
	if err := os.WriteFile(filepath.Join(bundle, "tree.json"), []byte(`{"success":true,"data":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	artifacts := artifact.Store{Root: t.TempDir()}
	fetched, err := artifacts.PublishContent(ctx, "fetch", strings.Repeat("a", 64), func(dir string) error { return os.CopyFS(dir, os.DirFS(bundle)) })
	if err != nil {
		t.Fatal(err)
	}
	row.RunID, row.ItemKey, row.FetchSha256 = 1, "vn:vbpl:1", &fetched.SHA256
	db := processFixtureDBWithRow(t, fetched, &row)
	env := stage.Env{DB: db, Artifacts: artifacts}
	run := stage.Run{ID: 1, Snapshot: stage.Snapshot{SourceList: []byte(`{}`), ExtractRecordContract: stage.ExtractRecordV1}}
	s := New().WithOCR(ocr, nil, "")
	items, err := s.Plan(ctx, env, run)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	selected, err := selectionFromRowV1(row, fetched.SHA256)
	if err != nil || items[0].InputHash != observationInputHash(fetched.SHA256, selected) {
		t.Fatalf("items=%+v selected=%+v err=%v", items, selected, err)
	}
	out, err := s.Process(ctx, env, run, items[0])
	if err != nil || out.Artifact == nil {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	data, err := os.ReadFile(filepath.Join(artifacts.Abs(out.Artifact.Path), "text.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		TextSource string `json:"text_source"`
		Detector   struct {
			OCR struct {
				State     string `json:"state"`
				Verdict   string `json:"verdict"`
				LineCount int    `json:"line_count"`
			} `json:"ocr_check"`
		} `json:"detector_inputs"`
	}
	if err = json.Unmarshal(data, &record); err != nil || record.TextSource != "none" || record.Detector.OCR.State != "checked" || record.Detector.OCR.Verdict != "rejected" || record.Detector.OCR.LineCount != 2 || strings.Contains(string(data), "OCR text") {
		t.Fatalf("record=%s err=%v", data, err)
	}
	if err = fetch.CheckExtractLineageContract(artifacts.Abs(out.Artifact.Path), fetched.SHA256, items[0].InputHash, stage.ExtractRecordV1); err != nil {
		t.Fatal(err)
	}
	legacyRun := run
	legacyRun.Snapshot.ExtractRecordContract = stage.ExtractRecordLegacy
	legacy, err := s.Process(ctx, env, legacyRun, stage.Item{Key: items[0].Key, InputHash: selectionInputHash(fetched.SHA256, selected)})
	if err != nil || legacy.Artifact == nil {
		t.Fatalf("legacy=%+v err=%v", legacy, err)
	}
	for _, name := range []string{"provisions.json", "report.json"} {
		before, readErr := os.ReadFile(filepath.Join(artifacts.Abs(legacy.Artifact.Path), name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		after, readErr := os.ReadFile(filepath.Join(artifacts.Abs(out.Artifact.Path), name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(before) != string(after) {
			t.Fatalf("%s changed", name)
		}
	}
}

func TestV1RecipeContractAndUnknown(t *testing.T) {
	s := New()
	legacy := s.Recipe(stage.Snapshot{})
	if s.Recipe(stage.Snapshot{ExtractRecordContract: stage.ExtractRecordLegacy}) != legacy {
		t.Fatal("explicit legacy changed recipe")
	}
	parts := append([]string{"html-transcription/1", "textparse/8", "heading-only/1", "ocr-store/2", ReleaseChecker}, AcceptedEngines()...)
	parts = append(parts, manualMismatchRecipe()...)
	parts = append(parts, stage.ExtractRecordV1)
	if got, want := s.Recipe(stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1}), stage.RecipeHash("extract", "tree/1", parts...); got != want || got == legacy {
		t.Fatalf("v1 recipe=%s want=%s legacy=%s", got, want, legacy)
	}
	run := stage.Run{Snapshot: stage.Snapshot{ExtractRecordContract: "unexpected"}}
	if s.Recipe(run.Snapshot) != "" {
		t.Fatal("unknown contract produced a recipe")
	}
	if _, err := s.Plan(context.Background(), stage.Env{}, run); err == nil {
		t.Fatal("plan accepted unknown contract")
	}
	if _, err := s.Process(context.Background(), stage.Env{}, run, stage.Item{}); err == nil {
		t.Fatal("process accepted unknown contract")
	}
}

func TestV1PipelineTerminalTreeGap(t *testing.T) {
	ctx := context.Background()
	tree := `{"success":true,"data":[` + strings.Repeat(`{"children":[`, 33) + strings.Repeat(`]}`, 33) + `]}`
	artifacts := artifact.Store{Root: t.TempDir()}
	fetched, err := artifacts.PublishContent(ctx, "fetch", strings.Repeat("a", 64), func(dir string) error { return os.WriteFile(filepath.Join(dir, "tree.json"), []byte(tree), 0600) })
	if err != nil {
		t.Fatal(err)
	}
	env := stage.Env{DB: processFixtureDB(t, fetched), Artifacts: artifacts}
	run := stage.Run{ID: 1, Snapshot: stage.Snapshot{SourceList: []byte(`{}`), ExtractRecordContract: stage.ExtractRecordV1}}
	s := New()
	items, err := s.Plan(ctx, env, run)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	out, err := s.Process(ctx, env, run, items[0])
	if err != nil || out.State != "gap" || out.Artifact != nil || len(out.Gaps) != 1 || out.Gaps[0].Kind != "tree_too_large" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestV1PipelineMeasuredTranscription(t *testing.T) {
	ctx := context.Background()
	bundle := transcriptionBundle(t, partialTree, partialHTML())
	artifacts := artifact.Store{Root: t.TempDir()}
	fetched, err := artifacts.PublishContent(ctx, "fetch", strings.Repeat("a", 64), func(dir string) error { return os.CopyFS(dir, os.DirFS(bundle)) })
	if err != nil {
		t.Fatal(err)
	}
	env := stage.Env{DB: processFixtureDB(t, fetched), Artifacts: artifacts}
	run := stage.Run{ID: 1, Snapshot: stage.Snapshot{SourceList: []byte(`{}`), ExtractRecordContract: stage.ExtractRecordV1}}
	s := New()
	items, err := s.Plan(ctx, env, run)
	if err != nil || len(items) != 1 || items[0].InputHash != InputHash(fetched.SHA256, "html_transcription") {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	out, err := s.Process(ctx, env, run, items[0])
	if err != nil || out.Artifact == nil {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	data, err := os.ReadFile(filepath.Join(artifacts.Abs(out.Artifact.Path), "text.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Source   string `json:"text_source"`
		Detector struct {
			Tree struct {
				State        string `json:"state"`
				MainAccepted bool   `json:"main_accepted"`
				TreeChars    int    `json:"tree_chars"`
			} `json:"tree_transcription"`
		} `json:"detector_inputs"`
	}
	if err = json.Unmarshal(data, &record); err != nil || record.Source != "html_transcription" || record.Detector.Tree.State != "measured" || !record.Detector.Tree.MainAccepted || record.Detector.Tree.TreeChars <= 0 {
		t.Fatalf("record=%s err=%v", data, err)
	}
	if err = fetch.CheckExtractLineageContract(artifacts.Abs(out.Artifact.Path), fetched.SHA256, items[0].InputHash, stage.ExtractRecordV1); err != nil {
		t.Fatal(err)
	}
}
