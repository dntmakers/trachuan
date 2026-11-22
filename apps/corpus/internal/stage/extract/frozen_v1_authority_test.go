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
	"danny.vn/trachuan/apps/corpus/internal/ocrstore"
	"danny.vn/trachuan/apps/corpus/internal/stage"
	"danny.vn/trachuan/apps/corpus/internal/stage/fetch"
	"danny.vn/trachuan/apps/corpus/internal/store"
)

func TestV1AuthorityRejectsInvalidAssociations(t *testing.T) {
	for _, change := range []string{"missing-pdf", "wrong-pdf-bytes", "wrong-engine", "wrong-checker", "wrong-text-id", "rejected-served", "unwaived-label-served"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			bundle, ocr, row, _, _ := processV1Fixture(t)
			if err := os.WriteFile(filepath.Join(bundle, "tree.json"), []byte(`{"success":true,"data":[]}`), 0600); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "missing-pdf":
				if err := os.Remove(filepath.Join(bundle, "files", *row.PdfSha256)); err != nil {
					t.Fatal(err)
				}
			case "wrong-pdf-bytes":
				if err := os.WriteFile(filepath.Join(bundle, "files", *row.PdfSha256), []byte("changed PDF"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-engine":
				value := "unknown-engine"
				row.EngineID = &value
			case "wrong-checker":
				value := "unknown-checker"
				row.CheckerID = &value
			case "wrong-text-id":
				value := int64(999)
				row.CheckOcrTextID = &value
			case "rejected-served":
				source := "ocr"
				row.TextSource, row.Gap = &source, ""
			case "unwaived-label-served":
				source := "ocr"
				row.TextSource, row.Gap = &source, ""
				result := ocrstore.CheckResult{SameDocument: true, Pages: 1, PDFPages: 1, Verdict: "flagged", Items: []ocrstore.CheckItem{{Kind: "article_label", Page: 1, OCR: "Điều 5", PDF: "Điều 6"}}}
				encoded, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
				row.CheckResult, row.ResultSha256, row.Flagged.Int32 = encoded, &hash, 1
			}
			artifacts := artifact.Store{Root: t.TempDir()}
			fetched, err := artifacts.PublishContent(ctx, "fetch", strings.Repeat("a", 64), func(dir string) error { return os.CopyFS(dir, os.DirFS(bundle)) })
			if err != nil {
				t.Fatal(err)
			}
			row.RunID, row.ItemKey, row.FetchSha256 = 1, "vn:vbpl:1", &fetched.SHA256
			base := fetch.ExtractInputHash(fetched.SHA256, *row.TextSource)
			if *row.TextSource == "none" {
				base = fetch.ExtractInputHash(fetched.SHA256, "none", row.Gap)
			}
			if *row.TextSource == "ocr" {
				base = fetch.ExtractInputHash(fetched.SHA256, "ocr", *row.TextSha256, *row.ResultSha256)
			}
			hash := fetch.ExtractObservationInputHash(base, *row.PdfSha256, *row.EngineID, *row.TextSha256, *row.CheckerID, *row.ResultSha256)
			item := stage.Item{Key: "vn:vbpl:1", InputHash: hash}
			run := stage.Run{ID: 1, Snapshot: stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1}}
			env := stage.Env{DB: processFixtureDBWithRow(t, fetched, &row), Artifacts: artifacts}
			s := New().WithOCR(ocr, []string{"engine"}, "checker")
			cached, err := artifacts.Publish(ctx, "extract", hash, s.Recipe(run.Snapshot), func(dir string) error { return os.WriteFile(filepath.Join(dir, "report.json"), []byte(`[]`), 0600) })
			if err != nil {
				t.Fatal(err)
			}
			if err = s.VerifyReuse(ctx, env, run, item, cached); err == nil {
				t.Errorf("reused invalid association %s", change)
			}
			out, err := s.Process(ctx, env, run, item)
			if err == nil || out.Artifact != nil {
				t.Errorf("processed invalid association %s: out=%+v err=%v", change, out, err)
			}
		})
	}
}

func TestV1AuthorityServingDecisions(t *testing.T) {
	for _, tc := range []struct {
		name, source, gap, verdict               string
		heading, flagged, waived, longText, want bool
	}{
		{"rejected heading fallback", "source_tree", "tree_headings_only", "rejected", true, false, false, false, true},
		{"unwaived heading fallback", "source_tree", "tree_headings_only", "flagged", true, true, false, false, true},
		{"parse-limited heading fallback", "source_tree", "tree_headings_only", "clean", true, false, false, false, true},
		{"usable OCR cannot fall back", "source_tree", "tree_headings_only", "clean", true, false, false, true, false},
		{"short OCR cannot bypass heading fallback", "ocr", "", "clean", true, false, false, false, false},
		{"clean OCR without tree", "ocr", "", "clean", false, false, false, false, true},
		{"waived label OCR", "ocr", "", "flagged", false, true, true, false, true},
		{"unwaived label withheld", "none", "ocr_label_flagged", "flagged", false, true, false, false, true},
		{"rejected OCR withheld", "none", "ocr_rejected", "rejected", false, false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			bundle, ocr, row, _, root := processV1Fixture(t)
			tree := `{"success":true,"data":[]}`
			if tc.heading {
				tree = bareTree
			}
			if err := os.WriteFile(filepath.Join(bundle, "tree.json"), []byte(tree), 0600); err != nil {
				t.Fatal(err)
			}
			result := ocrstore.CheckResult{SameDocument: tc.verdict != "rejected", Pages: 1, PDFPages: 1, Verdict: tc.verdict, Items: []ocrstore.CheckItem{}}
			if tc.flagged {
				result.Items = append(result.Items, ocrstore.CheckItem{Kind: "article_label", Page: 1, OCR: "Điều 5", PDF: "Điều 6"})
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
			row.TextSource, row.Gap, row.CheckResult, row.ResultSha256, row.Flagged.Int32 = &tc.source, tc.gap, encoded, &hash, int32(len(result.Items))
			if tc.longText {
				data := []byte("Điều 1. Quy định\n" + strings.Repeat("nội dung ", 80))
				textHash := fmt.Sprintf("%x", sha256.Sum256(data))
				if err = os.WriteFile(filepath.Join(root, "text", textHash+".txt"), data, 0600); err != nil {
					t.Fatal(err)
				}
				row.TextSha256 = &textHash
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
			item := stage.Item{Key: "vn:vbpl:1", InputHash: observationInputHash(fetched.SHA256, selected)}
			run := stage.Run{ID: 1, Snapshot: stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1}}
			env := stage.Env{DB: processFixtureDBWithRow(t, fetched, &row), Artifacts: artifacts}
			s := New().WithOCR(ocr, []string{"engine"}, "checker")
			if tc.waived {
				s.WithWaivers(Waivers{entries: []Waiver{{Key: item.Key, PDFSHA256: *row.PdfSha256, CheckSHA256: hash, Items: []WaiverItem{{Page: 1, Kind: "article_label", OCR: "Điều 5", PDF: "Điều 6"}}}}})
			}
			out, processErr := s.Process(ctx, env, run, item)
			if (processErr == nil) != tc.want || (tc.want && out.Artifact == nil) || (!tc.want && out.Artifact != nil) {
				t.Fatalf("out=%+v err=%v", out, processErr)
			}
			if tc.want {
				if reuseErr := s.VerifyReuse(ctx, env, run, item, *out.Artifact); reuseErr != nil {
					t.Fatalf("reuse err=%v", reuseErr)
				}
			}
		})
	}
}

func TestV1ReuseRejectsCachedRecordMismatch(t *testing.T) {
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
	selected, err := selectionFromRowV1(row, fetched.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	item := stage.Item{Key: row.ItemKey, InputHash: observationInputHash(fetched.SHA256, selected)}
	run := stage.Run{ID: 1, Snapshot: stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1}}
	env := stage.Env{DB: processFixtureDBWithRow(t, fetched, &row), Artifacts: artifacts}
	s := New().WithOCR(ocr, []string{"engine"}, "checker")
	cached, err := artifacts.Publish(ctx, "extract", item.InputHash, s.Recipe(run.Snapshot), func(dir string) error {
		return os.WriteFile(filepath.Join(dir, "text.json"), []byte(`{"text_source":"none","gap":"ocr_rejected","detector_inputs":{"version":1,"ocr_check":{"state":"unavailable"},"tree_transcription":{"state":"no_pair"}}}`), 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.VerifyReuse(ctx, env, run, item, cached); err == nil {
		t.Fatal("accepted cached record with different OCR association")
	}
	out, err := s.Process(ctx, env, run, item)
	if err == nil || out.Artifact != nil {
		t.Fatalf("accepted concurrently adopted artifact: out=%+v err=%v", out, err)
	}
}

type countedAuthorityOCR struct {
	ocrReader
	reads   int
	lookups int
}

func (c *countedAuthorityOCR) Lookup(context.Context, []string, []string, string) (map[string]ocrstore.Entry, error) {
	c.lookups++
	return nil, fmt.Errorf("unexpected OCR reselection")
}

func (c *countedAuthorityOCR) ReadText(entry store.OcrText) ([]byte, error) {
	c.reads++
	return c.ocrReader.ReadText(entry)
}

func TestV1AuthorityReadsOCROncePerFreshOrReuse(t *testing.T) {
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
	selected, err := selectionFromRowV1(row, fetched.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	item := stage.Item{Key: row.ItemKey, InputHash: observationInputHash(fetched.SHA256, selected)}
	run := stage.Run{ID: 1, Snapshot: stage.Snapshot{ExtractRecordContract: stage.ExtractRecordV1}}
	env := stage.Env{DB: processFixtureDBWithRow(t, fetched, &row), Artifacts: artifacts}
	counted := &countedAuthorityOCR{ocrReader: ocr}
	s := New().WithOCR(ocr, []string{"engine"}, "checker")
	s.ocr = counted
	out, err := s.Process(ctx, env, run, item)
	if err != nil || out.Artifact == nil || counted.reads != 1 || counted.lookups != 0 {
		t.Fatalf("out=%+v reads=%d lookups=%d err=%v", out, counted.reads, counted.lookups, err)
	}
	if err = s.VerifyReuse(ctx, env, run, item, *out.Artifact); err != nil || counted.reads != 2 || counted.lookups != 0 {
		t.Fatalf("reuse reads=%d lookups=%d err=%v", counted.reads, counted.lookups, err)
	}
}
