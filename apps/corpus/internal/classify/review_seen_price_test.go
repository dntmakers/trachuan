package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func addAmbiguousCreateCaptures(t *testing.T, r *Runner, s Submission) {
	t.Helper()
	refs := []providerRequestEvidence{{CustomID: customID(s.ID, 0), RequestHash: s.Items[0].RequestHash}}
	for _, at := range []time.Time{s.CreatedAt, s.CreatedAt.Add(time.Second)} {
		raw := fmt.Sprintf(`{"id":"batch_seen","processing_status":"in_progress","created_at":%q}`, at.Format(time.RFC3339Nano))
		_, err := r.captureBatchResponse(providerResponse{Owner: s.ID, Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: 200, Requests: refs, Body: []byte(raw)})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestReview_SeenBatchWithoutReceiptBlocksLiveAbandonment(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	client.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain create")
	}
	s := ledger.rows[0]
	addAmbiguousCreateCaptures(t, r, s)
	r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
	if err := r.Reconcile(ctx, s.ID, true); err == nil {
		t.Fatal("abandoned a submission with a seen batch ID")
	}
	if ledger.rows[0].State == "abandoned" {
		t.Fatal("seen batch ID failed to hold submission")
	}
}

func TestReview_SeenBatchWithoutReceiptBlocksAbandonedReplacement(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	client.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain create")
	}
	s := ledger.rows[0]
	r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
	if err := r.Reconcile(ctx, s.ID, true); err != nil {
		t.Fatal(err)
	}
	addAmbiguousCreateCaptures(t, r, s)
	store, err := r.verifyStore(ledger.rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	total, err := store.ledgerCost()
	if err != nil || store.retryAbandoned[s.ID] || total != s.ReserveMicroUSD {
		t.Fatalf("seen batch did not hold key and reserve: total=%d error=%v", total, err)
	}
}

func TestReview_B1PricedAttemptIgnoresUnrelatedMetadataDuplicates(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.ended = false
	client.beforeCreate = func(req []batchapi.Request) {
		client.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[\"1.1\"]}"}],"usage":{"input_tokens":10,"output_tokens":10},"metadata":{"x":1,"x":2}}`)}
	}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted polling")
	}
	s := ledger.rows[0]
	if err := r.captureResults(ctx, s.ID, s.BatchID, s.CreatedAt); err != nil {
		t.Fatal(err)
	}
	raw, err := rawResult(client.results[0])
	if err != nil {
		t.Fatal(err)
	}
	usage, cost := priceUsage(s, client.results[0].Message.Usage.RawJSON())
	if cost == nil {
		t.Fatal("fixture usage is not priceable")
	}
	fixture := attemptRecord{SubmissionID: s.ID, BatchID: s.BatchID, Index: 0, Key: s.Items[0].RequestHash, Outcome: "final", Raw: raw, Usage: usage, Cost: cost, PriceVersion: s.priceVersion()}
	b, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	path := r.attemptPath(s, 0)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	a, _, found, err := r.readAttempt(s, 0)
	if err != nil || !found || a.Cost == nil || *a.Cost != *cost {
		t.Fatalf("B1 priced fixture rejected: found=%t error=%v", found, err)
	}
	store, err := r.verifyStore(ledger.rows, nil)
	if err != nil {
		t.Fatalf("B1 priced attempt blocked store: %v", err)
	}
	total, err := store.ledgerCost()
	if err != nil || total != s.ReserveMicroUSD {
		t.Fatalf("unsettled reserve changed: %d %v", total, err)
	}
}

func TestReview_PricingUsesUsageEvenWhenNonpricingContentIsInvalid(t *testing.T) {
	recipe, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	s := Submission{ID: strings.Repeat("b", 32), BatchID: "batch", Recipe: recipe, PriceVersion: batchPriceVersion, Items: []Item{{RequestHash: strings.Repeat("c", 64)}}}
	raw := `{"type":"succeeded","message":{"stop_reason":"end_turn","content":false,"usage":{"input_tokens":10,"output_tokens":10}}}`
	a, _, err := parseAttempt(s, 0, raw)
	if err != nil || a.Outcome != "failed" || a.Cost == nil {
		t.Fatalf("valid usage was not priced independently: %+v %v", a, err)
	}
	expired := `{"type":"expired","metadata":{"a":1,"a":2}}`
	a, _, err = parseAttempt(s, 0, expired)
	if err != nil || a.Cost == nil || *a.Cost != 0 {
		t.Fatalf("expired response metadata changed zero price: %+v %v", a, err)
	}
}

func TestReview_ObservedIDWithoutCompleteBatchShapeStillHolds(t *testing.T) {
	for _, body := range []string{`{"id":"batch_seen"}`, `{"id":"batch_seen","processing_status":false}`, `{"id":"batch_seen","type":null}`, `{"id":"batch_seen","type":12}`} {
		t.Run(body, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, client, o := screenFixture(t)
			client.beforeCreate = nil
			client.createErr = context.DeadlineExceeded
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected uncertain create")
			}
			s := ledger.rows[0]
			response := providerResponse{Owner: s.ID, Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: 200, Requests: []providerRequestEvidence{{CustomID: customID(s.ID, 0), RequestHash: s.Items[0].RequestHash}}, Body: []byte(body)}
			if _, err := r.captureBatchResponse(response); err != nil {
				t.Fatal(err)
			}
			r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
			if err := r.Reconcile(ctx, s.ID, true); err == nil {
				t.Fatal("abandoned despite observed possible batch ID")
			}
		})
	}
}

func TestReview_ObservedIncompleteIDAfterAbandonmentKeepsReserve(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	client.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain create")
	}
	s := ledger.rows[0]
	r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
	if err := r.Reconcile(ctx, s.ID, true); err != nil {
		t.Fatal(err)
	}
	response := providerResponse{Owner: s.ID, Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: 200, Requests: []providerRequestEvidence{{CustomID: customID(s.ID, 0), RequestHash: s.Items[0].RequestHash}}, Body: []byte(`{"id":"batch_seen"}`)}
	if _, err := r.captureBatchResponse(response); err != nil {
		t.Fatal(err)
	}
	store, err := r.verifyStore(ledger.rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	total, err := store.ledgerCost()
	if err != nil || store.retryAbandoned[s.ID] || total != s.ReserveMicroUSD {
		t.Fatalf("incomplete ID did not hold reserve: total=%d error=%v", total, err)
	}
}

func TestReview_SeenIDBlocksTraceFreeAbandonment(t *testing.T) {
	r, ledger, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
	if err := r.writeAbandonment(&s, captureReference{}); err != nil {
		t.Fatal(err)
	}
	s.State = "abandoned"
	ledger.rows[0] = s
	response := providerResponse{Owner: s.ID, Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: 200, Requests: []providerRequestEvidence{{CustomID: customID(s.ID, 0), RequestHash: s.Items[0].RequestHash}}, Body: []byte(`{"id":"batch_seen"}`)}
	if _, err := r.captureBatchResponse(response); err != nil {
		t.Fatal(err)
	}
	store, err := r.verifyStore(ledger.rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	total, err := store.ledgerCost()
	if err != nil || store.retryAbandoned[s.ID] || total != s.ReserveMicroUSD {
		t.Fatalf("trace-free abandonment released a seen batch: %d %v", total, err)
	}
}

func TestReview_UnrelatedDuplicateMetadataNeverChangesPrice(t *testing.T) {
	recipe, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	s := Submission{ID: strings.Repeat("b", 32), BatchID: "batch", Recipe: recipe, PriceVersion: batchPriceVersion, Items: []Item{{RequestHash: strings.Repeat("c", 64)}}}
	bodies := []string{
		`{"type":"succeeded","metadata":1,"metadata":2,"message":{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":{"input_tokens":10,"output_tokens":10}}}`,
		`{"type":"succeeded","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"metadata":1,"metadata":2,"usage":{"input_tokens":10,"output_tokens":10}}}`,
		`{"type":"succeeded","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}","metadata":1,"metadata":2}],"usage":{"input_tokens":10,"output_tokens":10}}}`,
	}
	for _, raw := range bodies {
		a, _, err := parseAttempt(s, 0, raw)
		if err != nil || a.Outcome != "final" || a.Cost == nil {
			t.Fatalf("unrelated duplicate changed price: %+v %v", a, err)
		}
	}
}
