package classify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

type rawCreateFixture struct {
	*fakeClient
	raw     string
	failure error
}

func (f *rawCreateFixture) Create(context.Context, []batchapi.Request) (anthropic.MessageBatch, error) {
	var result anthropic.MessageBatch
	if err := json.Unmarshal([]byte(f.raw), &result); err != nil {
		return result, err
	}
	return result, f.failure
}

func (f *rawCreateFixture) CreateRaw(context.Context, []batchapi.Request) ([]byte, int, error) {
	status := 200
	if f.failure != nil {
		status = 500
	}
	return []byte(f.raw), status, f.failure
}

func TestReview_ListingWindowEndEqualsStart(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	client.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain create")
	}
	s := ledger.rows[0]
	n := 0
	r.Now = func() time.Time { n++; return s.CreatedAt.Add(25*time.Hour + time.Duration(n)*time.Second) }
	if err := r.Reconcile(ctx, s.ID, true); err != nil {
		t.Fatal(err)
	}
	var record abandonmentRecord
	if err := readRetainedFile(filepath.Join(r.IntentDir, "submissions", s.ID+".abandoned"), &record); err != nil {
		t.Fatal(err)
	}
	var start captureStart
	if err := readRetainedFile(filepath.Join(r.IntentDir, filepath.FromSlash(record.Path), "start.json"), &start); err != nil {
		t.Fatal(err)
	}
	if start.Before == nil || start.CapturedAt == nil || !start.Before.Equal(*start.CapturedAt) {
		t.Fatal("listing end does not equal start time")
	}
}

func TestReview_UnpricedUsageDoesNotBlockIndependentSubmission(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = func(req []batchapi.Request) {
		client.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":{"input_tokens":10,"cache_creation_input_tokens":60,"cache_creation":"invalid","output_tokens":10}}`)}
	}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil || ledger.rows[0].Settled {
		t.Fatal("expected unpriced attempt")
	}
	first := ledger.rows[0]
	attempt, _, found, err := r.readAttempt(first, 0)
	if err != nil || !found || attempt.Cost != nil || !strings.Contains(attempt.Raw, `"cache_creation":"invalid"`) {
		t.Fatalf("unpriced attempt not retained: found=%t error=%v", found, err)
	}
	before := client.creates
	o.RunID++
	o.Release = "20990102"
	o.Out = t.TempDir()
	client.beforeCreate = func(req []batchapi.Request) {
		client.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":{"input_tokens":10,"output_tokens":10}}`)}
	}
	if _, screenErr := r.Screen(ctx, o, []Document{{Key: "vn:vbpl:2", Title: "Other"}}, families()); screenErr != nil {
		t.Fatalf("independent submission blocked: %v", screenErr)
	}
	if client.creates != before+1 || ledger.rows[0].Settled || ledger.rows[0].ReserveMicroUSD != first.ReserveMicroUSD || !ledger.rows[1].Settled {
		t.Fatal("unpriced submission reserve or independent result incorrect")
	}
	store, err := r.verifyStore(ledger.rows, nil)
	if err != nil {
		t.Fatal(err)
	}
	total, err := store.ledgerCostFor("anthropic")
	if err != nil || total != first.ReserveMicroUSD+ledger.rows[1].CostMicroUSD {
		t.Fatalf("incorrect retained reserve %d: %v", total, err)
	}
	o.RunID++
	o.Release = "20990103"
	o.Out = t.TempDir()
	prior := client.creates
	if _, err = r.Screen(ctx, o, documents(), families()); err != nil || client.creates != prior {
		t.Fatalf("unpriced key was resubmitted: %v", err)
	}
}

func TestReview_StoredCountRejectsCaseVariants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "count.json")
	key := strings.Repeat("a", 64)
	if err := os.WriteFile(path, []byte(`{"key":"`+key+`","INPUT_TOKENS":20}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCount(path, key); err == nil {
		t.Fatal("accepted case variant count")
	}
}

func TestReview_CreateResponseIsStoredBeforeValidation(t *testing.T) {
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	raw := `{"id":"batch_one","ID":"batch_two","created_at":"2099-01-01T00:00:00Z","processing_status":"in_progress"}`
	r.Client = &rawCreateFixture{fakeClient: client, raw: raw, failure: errors.New("synthetic provider error")}
	if _, err := r.Screen(context.Background(), o, documents(), families()); err == nil {
		t.Fatal("accepted ambiguous create response")
	}
	if len(ledger.rows) != 1 {
		t.Fatalf("rows %d", len(ledger.rows))
	}
	if _, err := os.Stat(filepath.Join(r.IntentDir, "submissions", ledger.rows[0].ID+".batch")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("attached ambiguous provider identity")
	}
	dir := filepath.Join(r.IntentDir, "evidence", ledger.rows[0].ID, "responses")
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("response captures %d error %v", len(files), err)
	}
	var saved providerResponse
	if err = readRetainedFile(filepath.Join(dir, files[0].Name()), &saved); err != nil || string(saved.Body) != raw {
		t.Fatalf("raw create response not preserved: %v", err)
	}
}

func TestReview_StoredIntentRejectsNestedCaseVariant(t *testing.T) {
	r, _, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	fields["recipe"].(map[string]any)["PROMPT_VERSION"] = float64(2)
	b, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "intent.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	var decoded Submission
	if err = readRetainedFile(path, &decoded); err == nil {
		t.Fatal("accepted nested recipe case variant")
	}
}

type rawGetFixture struct {
	*fakeClient
	raw     string
	failure error
}

func (f *rawGetFixture) Get(context.Context, string) (anthropic.MessageBatch, error) {
	var result anthropic.MessageBatch
	if err := json.Unmarshal([]byte(f.raw), &result); err != nil {
		return result, err
	}
	return result, f.failure
}

func (f *rawGetFixture) GetRaw(context.Context, string) ([]byte, int, error) {
	status := 200
	if f.failure != nil {
		status = 500
	}
	return []byte(f.raw), status, f.failure
}

func TestReview_GetResponseIsStoredBeforeValidation(t *testing.T) {
	r, ledger, client, o := screenFixture(t)
	raw := `{"id":"batch_synthetic","processing_status":"in_progress","PROCESSING_STATUS":"ended"}`
	r.Client = &rawGetFixture{fakeClient: client, raw: raw}
	if _, err := r.Screen(context.Background(), o, documents(), families()); err == nil {
		t.Fatal("accepted ambiguous Get response")
	}
	if len(ledger.rows) != 1 || ledger.rows[0].Settled || client.downloads != 0 {
		t.Fatal("ambiguous response caused results download or settlement")
	}
	dir := filepath.Join(r.IntentDir, "evidence", ledger.rows[0].ID, "responses")
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range files {
		var saved providerResponse
		if err = readRetainedFile(filepath.Join(dir, file.Name()), &saved); err != nil {
			t.Fatal(err)
		}
		found = found || (saved.Operation == "get" && saved.Status == 200 && string(saved.Body) == raw)
	}
	if !found {
		t.Fatal("did not retain raw Get response")
	}
}

func TestReview_StoredNumericFieldsRequireExactIntegers(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		value any
	}{
		{`{"records":null}`, &captureEnd{}},
		{`{"records":"1"}`, &captureEnd{}},
		{`{"usage":{"input_tokens":null}}`, &attemptRecord{}},
		{`{"reserved_micro_usd":1e2}`, &Submission{}},
	} {
		if err := validateRetainedJSON([]byte(tc.raw), tc.value); err == nil {
			t.Fatalf("accepted invalid numeric field: %s", tc.raw)
		}
	}
	var signed struct {
		Seed int64 `json:"seed"`
	}
	if err := validateRetainedJSON([]byte(`{"seed":-9223372036854775808}`), &signed); err != nil {
		t.Fatalf("valid signed seed rejected: %v", err)
	}
}

func TestReview_CreateErrorEnvelopeCannotBecomeBatchReceipt(t *testing.T) {
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	raw := `{"id":"error_identifier","processing_status":"in_progress","error":{"type":"api_error"}}`
	r.Client = &rawCreateFixture{fakeClient: client, raw: raw, failure: errors.New("synthetic error")}
	if _, err := r.Screen(context.Background(), o, documents(), families()); err == nil {
		t.Fatal("expected provider error")
	}
	if len(ledger.rows) != 1 {
		t.Fatal("missing uncertain intent")
	}
	if _, err := os.Stat(filepath.Join(r.IntentDir, "submissions", ledger.rows[0].ID+".batch")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("provider error envelope became batch receipt")
	}
}

func TestReview_EmptyProviderResponseRemainsReadable(t *testing.T) {
	r, _, _, _ := screenFixture(t)
	owner := strings.Repeat("a", 32)
	if _, err := r.captureBatchResponse(providerResponse{Owner: owner, Operation: "get", BatchID: "batch_synthetic", AccountFingerprint: strings.Repeat("a", 32), Status: 200, Body: []byte{}}); err != nil {
		t.Fatal(err)
	}
	if err := inspectProviderResponses(filepath.Join(r.IntentDir, "evidence", owner, "responses"), owner); err != nil {
		t.Fatal(err)
	}
}
