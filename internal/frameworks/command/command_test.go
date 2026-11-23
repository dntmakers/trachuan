package command

import (
	"bytes"
	"crypto/sha256"
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
