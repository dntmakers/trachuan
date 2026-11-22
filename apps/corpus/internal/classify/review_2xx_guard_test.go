package classify

import (
	"context"
	"fmt"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

type createBodyCase struct {
	status int
	raw    string
}

func successfulCreateCases() []createBodyCase {
	return []createBodyCase{
		{200, `{"id":"batch_seen",`},
		{200, `{"type":"error","error":{"type":"api_error"}}`},
		{201, `null`},
		{204, ``},
		{299, `[]`},
	}
}

func TestReview_Any2xxCreateBlocksLiveListingAbandonment(t *testing.T) {
	for _, tc := range successfulCreateCases() {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.raw), func(t *testing.T) {
			ctx := context.Background()
			r, ledger, client, o := screenFixture(t)
			client.beforeCreate = nil
			r.Client = &statusResponseClient{fakeClient: client, raw: tc.raw, status: tc.status}
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected unusable create response")
			}
			s := ledger.rows[0]
			_, found, _, successful, receiptErr := r.retainedReceipt(s)
			if receiptErr != nil || found || !successful {
				t.Fatalf("2xx diagnostic incorrectly granted receipt or lost evidence: found=%t error=%v", found, receiptErr)
			}
			r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
			r.Client = client
			if err := r.Reconcile(ctx, s.ID, true); err == nil {
				t.Fatal("2xx create allowed listing abandonment")
			}
			if ledger.rows[0].State == "abandoned" {
				t.Fatal("submission was abandoned")
			}
			client.results = []batchapi.Result{{CustomID: customID(s.ID, 0), Type: "expired"}}
			if err := r.Reconcile(ctx, s.ID, false); err != nil || !ledger.rows[0].Settled {
				t.Fatalf("2xx diagnostic blocked matching-batch reconciliation: %v", err)
			}
		})
	}
}

func TestReview_Any2xxCreateKeepsAbandonedReserve(t *testing.T) {
	for _, tc := range successfulCreateCases() {
		t.Run(fmt.Sprintf("%d/%s", tc.status, tc.raw), func(t *testing.T) {
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
			response := providerResponse{Owner: s.ID, Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: tc.status, Requests: []providerRequestEvidence{{CustomID: customID(s.ID, 0), RequestHash: s.Items[0].RequestHash}}, Body: []byte(tc.raw)}
			if _, err := r.captureBatchResponse(response); err != nil {
				t.Fatal(err)
			}
			store, err := r.verifyStore(ledger.rows, nil)
			if err != nil {
				t.Fatal(err)
			}
			total, err := store.ledgerCostFor("anthropic")
			if err != nil || store.retryAbandoned[s.ID] || total != s.ReserveMicroUSD {
				t.Fatalf("2xx evidence released key or reserve: total=%d error=%v", total, err)
			}
			client.createErr = nil
			o.RunID++
			o.Release = "20990102"
			o.Out = t.TempDir()
			before := client.creates
			summary, screenErr := r.Screen(ctx, o, documents(), families())
			if screenErr != nil || client.creates != before || summary.UnknownKeys != 1 {
				t.Fatalf("2xx evidence allowed resubmission: creates=%d summary=%+v error=%v", client.creates, summary, screenErr)
			}
		})
	}
}

func TestReview_Non2xxCreateDoesNotBlockListingAbandonment(t *testing.T) {
	for _, status := range []int{0, 199, 300, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			ctx := context.Background()
			r, ledger, client, o := screenFixture(t)
			client.beforeCreate = nil
			r.Client = &statusResponseClient{fakeClient: client, raw: `{"type":"error","error":{"type":"api_error"}}`, status: status}
			if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
				t.Fatal("expected non-2xx create error")
			}
			s := ledger.rows[0]
			r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
			r.Client = client
			if err := r.Reconcile(ctx, s.ID, true); err != nil {
				t.Fatalf("non-2xx diagnostic blocked valid abandonment: %v", err)
			}
			if ledger.rows[0].State != "abandoned" {
				t.Fatal("expected abandonment")
			}
		})
	}
}

func TestReview_Late2xxCreateCanRetryAfterRealBatchSettles(t *testing.T) {
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
	response := providerResponse{Owner: s.ID, Operation: "create", AccountFingerprint: s.AccountFingerprint, Status: 200, Requests: []providerRequestEvidence{{CustomID: customID(s.ID, 0), RequestHash: s.Items[0].RequestHash}}, Body: []byte(`{"id":"batch_seen",`)}
	if _, err := r.captureBatchResponse(response); err != nil {
		t.Fatal(err)
	}
	client.createErr = nil
	client.results = []batchapi.Result{{CustomID: customID(s.ID, 0), Type: "expired"}}
	if err := r.Reconcile(ctx, s.ID, false); err != nil || !ledger.rows[0].Settled {
		t.Fatalf("real batch did not settle: %v", err)
	}
	o.RunID++
	o.Release = "20990102"
	o.Out = t.TempDir()
	before := client.creates
	client.ended = true
	client.beforeCreate = func(req []batchapi.Request) {
		client.results = []batchapi.Result{{CustomID: req[0].CustomID, Type: "expired"}}
	}
	if _, err := r.Screen(ctx, o, documents(), families()); err != nil || client.creates != before+1 {
		t.Fatalf("settled expired key could not retry: creates=%d error=%v", client.creates, err)
	}
}
