package classify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func TestResultStore_RejectsUnboundRetainedEvidence(t *testing.T) {
	for _, kind := range []string{"dispatch_without_intent", "attempt_without_receipt", "attempt_wrong_batch", "settlement_wrong_batch", "known_abandoned_without_receipt", "unrecognized_file"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, f, o := screenFixture(t)
			if _, err := r.Screen(ctx, o, documents(), families()); err != nil {
				t.Fatal(err)
			}
			s := ledger.rows[0]
			base := filepath.Join(r.IntentDir, "submissions", s.ID)
			attempt := r.attemptPath(s, 0)
			var bad string
			switch kind {
			case "dispatch_without_intent":
				for _, p := range []string{base + ".json", base + ".batch", base + ".settled", base + ".creator.lock", base + ".creator.dispatch", attempt} {
					if err := os.Remove(p); err != nil {
						t.Fatal(err)
					}
				}
				bad = base + ".creator.finished"
			case "attempt_without_receipt":
				for _, p := range []string{base + ".batch", base + ".settled"} {
					if err := os.Remove(p); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.RemoveAll(filepath.Join(r.IntentDir, "evidence", s.ID, "responses")); err != nil {
					t.Fatal(err)
				}
				bad = attempt
			case "attempt_wrong_batch":
				bad = attempt
				replaceEvidenceBatch(t, bad)
			case "settlement_wrong_batch":
				bad = base + ".settled"
				replaceEvidenceBatch(t, bad)
			case "known_abandoned_without_receipt":
				for _, p := range []string{base + ".batch", base + ".settled", base + ".creator.finished", base + ".creator.dispatch", attempt} {
					if err := os.Remove(p); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.RemoveAll(filepath.Join(r.IntentDir, "evidence", s.ID, "responses")); err != nil {
					t.Fatal(err)
				}
				s.State, s.Settled, s.CostMicroUSD = "abandoned", false, 0
				bad = base + ".batch"
			case "unrecognized_file":
				bad = filepath.Join(r.IntentDir, "unexpected")
				if err := os.WriteFile(bad, []byte("unexpected"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ledger.rows = nil
			if kind == "known_abandoned_without_receipt" {
				ledger.rows = []Submission{s}
			}
			if err := r.Rebuild(ctx); err == nil || !strings.Contains(err.Error(), filepath.Base(bad)) {
				t.Errorf("rebuild did not report %s: %v", bad, err)
			}
			ledger.rows = []Submission{s}
			f.beforeCreate = nil
			o.RunID++
			o.Out = filepath.Join(t.TempDir(), "another")
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil || !strings.Contains(err.Error(), filepath.Base(bad)) || f.creates != 1 {
				t.Errorf("admission did not report %s: %v, creates %d", bad, err, f.creates)
			}
		})
	}
}

func replaceEvidenceBatch(t *testing.T, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	fields["provider_job_id"] = json.RawMessage(`"batch_unrelated"`)
	b, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestResultStore_AbandonmentRequiresCreatorQuietEvidence(t *testing.T) {
	for _, removed := range []string{"creator.dispatch", "creator.finished"} {
		t.Run(removed, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, f, o := screenFixture(t)
			f.createErr = context.DeadlineExceeded
			f.beforeCreate = func(_ []batchapi.Request) {
				if _, err := os.Stat(filepath.Join(r.IntentDir, "submissions", ledger.rows[0].ID+".creator.dispatch")); err != nil {
					t.Fatalf("create called before durable dispatch trace: %v", err)
				}
			}
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected uncertain dispatch")
			}
			s := ledger.rows[0]
			r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
			if err := r.Reconcile(ctx, s.ID, true); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(r.IntentDir, "submissions", s.ID+"."+removed)); err != nil {
				t.Fatal(err)
			}
			ledger.rows = nil
			err := r.Rebuild(ctx)
			if removed == "creator.finished" {
				if err == nil {
					t.Fatal("accepted missing creator quiet evidence")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			retained, err := r.verifyStore(ledger.rows, nil)
			if err != nil || !retained.retryAbandoned[s.ID] {
				t.Fatalf("clean listing did not release key: %v", err)
			}
		})
	}
}

func TestResultStore_DeadlinePollerCannotDiscardOtherPollersResults(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.ended = false
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	s := ledger.rows[0]
	f.ended = true
	deadlinePoller := &Runner{Repository: ledger, Client: f, IntentDir: r.IntentDir, Now: func() time.Time { return s.DeadlineAt.Add(time.Second) }}
	download := &hookedReconcileClient{fakeClient: f, resultsHook: func() error {
		if err := deadlinePoller.poll(ctx, &s, o.Out); err == nil || ledger.rows[0].State != "failed" {
			t.Fatalf("second poller did not mark deadline: %v", err)
		}
		return nil
	}}
	r.Client = download
	if _, err := r.Screen(ctx, o, documents(), families()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.attemptPath(s, 0)); err != nil || !ledger.rows[0].Settled || ledger.rows[0].CostMicroUSD != 8 {
		t.Fatalf("deadline discarded priced results: settled %v, cost %d, error %v", ledger.rows[0].Settled, ledger.rows[0].CostMicroUSD, err)
	}
}

func TestResultStore_ReconcileRecoversFailedKnownBatch(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.ended = false
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	s := ledger.rows[0]
	r.Now = func() time.Time { return s.DeadlineAt.Add(time.Second) }
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil || ledger.rows[0].State != "failed" {
		t.Fatal("expected deadline to stop polling")
	}
	f.ended = true
	if err := r.Reconcile(ctx, s.ID, false); err != nil || !ledger.rows[0].Settled || ledger.rows[0].CostMicroUSD != 8 {
		t.Fatalf("failed batch not settled: settled %v, cost %d, error %v", ledger.rows[0].Settled, ledger.rows[0].CostMicroUSD, err)
	}
}

func TestResultStore_DownloadForDifferentBatchCannotSettle(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.ended = false
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	s := ledger.rows[0]
	if _, _, err := r.recordDownloaded(ctx, s, "batch_unrelated"); err == nil || ledger.rows[0].Settled {
		t.Fatalf("unrelated batch settled submission: %v", err)
	}
	if _, err := os.Stat(r.attemptPath(s, 0)); !os.IsNotExist(err) {
		t.Fatalf("unexpected attempt file: %v", err)
	}
}
