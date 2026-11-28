package command

import (
	"bytes"
	"context"
	"crypto/sha256"
	"danny.vn/trachuan/internal/frameworks/parse"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_SyntheticSourceOutput(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sources"), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"catalog":{"groups":[{"id":"private-citation","title":"invented-private-title","controls":[]}]}}`)
	sum := sha256.Sum256(data)
	if err := os.WriteFile(filepath.Join(dir, "fixture.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	registry := fmt.Sprintf("frameworks:\n  - code: alpha\n    version: '1.0'\n    name: Invented\n    publisher: Invented\n    language: en\n    access: licensed\n    ordinal: 1\n    citation_scheme: invented\n    parser: nist-oscal-800-53\n    expected_controls: 1\n    files:\n      - path: fixture.json\n        sha256: %x\n        size: %d\n        role: catalog\n        format: oscal-json\n", sum, len(data))
	if err := os.WriteFile(filepath.Join(dir, "sources", "frameworks.yaml"), []byte(registry), 0600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(old); err != nil {
			t.Error(err)
		}
	})
	t.Setenv("FRAMEWORKS_SOURCE_DIR", dir)
	var out bytes.Buffer
	if err := Run([]string{"alpha", "1.0"}, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run([]string{"edges"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatal("licensed edge statistics must be silent")
	}
	out.Reset()
	if err := Run([]string{"alpha", "1.0"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "controls: 1\n" {
		t.Error("licensed diagnostic must contain only the approved total")
	}
	for _, replacement := range []string{"expected_controls: 2", ""} {
		changed := strings.Replace(registry, "expected_controls: 1", replacement, 1)
		if err := os.WriteFile(filepath.Join(dir, "sources", "frameworks.yaml"), []byte(changed), 0600); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		err := Run([]string{"alpha", "1.0"}, &out)
		if err == nil || err.Error() != "frameworks command failed" || out.Len() != 0 {
			t.Errorf("expected generic error and no output: %v", err)
		}
		out.Reset()
		err = Run([]string{"edges"}, &out)
		if err == nil || err.Error() != "frameworks command failed" || out.Len() != 0 {
			t.Errorf("expected generic edge error and no output: %v", err)
		}
	}
	for _, tc := range []struct{ parser, content string }{
		{"nist-oscal-800-53", `{"catalog":"invented-private-value"}`},
		{"cis-controls-8-xlsx", "invented-private-value"},
	} {
		broken := []byte(tc.content)
		brokenSum := sha256.Sum256(broken)
		if err := os.WriteFile(filepath.Join(dir, "fixture.json"), broken, 0600); err != nil {
			t.Fatal(err)
		}
		revised := strings.Replace(registry, fmt.Sprintf("sha256: %x", sum), fmt.Sprintf("sha256: %x", brokenSum), 1)
		revised = strings.Replace(revised, fmt.Sprintf("size: %d", len(data)), fmt.Sprintf("size: %d", len(broken)), 1)
		revised = strings.Replace(revised, "nist-oscal-800-53", tc.parser, 1)
		if err := os.WriteFile(filepath.Join(dir, "sources", "frameworks.yaml"), []byte(revised), 0600); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		err := Run([]string{"alpha", "1.0"}, &out)
		if err == nil || err.Error() != "frameworks command failed" || out.Len() != 0 {
			t.Errorf("expected generic error and no output: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "fixture.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	free := strings.Replace(registry, "access: licensed", "access: free\n    "+"lic"+"ence_kind: public-domain\n    "+"lic"+"ence_url: https://example.test/license", 1)
	if err := os.WriteFile(filepath.Join(dir, "sources", "frameworks.yaml"), []byte(free), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run([]string{"alpha", "1.0"}, &out); err != nil || !strings.Contains(out.String(), "invalid citations:") {
		t.Fatal("free diagnostic lost details")
	}
	out.Reset()
	if err := Run([]string{"edges"}, &out); err != nil || !strings.Contains(out.String(), "resolved:") {
		t.Fatal("free edges lost details")
	}
}

func TestRun_UsageNamesBothForms(t *testing.T) {
	err := Run(nil, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "frameworks <code> <version>") || !strings.Contains(err.Error(), "frameworks edges") {
		t.Fatal("usage omits command forms")
	}
}

type inventedExtractor struct{ calls int }

func (e *inventedExtractor) Extract(_ context.Context, _ []byte, layout, bbox bool) (parse.PDFInputs, error) {
	e.calls++
	if !layout || bbox {
		return parse.PDFInputs{}, fmt.Errorf("wrong inputs")
	}
	return parse.PDFInputs{Layout: []string{"invented"}}, nil
}

func TestRun_InventedPDF(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("FRAMEWORKS_SOURCE_DIR", dir)
	if err := os.Mkdir("sources", 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("invented")
	sum := sha256.Sum256(data)
	if err := os.WriteFile("fixture.pdf", data, 0600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf("frameworks:\n  - code: alpha\n    version: '1.0'\n    name: Invented\n    publisher: Invented\n    language: en\n    access: licensed\n    ordinal: 1\n    citation_scheme: invented\n    parser: invented-pdf\n    parse_source: fixture.pdf\n    expected_controls: 1\n    files:\n      - path: fixture.pdf\n        sha256: %x\n        size: %d\n        role: text\n        format: pdf\n", sum, len(data))
	if err := os.WriteFile("sources/frameworks.yaml", []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	table := parse.Default()
	table["invented-pdf"] = parse.PDFParser{Revision: "1", Role: "text", Format: "pdf", Layout: true, Parse: func(in parse.PDFInputs, _ string, _ *int) (*parse.Result, error) {
		if len(in.Layout) != 1 {
			return nil, fmt.Errorf("missing layout")
		}
		return &parse.Result{Nodes: []parse.Node{{Citation: "A", CitationNorm: "A", Parent: -1}}}, nil
	}}
	extractor := &inventedExtractor{}
	var out bytes.Buffer
	if err := RunWithOptions(context.Background(), []string{"alpha", "1.0"}, &out, extractor, table); err != nil {
		t.Fatal(err)
	}
	if out.String() != "controls: 1\n" {
		t.Fatalf("unexpected output %q", out.String())
	}
	out.Reset()
	if err := RunWithOptions(context.Background(), []string{"edges"}, &out, extractor, table); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 || extractor.calls != 2 {
		t.Fatalf("output %q, extraction calls %d", out.String(), extractor.calls)
	}
}
