package classify

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAITrialCount_RecoversJournalBeforeRecount(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	o.DryRun = true
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(o.Out, "counts.json")); err != nil {
		t.Fatal(err)
	}
	wire.calls = nil
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil || len(wire.calls) != 0 {
		t.Fatalf("count recovery calls=%v err=%v", wire.calls, err)
	}
	trialRead(t, o.Out, "counts.json")
}

func TestOpenAITrialDocuments_DoesNotPersistEchoedCredential(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	wire.result = `{"codes":["fixture-credential"]}`
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil || strings.Contains(err.Error(), "fixture-credential") {
		t.Fatalf("expected safe failure, got %v", err)
	}
	files, err := os.ReadDir(o.Out)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data := trialRead(t, o.Out, file.Name())
		if bytes.Contains(data, []byte("fixture-credential")) {
			t.Fatalf("credential was stored in %s", file.Name())
		}
	}
}

func TestOpenAITrialDocuments_RecoversDurableReceiptBeforePhase(t *testing.T) {
	for _, scenario := range []struct{ phase, clear, pause string }{
		{"uploading", "upload", "POST /v1/batches"},
		{"dispatched", "create", "GET /v1/batches/batch-one"},
	} {
		t.Run(scenario.clear, func(t *testing.T) {
			o, docs, menu, _, wire := trialFixture(t)
			wire.pause = scenario.pause
			if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
				t.Fatal("expected interruption")
			}
			path := filepath.Join(o.Out, "state.json")
			var s openAITrialState
			if found, err := readTrialJSON(path, &s); err != nil || !found {
				t.Fatalf("read state: %v", err)
			}
			s.Phase, s.BatchID = scenario.phase, ""
			if scenario.clear == "upload" {
				s.FileID = ""
				if err := os.Remove(filepath.Join(o.Out, "create.capture")); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeTrialJSON(path, s); err != nil {
				t.Fatal(err)
			}
			wire.calls, wire.pause = nil, ""
			if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil {
				t.Fatal(err)
			}
			for _, call := range wire.calls {
				if call == "POST /v1/files" || (scenario.clear == "create" && call == "POST /v1/batches") {
					t.Fatalf("duplicate remote step: %v", wire.calls)
				}
			}
			if _, err := os.Stat(filepath.Join(o.Out, "families.json")); errors.Is(err, os.ErrNotExist) {
				t.Fatal("missing resumed artifact")
			}
		})
	}
}
