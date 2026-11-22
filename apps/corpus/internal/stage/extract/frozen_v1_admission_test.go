package extract

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
)

func TestV1AdmissionRejectsDifferentDerivedObservation(t *testing.T) {
	for _, field := range []string{"line_count", "other_items", "same_document"} {
		t.Run(field, func(t *testing.T) {
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
			fetched.Path = artifacts.Abs(fetched.Path)
			row.RunID, row.ItemKey, row.FetchSha256 = 1, "vn:vbpl:1", &fetched.SHA256
			selected, err := selectionFromRowV1(row, fetched.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			item := stage.Item{Key: row.ItemKey, InputHash: observationInputHash(fetched.SHA256, selected)}
			run := stage.Run{ID: 1, Snapshot: stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1}}
			db := processFixtureDBWithRow(t, fetched, &row)
			env := stage.Env{DB: db, Artifacts: artifacts}
			counted := &countedAuthorityOCR{ocrReader: ocr}
			s := New().WithOCR(ocr, []string{"engine"}, "checker")
			s.ocr = counted
			original, err := s.Process(ctx, env, run, item)
			if err != nil || original.Artifact == nil {
				t.Fatalf("original=%+v err=%v", original, err)
			}
			if err = s.VerifyReuse(ctx, env, run, item, *original.Artifact); err != nil {
				t.Fatalf("valid reuse: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(artifacts.Abs(original.Artifact.Path), "text.json"))
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]any
			if err = json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			check := record["detector_inputs"].(map[string]any)["ocr_check"].(map[string]any)
			switch field {
			case "line_count":
				check["line_count"] = float64(3)
			case "other_items":
				check["other_items"] = float64(1)
			case "same_document":
				check["same_document"], check["has_missing_text"], check["other_items"] = true, true, float64(1)
			}
			forged, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = fetch.VerifyExtractLineageV1Bytes(forged, fetched.SHA256, item.InputHash); err != nil {
				t.Fatalf("forged observation did not preserve valid lineage: %v", err)
			}
			other := artifact.Store{Root: t.TempDir()}
			cached, err := other.Publish(ctx, "extract", item.InputHash, s.Recipe(run.Snapshot), func(dir string) error {
				if writeErr := os.WriteFile(filepath.Join(dir, "text.json"), forged, 0600); writeErr != nil {
					return writeErr
				}
				if writeErr := os.WriteFile(filepath.Join(dir, "report.json"), []byte(`[]`), 0600); writeErr != nil {
					return writeErr
				}
				return os.WriteFile(filepath.Join(dir, "provisions.json"), []byte(`[]`), 0600)
			})
			if err != nil {
				t.Fatal(err)
			}
			cached.Path = other.Abs(cached.Path)
			if err = s.VerifyReuse(ctx, env, run, item, cached); err == nil {
				t.Errorf("cached %s divergence accepted", field)
			}
			otherEnv := stage.Env{DB: db, Artifacts: other}
			out, processErr := s.Process(ctx, otherEnv, run, item)
			if processErr == nil || out.Artifact != nil {
				t.Errorf("adopted %s divergence accepted: out=%+v err=%v", field, out, processErr)
			}
			if counted.reads != 4 {
				t.Fatalf("OCR reads=%d want one per call", counted.reads)
			}
		})
	}
}

func TestV1AdmissionRejectsFrozenOCRWhenHTMLTakesPrecedence(t *testing.T) {
	ctx := context.Background()
	bundle, ocr, row, _, _ := processV1Fixture(t)
	if err := os.WriteFile(filepath.Join(bundle, "tree.json"), []byte(`{"success":true,"data":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	html := []byte("<p>Điều 1. Nội dung</p><p>" + strings.Repeat("nội dung ", 60) + "</p>")
	htmlHash := fmt.Sprintf("%x", sha256.Sum256(html))
	if err := os.WriteFile(filepath.Join(bundle, "files", htmlHash), html, 0600); err != nil {
		t.Fatal(err)
	}
	files := fmt.Sprintf(`[{"name":"scan.pdf","sha256":"%s","size":26},{"name":"1_content.html","sha256":"%s","size":%d}]`, *row.PdfSha256, htmlHash, len(html))
	if err := os.WriteFile(filepath.Join(bundle, "files.json"), []byte(files), 0600); err != nil {
		t.Fatal(err)
	}
	artifacts := artifact.Store{Root: t.TempDir()}
	fetched, err := artifacts.PublishContent(ctx, "fetch", strings.Repeat("a", 64), func(dir string) error { return os.CopyFS(dir, os.DirFS(bundle)) })
	if err != nil {
		t.Fatal(err)
	}
	row.RunID, row.ItemKey, row.FetchSha256 = 1, "vn:vbpl:1", &fetched.SHA256
	selected, err := selectionFromRowV1(row, fetched.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	selected.entry.Check.Result = row.CheckResult
	selected.treeTranscription.State = "no_pair"
	item := stage.Item{Key: row.ItemKey, InputHash: observationInputHash(fetched.SHA256, selected)}
	run := stage.Run{ID: 1, Snapshot: stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1}}
	env := stage.Env{DB: processFixtureDBWithRow(t, fetched, &row), Artifacts: artifacts}
	s := New().WithOCR(ocr, []string{"engine"}, "checker")
	data, err := marshalV1TextRecord(&textRecord{TextSource: "none", Gap: "ocr_rejected"}, selected, []byte("OCR text\n"))
	if err != nil {
		t.Fatal(err)
	}
	cached, err := artifacts.Publish(ctx, "extract", item.InputHash, s.Recipe(run.Snapshot), func(dir string) error { return os.WriteFile(filepath.Join(dir, "text.json"), data, 0600) })
	if err != nil {
		t.Fatal(err)
	}
	if err = s.VerifyReuse(ctx, env, run, item, cached); err == nil {
		t.Error("reused frozen OCR despite eligible HTML")
	}
	out, err := s.Process(ctx, env, run, item)
	if err == nil || out.Artifact != nil {
		t.Fatalf("processed frozen OCR despite eligible HTML: out=%+v err=%v", out, err)
	}
}

func TestV1AdmissionRejectsDifferentTreeMeasurement(t *testing.T) {
	for _, field := range []string{"tree_chars", "tree_text_sha256", "main_accepted"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			bundle := transcriptionBundle(t, partialTree, partialHTML())
			artifacts := artifact.Store{Root: t.TempDir()}
			fetched, err := artifacts.PublishContent(ctx, "fetch", strings.Repeat("a", 64), func(dir string) error { return os.CopyFS(dir, os.DirFS(bundle)) })
			if err != nil {
				t.Fatal(err)
			}
			fetched.Path = artifacts.Abs(fetched.Path)
			db := processFixtureDB(t, fetched)
			env := stage.Env{DB: db, Artifacts: artifacts}
			run := stage.Run{ID: 1, Snapshot: stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1}}
			item := stage.Item{Key: "vn:vbpl:1", InputHash: InputHash(fetched.SHA256, "html_transcription")}
			s := New()
			original, err := s.Process(ctx, env, run, item)
			if err != nil || original.Artifact == nil {
				t.Fatalf("original=%+v err=%v", original, err)
			}
			data, err := os.ReadFile(filepath.Join(artifacts.Abs(original.Artifact.Path), "text.json"))
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]any
			if err = json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			tree := record["detector_inputs"].(map[string]any)["tree_transcription"].(map[string]any)
			switch field {
			case "tree_chars":
				tree["tree_chars"] = tree["tree_chars"].(float64) + 1
			case "tree_text_sha256":
				tree["tree_text_sha256"] = strings.Repeat("d", 64)
			case "main_accepted":
				tree["main_accepted"] = false
			}
			forged, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = fetch.VerifyExtractLineageV1Bytes(forged, fetched.SHA256, item.InputHash); err != nil {
				t.Fatalf("invalid forged lineage: %v", err)
			}
			other := artifact.Store{Root: t.TempDir()}
			cached, err := other.Publish(ctx, "extract", item.InputHash, s.Recipe(run.Snapshot), func(dir string) error { return os.WriteFile(filepath.Join(dir, "text.json"), forged, 0600) })
			if err != nil {
				t.Fatal(err)
			}
			cached.Path = other.Abs(cached.Path)
			if err = s.VerifyReuse(ctx, env, run, item, cached); err == nil {
				t.Errorf("cached %s divergence accepted", field)
			}
			out, processErr := s.Process(ctx, stage.Env{DB: db, Artifacts: other}, run, item)
			if processErr == nil || out.Artifact != nil {
				t.Errorf("adopted %s divergence accepted: out=%+v err=%v", field, out, processErr)
			}
		})
	}
}

func TestV1AdmissionAcceptsMatchingConcurrentArtifact(t *testing.T) {
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
	fetched.Path = artifacts.Abs(fetched.Path)
	row.RunID, row.ItemKey, row.FetchSha256 = 1, "vn:vbpl:1", &fetched.SHA256
	selected, err := selectionFromRowV1(row, fetched.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	item := stage.Item{Key: row.ItemKey, InputHash: observationInputHash(fetched.SHA256, selected)}
	run := stage.Run{ID: 1, Snapshot: stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1}}
	db := processFixtureDBWithRow(t, fetched, &row)
	counted := &countedAuthorityOCR{ocrReader: ocr}
	s := New().WithOCR(ocr, []string{"engine"}, "checker")
	s.ocr = counted
	original, err := s.Process(ctx, stage.Env{DB: db, Artifacts: artifacts}, run, item)
	if err != nil || original.Artifact == nil {
		t.Fatalf("original=%+v err=%v", original, err)
	}
	other := artifact.Store{Root: t.TempDir()}
	_, err = other.Publish(ctx, "extract", item.InputHash, s.Recipe(run.Snapshot), func(dir string) error {
		for _, name := range []string{"text.json", "report.json", "provisions.json"} {
			data, readErr := os.ReadFile(filepath.Join(artifacts.Abs(original.Artifact.Path), name))
			if readErr != nil {
				return readErr
			}
			if writeErr := os.WriteFile(filepath.Join(dir, name), data, 0600); writeErr != nil {
				return writeErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Process(ctx, stage.Env{DB: db, Artifacts: other}, run, item)
	if err != nil || out.Artifact == nil || !out.Artifact.Adopted || counted.reads != 2 {
		t.Fatalf("out=%+v reads=%d err=%v", out, counted.reads, err)
	}
}
