package evidence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
)

const testDetectorText = `{"text_source":"source_tree","detector_inputs":{"version":1,"ocr_check":{"state":"not_selected"},"tree_transcription":{"state":"no_pair"}}}`

type detectorReaderFixture struct {
	store     artifact.Store
	extract   artifact.Result
	build     artifact.Result
	inventory map[string]any
	doc       releaseDocument
	recipe    string
}

func detectorReaderSetup(t *testing.T, text string) *detectorReaderFixture {
	t.Helper()
	f := &detectorReaderFixture{store: artifact.Store{Root: t.TempDir()}, recipe: strings.Repeat("a", 64)}
	var err error
	f.extract, err = f.store.Publish(context.Background(), "extract", strings.Repeat("b", 64), f.recipe, func(dir string) error {
		if text == "" {
			return nil
		}
		return os.WriteFile(filepath.Join(dir, "text.json"), []byte(text), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	f.doc = releaseDocument{ID: 1, Key: "vn:vbpl:1", Source: "vbpl", TextSource: "source_tree", ExtractSHA256: f.extract.SHA256}
	f.inventory = map[string]any{"version": 2, "country": "vn", "expected_keys": []string{f.doc.Key}, "items": []map[string]any{
		{"stage": "fetch", "item_key": f.doc.Key, "state": "done", "input_hash": strings.Repeat("c", 64), "recipe_hash": strings.Repeat("d", 64), "artifact_sha256": strings.Repeat("e", 64)},
		{"stage": "extract", "item_key": f.doc.Key, "state": "done", "input_hash": strings.Repeat("b", 64), "recipe_hash": f.recipe, "artifact_sha256": f.extract.SHA256},
	}, "gaps": []any{}}
	return f
}

func (f *detectorReaderFixture) publishBuild(t *testing.T) {
	t.Helper()
	data, err := json.Marshal(f.inventory)
	if err != nil {
		t.Fatal(err)
	}
	f.build, err = f.store.Publish(context.Background(), "build", bareSHA256(data), strings.Repeat("9", 64), func(dir string) error { return os.WriteFile(filepath.Join(dir, "inputs.json"), data, 0600) })
	if err != nil {
		t.Fatal(err)
	}
}

func (f *detectorReaderFixture) read(t *testing.T) ([]detectorInput, error) {
	t.Helper()
	in := Inputs{Run: stage.Run{Country: "vn"}, BuildDir: f.store.Abs(f.build.Path), BuildSHA256: f.build.SHA256, ArtifactDir: func(stageName, key string) (string, bool, error) {
		if stageName == "extract" && key == f.doc.Key {
			return f.store.Abs(f.extract.Path), true, nil
		}
		return "", false, nil
	}}
	return loadDetectorInputs(context.Background(), in, &fakeRelease{documents: []releaseDocument{f.doc}}, f.recipe)
}

func TestDetectorReaderVerifiedArtifact(t *testing.T) {
	f := detectorReaderSetup(t, testDetectorText)
	f.publishBuild(t)
	got, err := f.read(t)
	if err != nil || len(got) != 1 || !got[0].Observable || got[0].Document.Key != f.doc.Key || got[0].Detector == nil || got[0].Detector.Version != 1 || got[0].Detector.OCRCheck.State != "not_selected" || got[0].Detector.TreeTranscription.State != "no_pair" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestDetectorReaderRejectsMismatch(t *testing.T) {
	for _, change := range []string{"recipe", "digest", "source", "tampered", "missing-record", "missing-path", "build-tampered"} {
		t.Run(change, func(t *testing.T) {
			text := testDetectorText
			if change == "missing-record" {
				text = ""
			}
			f := detectorReaderSetup(t, text)
			if change == "recipe" {
				f.inventory["items"].([]map[string]any)[1]["recipe_hash"] = strings.Repeat("0", 64)
			}
			if change == "digest" {
				f.doc.ExtractSHA256 = strings.Repeat("0", 64)
			}
			if change == "source" {
				f.doc.TextSource = "ocr"
			}
			f.publishBuild(t)
			if change == "tampered" {
				if err := os.WriteFile(filepath.Join(f.store.Abs(f.extract.Path), "text.json"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if change == "missing-path" {
				if err := os.RemoveAll(f.store.Abs(f.extract.Path)); err != nil {
					t.Fatal(err)
				}
			}
			if change == "build-tampered" {
				if err := os.WriteFile(filepath.Join(f.store.Abs(f.build.Path), "inputs.json"), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := f.read(t)
			if err == nil || len(got) != 0 {
				t.Fatalf("accepted %s: %+v err=%v", change, got, err)
			}
			if _, ok := err.(*IncompleteError); !ok {
				t.Fatalf("error type %T: %v", err, err)
			}
		})
	}
}

func TestDetectorReaderBaselineAndGap(t *testing.T) {
	for _, kind := range []string{"baseline", "fetch-gap", "extract-gap"} {
		t.Run(kind, func(t *testing.T) {
			f := detectorReaderSetup(t, testDetectorText)
			switch kind {
			case "baseline":
				f.doc.Baseline = true
			case "fetch-gap":
				f.doc.ExtractSHA256 = ""
				f.doc.TextSource = "none"
				f.inventory["items"] = []map[string]any{{"stage": "fetch", "item_key": f.doc.Key, "state": "gap"}}
				f.inventory["gaps"] = []map[string]any{{"stage": "fetch", "item_key": f.doc.Key, "kind": "missing"}}
			case "extract-gap":
				f.doc.ExtractSHA256 = ""
				f.doc.TextSource = "none"
				f.inventory["items"].([]map[string]any)[1] = map[string]any{"stage": "extract", "item_key": f.doc.Key, "state": "gap"}
				f.inventory["gaps"] = []map[string]any{{"stage": "extract", "item_key": f.doc.Key, "kind": "tree_too_large"}}
			}
			f.publishBuild(t)
			got, err := f.read(t)
			if err != nil || len(got) != 1 || got[0].Observable || got[0].Detector != nil {
				t.Fatalf("kind=%s got=%+v err=%v", kind, got, err)
			}
		})
	}
}

func TestDetectorReaderRejectsAmbiguousInventory(t *testing.T) {
	for _, change := range []string{"duplicate-item", "duplicate-key", "missing-extract", "implicit-gap", "malformed-v1", "wrong-tree-source"} {
		t.Run(change, func(t *testing.T) {
			text := testDetectorText
			if change == "malformed-v1" {
				text = strings.Replace(testDetectorText, `"version":1`, `"version":null`, 1)
			}
			if change == "wrong-tree-source" {
				text = strings.Replace(testDetectorText, `"no_pair"`, `"not_applicable"`, 1)
			}
			f := detectorReaderSetup(t, text)
			items := f.inventory["items"].([]map[string]any)
			switch change {
			case "duplicate-item":
				f.inventory["items"] = append(items, items[1])
			case "duplicate-key":
				f.inventory["expected_keys"] = []string{f.doc.Key, f.doc.Key}
			case "missing-extract":
				f.inventory["items"] = items[:1]
			case "implicit-gap":
				f.doc.ExtractSHA256 = ""
				f.doc.TextSource = "none"
				items[1] = map[string]any{"stage": "extract", "item_key": f.doc.Key, "state": "gap"}
			}
			f.publishBuild(t)
			if got, err := f.read(t); err == nil || len(got) != 0 {
				t.Fatalf("change=%s got=%+v err=%v", change, got, err)
			}
		})
	}
}

func TestDetectorReaderNoPartialSuccess(t *testing.T) {
	f := detectorReaderSetup(t, testDetectorText)
	f.publishBuild(t)
	other := releaseDocument{ID: 2, Key: "vn:vbpl:2", Source: "vbpl", TextSource: "source_tree"}
	in := Inputs{Run: stage.Run{Country: "vn"}, Build: func() (string, string, error) { return f.store.Abs(f.build.Path), f.build.SHA256, nil }, ArtifactDir: func(stageName, key string) (string, bool, error) { return f.store.Abs(f.extract.Path), true, nil }}
	got, err := loadDetectorInputs(context.Background(), in, &fakeRelease{documents: []releaseDocument{f.doc, other}}, f.recipe)
	if err == nil || len(got) != 0 {
		t.Fatalf("partial result=%+v err=%v", got, err)
	}
}

func TestDetectorReaderRejectsTreeSourceForNonVBPL(t *testing.T) {
	text := strings.Replace(testDetectorText, `"no_pair"`, `"not_applicable"`, 1)
	f := detectorReaderSetup(t, text)
	f.doc.Key, f.doc.Source = "vn:chinhphu:1", "chinhphu"
	f.inventory["expected_keys"] = []string{f.doc.Key}
	for _, item := range f.inventory["items"].([]map[string]any) {
		item["item_key"] = f.doc.Key
	}
	f.publishBuild(t)
	if got, err := f.read(t); err == nil || len(got) != 0 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}
