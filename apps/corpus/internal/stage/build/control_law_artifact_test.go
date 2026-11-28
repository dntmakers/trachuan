package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const controlLawArtifact = `- id: law-link
  kind: control_law_link
  target: {key: "vn:vbpl:1", anchor: "dieu-1"}
  claim: {control: {code: nist, version: "1", citation: AC-1}, relation_kind: related}
  evidence: {quote: "exact law provision text"}
  depends:
    contract: 1
    target: {number_norm: "1", text_source: source_tree, text_sha256: "sha256:1111111111111111111111111111111111111111111111111111111111111111", heading_sha256: "sha256:2222222222222222222222222222222222222222222222222222222222222222", ancestors: []}
    target_own_sha256: "sha256:3333333333333333333333333333333333333333333333333333333333333333"
    control_sha256: "sha256:4444444444444444444444444444444444444444444444444444444444444444"
  reviewed: {date: "2099-11-18", by: owner, method: human}
`

func TestControlLawArtifactRefused(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, knowledgeFileName), []byte(controlLawArtifact), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, knowledgeReportName), []byte("[]"), 0600); e != nil {
		t.Fatal(e)
	}
	_, err := knowledgeArtifactHash("vn", dir)
	if err == nil || !strings.Contains(err.Error(), "cannot be validated against a law release yet") {
		t.Fatalf("err = %v", err)
	}
}
