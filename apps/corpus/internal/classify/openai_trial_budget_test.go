package classify

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAITrialBudget_UnpricedSiblingBlocksCreateButNotCollection(t *testing.T) {
	for _, scenario := range []struct{ name, usage string }{
		{"outside-tier", `{"input_tokens":300000,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":10,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":300010}`},
		{"missing-usage", `null`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			o, docs, menu, nodes, blocked := trialFixture(t)
			ctx := t.Context()
			blocked.pause = "GET /v1/batches/batch-one"
			blocked.usage = scenario.usage
			if _, err := RunOpenAIDocumentTrial(ctx, o, blocked.client(), docs, menu); err == nil {
				t.Fatal("expected interrupted poll")
			}
			collect := o
			collect.Out = filepath.Join(filepath.Dir(o.Out), "collect")
			paid := &trialWire{t: t, out: collect.Out, pause: "GET /v1/batches/batch-one"}
			if _, err := RunOpenAIDocumentTrial(ctx, collect, paid.client(), docs, menu); err == nil {
				t.Fatal("expected interrupted poll")
			}
			admit := o
			admit.Out = filepath.Join(filepath.Dir(o.Out), "admit")
			unpaid := &trialWire{t: t, out: admit.Out}
			stopTrialAfterUpload(t, admit, docs, menu, unpaid)

			blocked.pause = ""
			if _, _, err := ResumeOpenAITrial(ctx, "documents", o, blocked.client()); err == nil {
				t.Fatal("unpriced usage unexpectedly settled")
			}
			unpaid.calls = nil
			if _, _, err := ResumeOpenAITrial(ctx, "documents", admit, unpaid.client()); err == nil || !strings.Contains(err.Error(), "unpriced") || len(unpaid.calls) != 0 {
				t.Errorf("new Create not blocked: calls=%v err=%v", unpaid.calls, err)
			}
			paid.calls, paid.pause = nil, ""
			if _, _, err := ResumeOpenAITrial(ctx, "documents", collect, paid.client()); err != nil || !reflect.DeepEqual(paid.calls, []string{"GET /v1/batches/batch-one", "GET /v1/files/file-result/content"}) {
				t.Fatalf("paid collection blocked: calls=%v err=%v", paid.calls, err)
			}
			if _, _, err := ReadFamilyArtifactForScore(filepath.Join(collect.Out, "families.json"), o.Recipe.TaxonomyHash, nodes); err != nil {
				t.Fatal(err)
			}
			state := trialRead(t, o.Out, "state.json")
			if !bytes.Contains(state, []byte(`"unpriced_results": 1`)) || !bytes.Contains(state, []byte(`"reserve_micro_usd": 20605`)) {
				t.Errorf("state lost the unresolved item count or reserve: %s", state)
			}
			cost := trialRead(t, o.Out, "cost.json")
			if scenario.name == "outside-tier" && !bytes.Contains(cost, []byte(`"usage_input_tokens": 300000`)) {
				t.Errorf("out-of-bound usage not retained: %s", cost)
			}
		})
	}
}

func TestOpenAITrialBudget_FlagsUnpricedOutputEvenIfErrorDownloadFails(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	docs = append(docs, Document{Key: "vn/other", Title: "Luật khác"})
	wire.usage = `{"input_tokens":300000,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":10,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":300010}`
	wire.errorResult = `{"custom_id":"1","error":{"code":"batch_expired"},"response":null}` + "\n"
	wire.pause = "GET /v1/files/file-error/content"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted error download")
	}
	if s := loadTrialFixtureState(t, o.Out); s.UnpricedResults == 0 || s.ReserveMicroUSD != 41210 {
		t.Fatalf("failed download hid an observed unpriced result: count=%d reserve=%d", s.UnpricedResults, s.ReserveMicroUSD)
	}
	if cost := trialRead(t, o.Out, "cost.json"); !bytes.Contains(cost, []byte(`"usage_input_tokens": 300000`)) {
		t.Fatalf("failed download hid retained usage: %s", cost)
	}
}

func TestOpenAITrialBudget_CrashBetweenFilesBlocksUntilSettled(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	candidate := o
	candidate.Out = filepath.Join(filepath.Dir(o.Out), "candidate")
	pending := &trialWire{t: t, out: candidate.Out}
	stopTrialAfterUpload(t, candidate, docs, menu, pending)

	docs = append(docs, Document{Key: "vn/other", Title: "Luật khác"})
	wire.errorResult = `{"custom_id":"1","error":{"code":"batch_expired"},"response":null}` + "\n"
	wire.pause = "GET /v1/files/file-error/content"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted error download")
	}
	attempts, err := filepath.Glob(filepath.Join(o.Out, "error-*.capture"))
	if err != nil || len(attempts) != 1 {
		t.Fatalf("unexpected error capture attempts: %v, %v", attempts, err)
	}
	if err := os.Remove(attempts[0]); err != nil {
		t.Fatal(err)
	}
	state := loadTrialFixtureState(t, o.Out)
	if state.Phase != "terminal" || state.Output == nil || state.Error != nil || state.UnpricedResults != 0 {
		t.Fatal("fixture should have only bound output and no pricing decision")
	}
	trialAbsent(t, o.Out, "cost.json")

	pending.calls = nil
	if _, _, err := ResumeOpenAITrial(t.Context(), "documents", candidate, pending.client()); err == nil || !strings.Contains(err.Error(), filepath.Base(o.Out)) || len(pending.calls) != 0 {
		t.Fatalf("unsettled sibling did not block Create: calls=%v, err=%v", pending.calls, err)
	}
	wire.calls, wire.pause = nil, ""
	if _, _, err := ResumeOpenAITrial(t.Context(), "documents", o, wire.client()); err != nil || !reflect.DeepEqual(wire.calls, []string{"GET /v1/files/file-error/content"}) {
		t.Fatalf("blocked paid collection: calls=%v, err=%v", wire.calls, err)
	}
	pending.pause = "POST /v1/batches"
	if _, _, err := ResumeOpenAITrial(t.Context(), "documents", candidate, pending.client()); err == nil || !reflect.DeepEqual(pending.calls, []string{"POST /v1/batches"}) {
		t.Fatalf("explicit settled cost did not unblock Create: calls=%v, err=%v", pending.calls, err)
	}
}

func TestOpenAITrialBudget_SettledSiblingRequiresExplicitCompleteCost(t *testing.T) {
	for _, tc := range []struct{ name, change, replacement string }{
		{"missing file", "", ""},
		{"missing total", "  \"priced_micro_usd\": 151,\n", ""},
		{"partial report", `"complete": true`, `"complete": false`},
		{"unspecified unpriced items", `"unpriced": []`, `"unpriced": null`},
		{"missing item cost", `"cost_micro_usd": 151`, `"cost_micro_usd": null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, docs, menu, _, wire := trialFixture(t)
			if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil {
				t.Fatal(err)
			}
			costPath := filepath.Join(o.Out, "cost.json")
			if tc.change == "" {
				if err := os.Remove(costPath); err != nil {
					t.Fatal(err)
				}
			} else {
				cost := trialRead(t, o.Out, "cost.json")
				if !bytes.Contains(cost, []byte(tc.change)) {
					t.Fatalf("unchanged test fixture: %s", cost)
				}
				cost = bytes.Replace(cost, []byte(tc.change), []byte(tc.replacement), 1)
				if err := os.WriteFile(costPath, cost, 0600); err != nil {
					t.Fatal(err)
				}
			}
			candidate := o
			candidate.Out = filepath.Join(filepath.Dir(o.Out), "candidate")
			pending := &trialWire{t: t, out: candidate.Out}
			if _, err := RunOpenAIDocumentTrial(t.Context(), candidate, pending.client(), docs, menu); err == nil || !strings.Contains(err.Error(), filepath.Base(o.Out)) {
				t.Errorf("incomplete settlement did not block admission: %v", err)
			}
			for _, call := range pending.calls {
				if call == "POST /v1/batches" {
					t.Errorf("unverified sibling cost did not block Create: %v", pending.calls)
				}
			}
		})
	}
}

func TestOpenAITrialBudget_TerminalReceiptBlocksEvenBeforePhaseWrite(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	candidate := o
	candidate.Out = filepath.Join(filepath.Dir(o.Out), "candidate")
	pending := &trialWire{t: t, out: candidate.Out}
	stopTrialAfterUpload(t, candidate, docs, menu, pending)

	wire.pause = "GET /v1/files/file-result/content"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted download")
	}
	s := loadTrialFixtureState(t, o.Out)
	trialRead(t, o.Out, s.TerminalCapture)
	s.Phase, s.TerminalHash, s.TerminalCapture = "submitted", "", ""
	if err := writeTrialJSON(filepath.Join(o.Out, "state.json"), s); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(o.Out, "terminal.json")); err != nil {
		t.Fatal(err)
	}
	attempts, err := filepath.Glob(filepath.Join(o.Out, "output-*.capture"))
	if err != nil || len(attempts) != 1 {
		t.Fatalf("unexpected output capture attempts: %v, %v", attempts, err)
	}
	if err := os.Remove(attempts[0]); err != nil {
		t.Fatal(err)
	}
	pending.calls = nil
	if _, _, err := ResumeOpenAITrial(t.Context(), "documents", candidate, pending.client()); err == nil || !strings.Contains(err.Error(), filepath.Base(o.Out)) || len(pending.calls) != 0 {
		t.Fatalf("persisted terminal receipt did not block Create: calls=%v err=%v", pending.calls, err)
	}
}
