package classify

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

func TestRetainedReads_MissingCaptureStartStopsEntireStore(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	if _, err := r.Screen(ctx, o, documents(), families()); err != nil {
		t.Fatal(err)
	}
	var start string
	if err := filepath.WalkDir(filepath.Join(r.IntentDir, "evidence"), func(path string, f fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if f.Name() == "start.json" {
			start = path
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(start); err != nil {
		t.Fatal(err)
	}
	ledger.rows = nil
	if err := r.Rebuild(ctx); err == nil || !strings.Contains(err.Error(), start) {
		t.Errorf("missing child was treated as an empty store: %v", err)
	}
	o.RunID++
	o.Out = filepath.Join(t.TempDir(), "another")
	f.beforeCreate = nil
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil || !strings.Contains(err.Error(), start) || f.creates != 1 {
		t.Fatalf("admission passed a broken capture: creates=%d, %v", f.creates, err)
	}
}

func TestRetainedReads_ReplaysFinalLineBeforeDisconnectWithoutClient(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	o.Documents = 2
	docs := append(documents(), Document{Key: "vn:vbpl:2", Title: "Second synthetic document"})
	f.beforeCreate = func(req []batchapi.Request) {
		for _, v := range req {
			body, err := v.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), docs[0].Key) {
				f.results = []batchapi.Result{storeResult(t, v.CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[\"1.1\",\"retired\"]}"}],"usage":{"input_tokens":100,"output_tokens":10}}`)}
			}
		}
	}
	r.Client = &providerFault{fakeClient: f, stream: func(_ context.Context, _ string, visit func(batchapi.Result) error) error {
		if err := visit(f.results[0]); err != nil {
			return err
		}
		return context.DeadlineExceeded
	}}
	if _, err := r.Screen(ctx, o, docs, families()); err == nil || ledger.rows[0].Settled {
		t.Fatalf("disconnect did not retain the reserve: %v", err)
	}
	reserve := ledger.rows[0].ReserveMicroUSD
	o.RunID++
	o.Release = "20990102"
	o.Documents = 1
	o.Out = filepath.Join(t.TempDir(), "replay")
	r.Client = nil
	ledger.rows = nil
	if err := r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	for i, item := range ledger.rows[0].Items {
		if item.Key == docs[1].Key {
			if _, _, found, err := r.readAttempt(ledger.rows[0], i); err != nil || found {
				t.Fatalf("rebuild inferred an outcome for a missing item: found %t, error %v", found, err)
			}
		}
	}
	s, err := r.Screen(ctx, o, docs[:1], families())
	artifact, readErr := os.ReadFile(filepath.Join(o.Out, "families.json"))
	if err != nil || readErr != nil || s.Requests != 0 || s.LedgerMicroUSD != reserve || ledger.rows[0].Settled || !strings.Contains(string(artifact), "retired") {
		t.Fatalf("did not replay the saved line: summary %+v, error %v, read %v, artifact %s", s, err, readErr, artifact)
	}
}

func TestRetainedReads_ReconcileCannotForgetEarlierCompetingBatch(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.createErr = context.DeadlineExceeded
	f.beforeCreate = nil
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected lost create response")
	}
	s := ledger.rows[0]
	batches := []anthropic.MessageBatch{{ID: "batch_A", CreatedAt: s.CreatedAt, ProcessingStatus: "ended"}, {ID: "batch_B", CreatedAt: s.CreatedAt, ProcessingStatus: "ended"}}
	r.Client = &providerFault{fakeClient: f, pages: func(_ context.Context, visit func([]anthropic.MessageBatch, string) error) error {
		return visit(batches, fakeListingRaw(batches))
	}, stream: func(_ context.Context, _ string, visit func(batchapi.Result) error) error {
		return visit(batchapi.Result{CustomID: customID(s.ID, 0), Type: "expired"})
	}}
	if err := r.Reconcile(ctx, s.ID, false); err == nil || ledger.rows[0].BatchID != "" {
		t.Fatalf("first ambiguous lookup attached a batch: %v", err)
	}
	batches = batches[:1]
	if err := r.Reconcile(ctx, s.ID, false); err == nil || !strings.Contains(err.Error(), "multiple batches") || ledger.rows[0].BatchID != "" {
		t.Fatalf("second reconciliation forgot a competing batch: %v, batch %q", err, ledger.rows[0].BatchID)
	}
}

func TestRetainedReads_UnclosedCaptureCanReplayButCannotSettle(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	r.Client = &providerFault{fakeClient: f, stream: func(_ context.Context, _ string, visit func(batchapi.Result) error) error {
		if err := visit(f.results[0]); err != nil {
			return err
		}
		return context.DeadlineExceeded
	}}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil || ledger.rows[0].Settled {
		t.Fatalf("disconnect released reserve: %v", err)
	}
	ledger.rows = nil
	r.Client = nil
	if err := r.Rebuild(ctx); err != nil || len(ledger.rows) != 1 || ledger.rows[0].Settled {
		t.Fatalf("rebuild settled without a completed download: %v", err)
	}
	if a, _, found, err := r.readAttempt(ledger.rows[0], 0); err != nil || !found || a.Outcome != "final" {
		t.Fatalf("rebuild omitted verified final line: found %t, outcome %s, error %v", found, a.Outcome, err)
	}
	r.Client = f
	if err := r.Reconcile(ctx, ledger.rows[0].ID, false); err != nil || !ledger.rows[0].Settled || ledger.rows[0].CostMicroUSD != 8 {
		t.Fatalf("clean confirmation did not settle the recovered line: %v", err)
	}
}
