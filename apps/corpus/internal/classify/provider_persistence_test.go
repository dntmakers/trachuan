package classify

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

type providerFault struct {
	*fakeClient
	stream func(context.Context, string, func(batchapi.Result) error) error
	pages  func(context.Context, func([]anthropic.MessageBatch, string) error) error
	count  func(context.Context, anthropic.MessageNewParams) (int64, error)
}

func (f *providerFault) StreamResults(ctx context.Context, id string, visit func(batchapi.Result) error) error {
	return f.stream(ctx, id, visit)
}

func (f *providerFault) StreamPages(ctx context.Context, visit func([]anthropic.MessageBatch, string) error) error {
	return f.pages(ctx, visit)
}

func (f *providerFault) CountTokens(ctx context.Context, p anthropic.MessageNewParams) (int64, error) {
	if f.count != nil {
		return f.count(ctx, p)
	}
	return f.fakeClient.CountTokens(ctx, p)
}

func hasCaptured(t *testing.T, root, needle string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(root, func(path string, f fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if f.IsDir() {
			return nil
		}
		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		found = found || strings.Contains(string(b), needle)
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return found
}

func TestProviderPersistence_StreamFailureKeepsDecodedLine(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	fault := &providerFault{fakeClient: f}
	r.Client = fault
	fault.stream = func(_ context.Context, _ string, visit func(batchapi.Result) error) error {
		if err := visit(f.results[0]); err != nil {
			return err
		}
		if !hasCaptured(t, filepath.Join(r.IntentDir, "evidence"), f.results[0].CustomID) {
			t.Error("decoded line was not durable before reading again")
		}
		return context.DeadlineExceeded
	}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil || ledger.rows[0].Settled {
		t.Fatalf("partial stream settled: %v", err)
	}
	if !hasCaptured(t, filepath.Join(r.IntentDir, "evidence"), "timed out") {
		t.Fatal("download error not recorded")
	}
}

func TestProviderPersistence_ReconcileKeepsEarlierBatch(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.ended = false
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	known := ledger.rows[0]
	knownResults := f.results
	f.createErr = context.DeadlineExceeded
	f.beforeCreate = nil
	o.RunID++
	o.Out = filepath.Join(t.TempDir(), "uncertain")
	if _, err := r.Screen(ctx, o, []Document{{Key: "vn:vbpl:2", Title: "other"}}, families()); err == nil {
		t.Fatal("expected lost create response")
	}
	fault := &providerFault{fakeClient: f}
	r.Client = fault
	fault.pages = func(_ context.Context, visit func([]anthropic.MessageBatch, string) error) error {
		return visit([]anthropic.MessageBatch{{ID: known.BatchID, CreatedAt: known.CreatedAt, ProcessingStatus: "ended"}, {ID: "batch_later", CreatedAt: known.CreatedAt, ProcessingStatus: "ended"}}, `{"data":[],"has_more":false,"test_marker":"first page"}`)
	}
	fault.stream = func(_ context.Context, id string, visit func(batchapi.Result) error) error {
		if id == known.BatchID {
			return visit(knownResults[0])
		}
		if !hasCaptured(t, filepath.Join(r.IntentDir, "evidence"), knownResults[0].CustomID) {
			t.Error("earlier batch not durable before next download")
		}
		return context.DeadlineExceeded
	}
	if err := r.Reconcile(ctx, ledger.rows[1].ID, false); err == nil {
		t.Fatal("later download failure was ignored")
	}
	if !ledger.rows[0].Settled || ledger.rows[1].Settled {
		t.Fatal("earlier known batch not settled independently")
	}
}

func TestProviderPersistence_ListFailureKeepsEarlierPage(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.createErr = context.DeadlineExceeded
	f.beforeCreate = nil
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected lost create response")
	}
	fault := &providerFault{fakeClient: f}
	r.Client = fault
	fault.pages = func(_ context.Context, visit func([]anthropic.MessageBatch, string) error) error {
		if err := visit(nil, `{"data":[],"has_more":true,"test_marker":"earlier list page"}`); err != nil {
			return err
		}
		if !hasCaptured(t, filepath.Join(r.IntentDir, "evidence"), "earlier list page") {
			t.Error("page not durable before fetching the next page")
		}
		return context.DeadlineExceeded
	}
	if err := r.Reconcile(ctx, ledger.rows[0].ID, false); err == nil || ledger.rows[0].Settled {
		t.Fatalf("partial listing settled: %v", err)
	}
}

type interruptedRepository struct {
	*memoryLedger
	down bool
}

func (m *interruptedRepository) Lock(ctx context.Context) (func(), error) {
	if m.down {
		return nil, errors.New("synthetic database down")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.memoryLedger.Lock(ctx)
}

func TestProviderPersistence_CreateReceiptSurvivesAttachFailure(t *testing.T) {
	for _, mode := range []string{"cancelled", "database_down"} {
		t.Run(mode, func(t *testing.T) {
			r, ledger, f, o := screenFixture(t)
			repo := &interruptedRepository{memoryLedger: ledger}
			r.Repository = repo
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			f.beforeCreate = func(_ []batchapi.Request) {
				if mode == "cancelled" {
					cancel()
				} else {
					repo.down = true
				}
			}
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected attach failure")
			}
			id := ledger.rows[0].ID
			if _, err := os.Stat(filepath.Join(r.IntentDir, "submissions", id+".batch")); err != nil {
				t.Errorf("receipt not durable: %v", err)
			}
			repo.down = false
			ledger.rows = nil
			if err := r.Rebuild(context.Background()); err != nil || len(ledger.rows) != 1 || ledger.rows[0].BatchID == "" {
				t.Fatalf("rebuild lost batch id: rows %d, error %v", len(ledger.rows), err)
			}
		})
	}
}

func TestProviderPersistence_DryRunRetainsCountBeforeLaterFailure(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	o.DryRun = true
	o.Documents = 2
	calls := 0
	var firstKey string
	fault := &providerFault{fakeClient: f, count: func(_ context.Context, p anthropic.MessageNewParams) (int64, error) {
		calls++
		if calls == 1 {
			var err error
			firstKey, err = requestKey(&batchapi.Request{Params: p})
			if err != nil {
				t.Fatal(err)
			}
			return 100, nil
		}
		if _, err := os.Stat(filepath.Join(r.IntentDir, "counts", firstKey+".json")); err != nil {
			t.Errorf("first count not durable before second call: %v", err)
		}
		return 0, context.DeadlineExceeded
	}}
	r.Client = fault
	if _, err := r.Screen(ctx, o, append(documents(), Document{Key: "vn:vbpl:2", Title: "other"}), families()); err == nil || len(ledger.rows) != 0 {
		t.Fatalf("failed dry run wrote ledger: %v", err)
	}
}

func TestProviderPersistence_RebuildUsesCompleteCaptureAfterDatabaseFailure(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	repo := &interruptedRepository{memoryLedger: ledger}
	r.Repository = repo
	r.Client = &providerFault{fakeClient: f, stream: func(_ context.Context, _ string, visit func(batchapi.Result) error) error {
		if err := visit(f.results[0]); err != nil {
			return err
		}
		repo.down = true
		return nil
	}}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected database failure after download")
	}
	repo.down = false
	ledger.rows = nil
	r.Client = nil
	if err := r.Rebuild(ctx); err != nil || len(ledger.rows) != 1 || !ledger.rows[0].Settled || ledger.rows[0].CostMicroUSD != 8 {
		t.Fatalf("rebuild did not use a verified complete download: rows %v, error %v", ledger.rows, err)
	}
}

func TestProviderPersistence_ConflictingCountDoesNotOverwrite(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	o.DryRun = true
	var key string
	r.Client = &providerFault{fakeClient: f, count: func(_ context.Context, p anthropic.MessageNewParams) (int64, error) {
		var err error
		key, err = requestKey(&batchapi.Request{Params: p})
		if err != nil {
			t.Fatal(err)
		}
		if err = r.writeCount(key, 99); err != nil {
			t.Fatal(err)
		}
		return 100, nil
	}}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil || len(ledger.rows) != 0 {
		t.Fatalf("conflicting count admitted: %v", err)
	}
	if n, err := readCount(r.countPath(key), key); n != 99 || err != nil {
		t.Fatalf("first count replaced: %d, %v", n, err)
	}
}

func TestProviderPersistence_MultipleCandidatesStayUnattached(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.createErr = context.DeadlineExceeded
	f.beforeCreate = nil
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected lost create response")
	}
	s := ledger.rows[0]
	r.Client = &providerFault{fakeClient: f, pages: func(_ context.Context, visit func([]anthropic.MessageBatch, string) error) error {
		return visit([]anthropic.MessageBatch{
			{ID: "batch_first", CreatedAt: s.CreatedAt, ProcessingStatus: "ended"},
			{ID: "batch_second", CreatedAt: s.CreatedAt, ProcessingStatus: "ended"},
		}, "")
	}, stream: func(_ context.Context, id string, visit func(batchapi.Result) error) error {
		return visit(batchapi.Result{CustomID: customID(s.ID, 0), Type: "expired", RawJSON: `{"type":"expired","source":"` + id + `"}`})
	}}
	if err := r.Reconcile(ctx, s.ID, false); err == nil || ledger.rows[0].Settled || ledger.rows[0].BatchID != "" {
		t.Fatalf("ambiguous batch was attached: %v", err)
	}
	for _, id := range []string{"batch_first", "batch_second"} {
		if !hasCaptured(t, filepath.Join(r.IntentDir, "evidence", s.ID, captureScope(id)), customID(s.ID, 0)) {
			t.Fatalf("candidate %s lost raw evidence", id)
		}
	}
}

func TestProviderPersistence_CandidatePrefixAloneDoesNotAttach(t *testing.T) {
	ctx := context.Background()
	r, ledger, f, o := screenFixture(t)
	f.createErr = context.DeadlineExceeded
	f.beforeCreate = nil
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected lost create response")
	}
	s := ledger.rows[0]
	f.results = []batchapi.Result{{CustomID: customID(s.ID, 37), Type: "expired"}}
	if err := r.Reconcile(ctx, s.ID, false); err == nil || ledger.rows[0].BatchID != "" {
		t.Fatalf("foreign item alone attached a candidate: %v", err)
	}
	if !hasCaptured(t, filepath.Join(r.IntentDir, "evidence", s.ID, captureScope("batch_synthetic")), f.results[0].CustomID) {
		t.Fatal("invalid candidate lost captured evidence")
	}
}
