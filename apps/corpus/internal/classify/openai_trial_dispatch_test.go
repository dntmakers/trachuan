package classify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stopTrialAfterUpload(t *testing.T, o OpenAITrialOptions, docs []Document, menu []Family, wire *trialWire) OpenAITrialSummary {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wire.afterUpload = cancel
	defer func() { wire.afterUpload = nil }()
	summary, err := RunOpenAIDocumentTrial(ctx, o, wire.client(), docs, menu)
	if !errors.Is(err, context.Canceled) || len(wire.calls) == 0 || wire.calls[len(wire.calls)-1] != "POST /v1/files" {
		t.Fatalf("did not stop after upload: calls=%v err=%v", wire.calls, err)
	}
	return summary
}

func loadTrialFixtureState(t *testing.T, dir string) openAITrialState {
	t.Helper()
	var state openAITrialState
	if ok, err := readTrialJSON(filepath.Join(dir, "state.json"), &state); err != nil || !ok {
		t.Fatalf("read state: %v", err)
	}
	return state
}

func TestOpenAITrialResume_EnforcesCapBeforeCreate(t *testing.T) {
	o, _, menu, _, wire := trialFixture(t)
	o.Recipe.MaxTokens = 32000
	wire.count = 32000
	var docs []Document
	for i := range 60 {
		docs = append(docs, Document{Key: fmt.Sprintf("vn/doc%02d", i), Title: "Luật thử nghiệm"})
	}
	summary := stopTrialAfterUpload(t, o, docs, menu, wire)
	if summary.ReserveMicroUSD != 12_000_000 {
		t.Fatalf("fixture reserve = %d", summary.ReserveMicroUSD)
	}
	o.CapMicroUSD = 1_000_000
	wire.calls = nil
	if _, _, err := ResumeOpenAITrial(t.Context(), "documents", o, wire.client()); err == nil || !strings.Contains(err.Error(), "exceeds --cap-usd") || len(wire.calls) != 0 {
		t.Fatalf("underfunded resume: calls=%v err=%v", wire.calls, err)
	}
	o.CapMicroUSD = 12_000_000
	wire.pause = "POST /v1/batches"
	if _, _, err := ResumeOpenAITrial(t.Context(), "documents", o, wire.client()); err == nil || len(wire.calls) != 1 || wire.calls[0] != "POST /v1/batches" {
		t.Fatalf("exactly funded trial did not dispatch once: calls=%v err=%v", wire.calls, err)
	}
}

func TestOpenAITrialUpload_RejectsReceiptCopiedFromSibling(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	stopTrialAfterUpload(t, o, docs, menu, wire)
	copied := trialRead(t, o.Out, "upload.capture")
	o.Out = filepath.Join(filepath.Dir(o.Out), "second")
	wire.calls, wire.out = nil, o.Out
	stopTrialAfterUpload(t, o, docs, menu, wire)
	s := loadTrialFixtureState(t, o.Out)
	s.Phase, s.FileID = "uploading", ""
	if err := writeTrialJSON(filepath.Join(o.Out, "state.json"), s); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.Out, "upload.capture"), copied, 0600); err != nil {
		t.Fatal(err)
	}
	wire.calls = nil
	if _, _, err := ResumeOpenAITrial(t.Context(), "documents", o, wire.client()); err == nil || !strings.Contains(err.Error(), "upload receipt") || len(wire.calls) != 0 {
		t.Fatalf("used foreign upload receipt: calls=%v err=%v", wire.calls, err)
	}
}

func TestOpenAITrialDownload_RecoversCompleteCaptureBeforeStateWrite(t *testing.T) {
	o, docs, menu, nodes, wire := trialFixture(t)
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil {
		t.Fatal(err)
	}
	s := loadTrialFixtureState(t, o.Out)
	if s.Output == nil {
		t.Fatal("missing saved capture")
	}
	trialRead(t, o.Out, s.Output.Capture)
	s.Phase, s.CostMicroUSD, s.Output = "terminal", 0, nil
	if err := writeTrialJSON(filepath.Join(o.Out, "state.json"), s); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"output.jsonl", "cost.json", "families.json", "MANIFEST.sha256"} {
		if err := os.Remove(filepath.Join(o.Out, filename)); err != nil {
			t.Fatal(err)
		}
	}
	wire.calls, wire.pause = nil, "GET /v1/files/file-result/content"
	if _, _, err := ResumeOpenAITrial(t.Context(), "documents", o, wire.client()); err != nil || len(wire.calls) != 0 {
		t.Fatalf("did not adopt retained result: calls=%v err=%v", wire.calls, err)
	}
	f, _, err := ReadFamilyArtifactForScore(filepath.Join(o.Out, "families.json"), o.Recipe.TaxonomyHash, nodes)
	if err != nil || f.Documents[docs[0].Key].Outcome != "ok" {
		t.Fatalf("invalid recovered artifact: %+v, %v", f, err)
	}
}
