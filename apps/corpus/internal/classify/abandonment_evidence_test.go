package classify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

func TestAbandonment_CompleteListingReleasesDispatchedReserve(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	client.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain dispatch")
	}
	s := ledger.rows[0]
	r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
	if err := r.Reconcile(ctx, s.ID, true); err != nil {
		t.Fatal(err)
	}
	store, err := r.verifyStore(ledger.rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !store.retryAbandoned[s.ID] {
		t.Fatal("complete clean listing did not release key")
	}
	cost, err := store.ledgerCostFor("anthropic")
	if err != nil || cost != 0 {
		t.Fatalf("reserve remains: %d, %v", cost, err)
	}
	client.createErr = nil
	o.RunID++
	o.Out = filepath.Join(t.TempDir(), "replacement")
	_, _ = r.Screen(ctx, o, documents(), families())
	if client.creates != 2 {
		t.Fatalf("replacement was not submitted: %d creates", client.creates)
	}
}

func TestAbandonment_IncompleteEvidenceKeepsReserve(t *testing.T) {
	for _, scenario := range []string{"interrupted listing", "running candidate", "interrupted results", "matching result"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, client, o := screenFixture(t)
			client.beforeCreate = nil
			client.createErr = context.DeadlineExceeded
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected uncertain dispatch")
			}
			s := ledger.rows[0]
			r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
			fault := &providerFault{fakeClient: client}
			fault.pages = func(_ context.Context, visit func([]anthropic.MessageBatch, string) error) error {
				status := anthropic.MessageBatchProcessingStatus("ended")
				if scenario == "running candidate" {
					status = "in_progress"
				}
				data := []anthropic.MessageBatch{{ID: "candidate", CreatedAt: s.CreatedAt, ProcessingStatus: status}}
				if err := visit(data, fakeListingRaw(data)); err != nil {
					return err
				}
				if scenario == "interrupted listing" {
					return context.DeadlineExceeded
				}
				return nil
			}
			fault.stream = func(_ context.Context, _ string, visit func(batchapi.Result) error) error {
				if scenario == "matching result" {
					return visit(storeResult(t, s.ID+"-000000", `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}]}`))
				}
				if scenario == "interrupted results" {
					return context.DeadlineExceeded
				}
				return nil
			}
			r.Client = fault
			_ = r.Reconcile(ctx, s.ID, true)
			if ledger.rows[0].State == "abandoned" {
				t.Fatal("incomplete or matching evidence allowed abandonment")
			}
			store, err := r.verifyStore(ledger.rows, nil)
			if err != nil {
				t.Fatal(err)
			}
			cost, err := store.ledgerCostFor("anthropic")
			if err != nil || cost != s.ReserveMicroUSD {
				t.Fatalf("reserve released: %d, %v", cost, err)
			}
		})
	}
}

func TestAbandonment_ReferencedListingMustRemainIntact(t *testing.T) {
	for _, scenario := range []string{"missing", "altered"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, client, o := screenFixture(t)
			client.beforeCreate = nil
			client.createErr = context.DeadlineExceeded
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected uncertain dispatch")
			}
			s := ledger.rows[0]
			r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
			if err := r.Reconcile(ctx, s.ID, true); err != nil {
				t.Fatal(err)
			}
			var record abandonmentRecord
			if err := readRetainedFile(filepath.Join(r.IntentDir, "submissions", s.ID+".abandoned"), &record); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(r.IntentDir, filepath.FromSlash(record.Path), captureRecordName(0))
			if scenario == "missing" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(path, append(b, ' '), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := r.verifyStore(ledger.rows, nil); err == nil {
				t.Fatal("admission accepted damaged listing")
			}
			if err := r.Rebuild(ctx); err == nil {
				t.Fatal("rebuild accepted damaged listing")
			}
		})
	}
}

func TestAbandonment_DispatchWithoutListingIsInconsistent(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	client.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain dispatch")
	}
	s := ledger.rows[0]
	r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
	if err := r.writeAbandonment(&s, captureReference{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.verifyStore(ledger.rows, nil); err == nil {
		t.Fatal("admission accepted abandonment without evidence")
	}
	if err := r.Rebuild(ctx); err == nil {
		t.Fatal("rebuild accepted abandonment without evidence")
	}
}
