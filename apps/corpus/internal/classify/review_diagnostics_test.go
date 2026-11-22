package classify

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

func TestReview_NonBatchCreateAndGetBodiesRemainDiagnostics(t *testing.T) {
	for _, op := range []string{"create", "get"} {
		t.Run(op, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, client, o := screenFixture(t)
			raw := `{"type":"error","id":"diagnostic_id","error":{"type":"api_error"}}`
			if op == "create" {
				client.beforeCreate = nil
				r.Client = &statusResponseClient{fakeClient: client, raw: raw, status: 200}
			} else {
				r.Client = &rawGetFixture{fakeClient: client, raw: raw}
			}
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected invalid provider response")
			}
			if len(ledger.rows) != 1 {
				t.Fatal("missing submission")
			}
			s := ledger.rows[0]
			if op == "create" && s.BatchID != "" {
				t.Fatal("diagnostic became receipt")
			}
			if _, err := r.verifyStore(ledger.rows, nil); err != nil {
				t.Fatalf("diagnostic blocked store: %v", err)
			}
			if err := r.Rebuild(ctx); err != nil {
				t.Fatalf("diagnostic blocked rebuild: %v", err)
			}
			r.Client = client
			if op == "create" {
				if err := r.Reconcile(ctx, s.ID, false); err == nil || !strings.Contains(err.Error(), "no ended provider batch matches submission") {
					t.Fatalf("diagnostic prevented reconciliation: %v", err)
				}
			} else {
				client.ended = true
				if err := r.Reconcile(ctx, s.ID, false); err != nil || !ledger.rows[0].Settled {
					t.Fatalf("diagnostic prevented later settlement: %v", err)
				}
			}
		})
	}
}

func TestReview_B1FinalUnpricedAttemptReadsAndReplays(t *testing.T) {
	cases := []struct {
		name, usage string
		want        tokenUsage
	}{
		{"duplicate usage", `{"input_tokens":10,"input_tokens":20,"output_tokens":10}`, tokenUsage{}},
		{"malformed cache detail", `{"input_tokens":10,"cache_creation_input_tokens":60,"cache_creation":"invalid","output_tokens":10}`, tokenUsage{Input: 10, CacheWrite: 60, Output: 10}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			r, ledger, client, o := screenFixture(t)
			client.beforeCreate = func(req []batchapi.Request) {
				client.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[\"1.1\"]}"}],"usage":`+tc.usage+`}`)}
			}
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected unpriced response")
			}
			s := ledger.rows[0]
			raw, err := rawResult(client.results[0])
			if err != nil {
				t.Fatal(err)
			}
			fixture := attemptRecord{SubmissionID: s.ID, BatchID: s.BatchID, Index: 0, Key: s.Items[0].RequestHash, Outcome: "final", Raw: raw, Usage: tc.want, Cost: nil, PriceVersion: s.priceVersion()}
			b, err := json.Marshal(fixture)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(r.attemptPath(s, 0), b, 0600); err != nil {
				t.Fatal(err)
			}
			attempt, _, found, err := r.readAttempt(s, 0)
			if err != nil || !found || attempt.Outcome != "final" || attempt.Cost != nil {
				t.Fatalf("B1 final unpriced attempt rejected: found=%t error=%v", found, err)
			}
			store, err := r.verifyStore(ledger.rows, nil)
			if err != nil {
				t.Fatal(err)
			}
			total, err := store.ledgerCostFor("anthropic")
			if err != nil || total != s.ReserveMicroUSD {
				t.Fatalf("unpriced reserve %d error %v", total, err)
			}
			o.RunID++
			o.Release = "20990102"
			o.Out = filepath.Join(t.TempDir(), "replay")
			before := client.creates
			if _, err = r.Screen(ctx, o, documents(), families()); err != nil || client.creates != before {
				t.Fatalf("B1 final did not replay: creates=%d error=%v", client.creates, err)
			}
			artifact, err := os.ReadFile(filepath.Join(o.Out, "families.json"))
			if err != nil || !strings.Contains(string(artifact), "1.1") {
				t.Fatalf("replayed family missing: %v", err)
			}
		})
	}
}

func TestReview_UsageIssuesDoNotChangeValidContentOutcome(t *testing.T) {
	recipe, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	s := Submission{ID: strings.Repeat("b", 32), BatchID: "batch", Recipe: recipe, PriceVersion: batchPriceVersion, Items: []Item{{RequestHash: strings.Repeat("c", 64)}}}
	for _, usage := range []string{`"bad"`, `{"input_tokens":1,"INPUT_TOKENS":2,"output_tokens":1}`, `{"input_tokens":1,"input_tokens":2,"output_tokens":1}`} {
		raw := `{"type":"succeeded","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[\"1.1\"]}"}],"usage":` + usage + `}}`
		a, _, parseErr := parseAttempt(s, 0, raw)
		if parseErr != nil || a.Outcome != "final" || a.Cost != nil {
			t.Fatalf("valid content lost final outcome for %s: %+v %v", usage, a, parseErr)
		}
	}
	for _, usageFields := range []string{
		`"usage":{"input_tokens":1,"output_tokens":1},"usage":{"input_tokens":2,"output_tokens":2}`,
		`"USAGE":{"input_tokens":1,"output_tokens":1}`,
	} {
		raw := `{"type":"succeeded","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[\"1.1\"]}"}],` + usageFields + `}}`
		a, _, err := parseAttempt(s, 0, raw)
		if err != nil || a.Outcome != "final" || a.Cost != nil {
			t.Fatalf("message usage ambiguity changed final result: %+v %v", a, err)
		}
	}
}

func TestReview_DiagnosticBodiesDoNotOverrideReceiptOrPoisonStore(t *testing.T) {
	r, _, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	refs := []providerRequestEvidence{{CustomID: customID(s.ID, 0), RequestHash: s.Items[0].RequestHash}}
	saved := batchRecord{ID: "batch_owned", SubmittedAt: s.CreatedAt, DeadlineAt: s.CreatedAt.Add(26 * time.Hour)}
	for _, raw := range []string{
		`{}`, `[]`, `null`, `{"type":"error","id":"diagnostic_id","error":{"type":"api_error"}}`,
		`{"id":"batch_owned","processing_status":"ended","created_at":"not-a-date"}`,
		`{"id":"batch_owned","PROCESSING_STATUS":"ended","created_at":"2099-01-01T00:00:00Z"}`,
		`{"id":"diagnostic_id","processing_status":false}`,
		`{"id":"diagnostic_id","processing_status":"unexpected"}`,
		`{"id":"diagnostic_id","PROCESSING_STATUS":"ended"}`,
		`{"type":"diagnostic","id":"diagnostic_id"}`,
	} {
		response := providerResponse{Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: 200, Requests: refs, Body: []byte(raw)}
		receipt, found, _, _, err := verifyReceipt(s, &saved, []providerResponse{response})
		if err != nil || !found || receipt.ID != saved.ID {
			t.Fatalf("diagnostic blocked valid receipt for %s: found=%t error=%v", raw, found, err)
		}
	}
}

func TestReview_ContradictoryBatchIdentitiesStillStopStore(t *testing.T) {
	r, _, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	refs := []providerRequestEvidence{{CustomID: customID(s.ID, 0), RequestHash: s.Items[0].RequestHash}}
	saved := batchRecord{ID: "batch_owned", SubmittedAt: s.CreatedAt, DeadlineAt: s.CreatedAt.Add(26 * time.Hour)}
	for _, raw := range []string{
		`{"id":"batch_other","processing_status":"ended","created_at":"invalid"}`,
		`{"id":"batch_owned","ID":"batch_other","processing_status":"ended"}`,
		`{"id":"batch_owned","id":"batch_other","processing_status":"ended"}`,
	} {
		response := providerResponse{Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: 200, Requests: refs, Body: []byte(raw)}
		if _, _, _, _, err := verifyReceipt(s, &saved, []providerResponse{response}); err == nil {
			t.Fatalf("accepted contradictory identity in %s", raw)
		}
	}
}

func TestReview_UnverifiedTimestampsDoNotPoisonReceiptStore(t *testing.T) {
	r, _, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	refs := []providerRequestEvidence{{CustomID: customID(s.ID, 0), RequestHash: s.Items[0].RequestHash}}
	body := func(at time.Time) []byte {
		return []byte(`{"id":"batch_owned","processing_status":"in_progress","created_at":"` + at.Format(time.RFC3339) + `"}`)
	}
	responses := []providerResponse{{Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: 200, Requests: refs, Body: body(s.CreatedAt)}, {Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: 200, Requests: refs, Body: body(s.CreatedAt.Add(time.Second))}}
	if _, found, _, _, err := verifyReceipt(s, nil, responses); err != nil || found {
		t.Fatalf("conflicting nonidentity metadata must not poison store or establish receipt: found=%t error=%v", found, err)
	}
}

func TestReview_DuplicateBatchIdentityAcrossSubmissionsStopsStore(t *testing.T) {
	r, ledger, _, o := screenFixture(t)
	first := seedUndispatchedScreen(t, r, o, documents())
	second := first
	second.ID = strings.Repeat("f", 32)
	second.RunID++
	second.ScreenHash = strings.Repeat("e", 64)
	if err := r.writeIntent(second); err != nil {
		t.Fatal(err)
	}
	if err := r.recordBatch(&first, "batch_shared"); err != nil {
		t.Fatal(err)
	}
	if err := r.recordBatch(&second, "batch_shared"); err != nil {
		t.Fatal(err)
	}
	ledger.rows = []Submission{first, second}
	if _, err := r.verifyStore(ledger.rows, nil); err == nil {
		t.Fatal("two submissions claimed one batch identity")
	}
}

func TestReview_BatchLookingDiagnosticCannotClaimAnotherSubmission(t *testing.T) {
	r, ledger, _, o := screenFixture(t)
	first := seedUndispatchedScreen(t, r, o, documents())
	second := first
	second.ID = strings.Repeat("f", 32)
	second.RunID++
	second.ScreenHash = strings.Repeat("e", 64)
	if err := r.writeIntent(second); err != nil {
		t.Fatal(err)
	}
	if err := r.recordBatch(&first, "batch_shared"); err != nil {
		t.Fatal(err)
	}
	response := providerResponse{Owner: second.ID, Operation: "create", AccountFingerprint: second.AccountFingerprint, Status: 200, Requests: []providerRequestEvidence{{CustomID: customID(second.ID, 0), RequestHash: second.Items[0].RequestHash}}, Body: []byte(`{"id":"batch_shared","processing_status":"ended"}`)}
	if _, err := r.captureBatchResponse(response); err != nil {
		t.Fatal(err)
	}
	ledger.rows = []Submission{first, second}
	if _, err := r.verifyStore(ledger.rows, nil); err == nil {
		t.Fatal("diagnostic claimed another submission batch")
	}
}

func TestReview_InvalidGetMetadataWithSameIdentityDoesNotPoisonStore(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	raw := `{"id":"batch_synthetic","processing_status":false,"created_at":"invalid"}`
	r.Client = &rawGetFixture{fakeClient: client, raw: raw}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected unusable get response")
	}
	if _, err := r.verifyStore(ledger.rows, nil); err != nil {
		t.Fatalf("nonidentity get metadata poisoned store: %v", err)
	}
	if err := r.Rebuild(ctx); err != nil {
		t.Fatalf("nonidentity get metadata blocked rebuild: %v", err)
	}
}

func TestReview_NonContentMetadataAmbiguityKeepsFinalAndPrice(t *testing.T) {
	recipe, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	s := Submission{ID: strings.Repeat("b", 32), BatchID: "batch", Recipe: recipe, PriceVersion: batchPriceVersion, Items: []Item{{RequestHash: strings.Repeat("c", 64)}}}
	raw := `{"type":"succeeded","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":{"input_tokens":1,"output_tokens":1},"extra":1,"extra":2}}`
	a, _, err := parseAttempt(s, 0, raw)
	if err != nil || a.Outcome != "final" || a.Cost == nil {
		t.Fatalf("unrelated metadata changed final outcome or price: %+v %v", a, err)
	}
}

func TestReview_B1FinalUnpricedUsageSnapshotRemainsReadable(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = func(req []batchapi.Request) {
		client.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[\"1.1\"]}"}],"usage":{"input_tokens":10,"cache_creation_input_tokens":60,"cache_creation":"invalid","output_tokens":10},"usage":{"input_tokens":20,"cache_creation_input_tokens":70,"cache_creation":"invalid","output_tokens":20}}`)}
	}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected unpriced result")
	}
	s := ledger.rows[0]
	raw, err := rawResult(client.results[0])
	if err != nil {
		t.Fatal(err)
	}
	b1Usage, b1Cost := priceUsage(s, client.results[0].Message.Usage.RawJSON())
	if b1Cost != nil || b1Usage.Input == 0 {
		t.Fatal("fixture does not represent B1 partial usage")
	}
	fixture := attemptRecord{SubmissionID: s.ID, BatchID: s.BatchID, Index: 0, Key: s.Items[0].RequestHash, Outcome: "final", Raw: raw, Usage: b1Usage, Cost: nil, PriceVersion: s.priceVersion()}
	b, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(r.attemptPath(s, 0), b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, found, readErr := r.readAttempt(s, 0); readErr != nil || !found {
		t.Fatalf("historical unpriced usage snapshot rejected: found=%t error=%v", found, readErr)
	}
	o.RunID++
	o.Release = "20990102"
	o.Out = filepath.Join(t.TempDir(), "replay")
	before := client.creates
	if _, err = r.Screen(ctx, o, documents(), families()); err != nil || client.creates != before {
		t.Fatalf("historical unpriced usage snapshot did not replay: %v", err)
	}
	preserved, readErr := os.ReadFile(r.attemptPath(s, 0))
	if readErr != nil || !bytes.Equal(preserved, b) {
		t.Fatalf("historical attempt changed: %v", readErr)
	}
	store, err := r.verifyStore(ledger.rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	total, err := store.ledgerCostFor("anthropic")
	if err != nil || total != s.ReserveMicroUSD {
		t.Fatalf("historical reserve changed: %d %v", total, err)
	}
}

func TestReview_ExistingAttemptStillRejectsIncomingIdentityMismatch(t *testing.T) {
	r, ledger, _, o := screenFixture(t)
	if _, err := r.Screen(context.Background(), o, documents(), families()); err != nil {
		t.Fatal(err)
	}
	s := ledger.rows[0]
	a, _, found, err := r.readAttempt(s, 0)
	if err != nil || !found {
		t.Fatalf("missing attempt: %v", err)
	}
	a.SubmissionID = strings.Repeat("f", 32)
	if err = r.writeAttempt(s, 0, a); err == nil {
		t.Fatal("accepted mismatched incoming attempt identity")
	}
}
