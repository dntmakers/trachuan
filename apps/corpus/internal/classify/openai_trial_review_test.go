package classify

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAITrialBudget_RefusesMissingSiblingMoneyEvidence(t *testing.T) {
	for _, trace := range []string{"create.json", "state.json"} {
		t.Run(trace, func(t *testing.T) {
			o, docs, menu, _, wire := trialFixture(t)
			if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil {
				t.Fatal(err)
			}
			if trace == "create.json" {
				if err := os.Remove(filepath.Join(o.Out, "state.json")); err != nil {
					t.Fatal(err)
				}
			} else {
				data := trialRead(t, o.Out, "state.json")
				data = bytes.Replace(data, []byte("  \"cost_micro_usd\": 151,\n"), nil, 1)
				if err := os.WriteFile(filepath.Join(o.Out, "state.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			o.Out = filepath.Join(filepath.Dir(o.Out), "second")
			o.DryRun = true
			wire.out = o.Out
			if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
				t.Fatal("accepted a sibling whose budget charge could be lost")
			}
		})
	}
}

func TestOpenAITrialCreate_RetainsUnusableResponse(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	wire.oddCreate = true
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("accepted invalid create receipt")
	}
	raw := trialRead(t, o.Out, "create.capture")
	if !bytes.Contains(raw, []byte(`"status":200`)) || !bytes.Contains(raw, []byte(`"complete":true`)) || !bytes.Contains(raw, []byte(`"id":17`)) {
		t.Fatalf("create capture lost raw response or framing: %s", raw)
	}
	trialAbsent(t, o.Out, "create.json")
}

func TestOpenAITrialUpload_DoesNotRetryUncertainDispatch(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	wire.pause = "POST /v1/files"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted upload")
	}
	wire.calls, wire.pause = nil, ""
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil || len(wire.calls) != 0 {
		t.Fatalf("repeated upload/create after dispatch: %v, %v", wire.calls, err)
	}
}

func TestOpenAITrialResume_UsesSavedPlanDespiteUnavailableInputsAndLowCap(t *testing.T) {
	o, docs, menu, nodes, wire := trialFixture(t)
	wire.pause = "GET /v1/batches/batch-one"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted retrieve")
	}
	o.CapMicroUSD = 1
	wire.calls, wire.pause = nil, ""
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), nil, nil); err != nil {
		t.Fatalf("could not collect paid result: %v", err)
	}
	f, _, err := ReadFamilyArtifactForScore(filepath.Join(o.Out, "families.json"), o.Recipe.TaxonomyHash, nodes)
	if err != nil || f.Documents[docs[0].Key].Outcome != "ok" {
		t.Fatalf("invalid recovered artifact: %+v, %v", f, err)
	}
}

func TestOpenAITrialDownload_RejectsUnboundResultFile(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	wire.pause = "GET /v1/files/file-result/content"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted download")
	}
	if err := os.WriteFile(filepath.Join(o.Out, "output.jsonl"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	wire.calls, wire.pause = nil, ""
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil || !strings.Contains(err.Error(), "unbound") {
		t.Fatalf("existing unbound result was used: %v", err)
	}
	trialAbsent(t, o.Out, "cost.json")
}

func TestOpenAITrialCredential_RejectsProjectAndEscapedResult(t *testing.T) {
	t.Run("project", func(t *testing.T) {
		o, docs, menu, _, wire := trialFixture(t)
		client := NewOpenAITrialClient("fixture-credential", "project_fixture-credential", wire)
		if _, err := RunOpenAIDocumentTrial(t.Context(), o, client, docs, menu); err == nil || len(wire.calls) != 0 || strings.Contains(err.Error(), "fixture-credential") {
			t.Fatalf("accepted or leaked key: calls=%v err=%v", wire.calls, err)
		}
		trialAbsent(t, o.Out, "state.json")
	})
	t.Run("escaped", func(t *testing.T) {
		o, docs, menu, _, wire := trialFixture(t)
		wire.result = `{"codes":["fixture-credential"]}`
		if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil || strings.Contains(err.Error(), "fixture-credential") || !strings.Contains(err.Error(), "contains credentials") {
			t.Fatalf("accepted or leaked escaped key: %v", err)
		}
		trialAbsent(t, o.Out, "output.jsonl")
	})
}

func TestOpenAITrialDownload_RejectsChangedTerminalReceipt(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	wire.pause = "GET /v1/files/file-result/content"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted download")
	}
	data := append([]byte{' '}, trialRead(t, o.Out, "terminal.json")...)
	if err := os.WriteFile(filepath.Join(o.Out, "terminal.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	wire.calls, wire.pause = nil, ""
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil || len(wire.calls) != 0 {
		t.Fatalf("used changed terminal evidence: %v, %v", wire.calls, err)
	}
}

func TestOpenAITrialCount_CarriesForwardLegacyDryRun(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	o.DryRun = true
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil {
		t.Fatal(err)
	}
	var state openAITrialState
	path := filepath.Join(o.Out, "state.json")
	if found, err := readTrialJSON(path, &state); err != nil || !found {
		t.Fatalf("could not read state: %v", err)
	}
	state.Version, state.Effort = 1, ""
	if err := writeTrialJSON(path, state); err != nil {
		t.Fatal(err)
	}
	wire.calls = nil
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil || len(wire.calls) != 0 {
		t.Fatalf("legacy cache was not reused: calls=%v err=%v", wire.calls, err)
	}
	if found, err := readTrialJSON(path, &state); err != nil || !found || state.Version != 2 || state.Effort != o.Recipe.Effort {
		t.Fatalf("legacy state was not upgraded: %+v, %v", state, err)
	}
}
