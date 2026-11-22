package classify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

type statusResponseClient struct {
	*fakeClient
	raw     string
	status  int
	failure error
}

func (c *statusResponseClient) Create(_ context.Context, _ []batchapi.Request) (anthropic.MessageBatch, error) {
	var batch anthropic.MessageBatch
	if err := json.Unmarshal([]byte(c.raw), &batch); err != nil {
		return batch, err
	}
	return batch, c.failure
}
func (c *statusResponseClient) CreateRaw(context.Context, []batchapi.Request) ([]byte, int, error) {
	return []byte(c.raw), c.status, c.failure
}
func (c *statusResponseClient) GetRaw(context.Context, string) ([]byte, int, error) {
	return nil, 0, errors.New("synthetic")
}

func TestReview_CreateResponseRecoversMissingReceipt(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	raw := `{"id":"batch_recovered","created_at":"2099-01-01T00:00:00Z","processing_status":"in_progress"}`
	r.Client = &statusResponseClient{fakeClient: client, raw: raw, status: 200, failure: errors.New("synthetic interruption")}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected interrupted create")
	}
	if len(ledger.rows) != 1 {
		t.Fatal("missing intent")
	}
	id := ledger.rows[0].ID
	captures, captureErr := readProviderResponses(filepath.Join(r.IntentDir, "evidence", id, "responses"), id)
	if captureErr != nil || len(captures) != 1 || captures[0].Status != 200 || string(captures[0].Body) != raw {
		t.Fatalf("successful response status/body not retained: %v", captureErr)
	}
	path := filepath.Join(r.IntentDir, "submissions", id+".batch")
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	ledger.rows = nil
	if err := r.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	if len(ledger.rows) != 1 || ledger.rows[0].BatchID != "batch_recovered" {
		t.Fatalf("raw create did not restore receipt: %+v", ledger.rows)
	}
	r.Now = func() time.Time { return ledger.rows[0].CreatedAt.Add(25 * time.Hour) }
	if err := r.Reconcile(ctx, id, true); err == nil {
		t.Fatal("abandoned despite recovered receipt")
	}
}

func TestReview_Non2xxBatchLookingBodyNeverCreatesReceipt(t *testing.T) {
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	raw := `{"id":"batch_untrusted","created_at":"2099-01-01T00:00:00Z","processing_status":"in_progress"}`
	r.Client = &statusResponseClient{fakeClient: client, raw: raw, status: 500, failure: errors.New("synthetic error")}
	if _, err := r.Screen(context.Background(), o, documents(), families()); err == nil {
		t.Fatal("expected provider error")
	}
	if len(ledger.rows) != 1 {
		t.Fatal("missing intent")
	}
	id := ledger.rows[0].ID
	captures, captureErr := readProviderResponses(filepath.Join(r.IntentDir, "evidence", id, "responses"), id)
	if captureErr != nil || len(captures) != 1 || captures[0].Status != 500 || string(captures[0].Body) != raw {
		t.Fatalf("failed response status/body not retained: %v", captureErr)
	}
	if _, err := os.Stat(filepath.Join(r.IntentDir, "submissions", ledger.rows[0].ID+".batch")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("non-2xx response became receipt")
	}
}

func TestReview_AmbiguousUsageDoesNotBlockIndependentSubmission(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = func(req []batchapi.Request) {
		client.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":{"input_tokens":10,"INPUT_TOKENS":20,"output_tokens":10}}`)}
	}
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected unpriced result")
	}
	first := ledger.rows[0]
	attempt, _, found, err := r.readAttempt(first, 0)
	if err != nil || !found || attempt.Cost != nil {
		t.Fatalf("unpriced attempt not recorded: found %t error %v", found, err)
	}
	o.RunID++
	o.Release = "20990102"
	o.Out = t.TempDir()
	client.beforeCreate = func(req []batchapi.Request) {
		client.results = []batchapi.Result{storeResult(t, req[0].CustomID, `{"stop_reason":"end_turn","content":[{"type":"text","text":"{\"codes\":[]}"}],"usage":{"input_tokens":10,"output_tokens":10}}`)}
	}
	if _, err = r.Screen(ctx, o, []Document{{Key: "vn:vbpl:2", Title: "Other"}}, families()); err != nil {
		t.Fatalf("independent key blocked: %v", err)
	}
	if ledger.rows[0].Settled || !ledger.rows[1].Settled {
		t.Fatal("wrong settlement scope")
	}
}

func TestReview_RequiredEvidenceFieldsAndRootCannotBeMissing(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		value any
	}{
		{`{}`, &captureEnd{}}, {`null`, &captureEnd{}}, {`{"records":null}`, &captureEnd{}},
		{`{"intent_id":"x"}`, &captureStart{}}, {`{"provider_job_id":"x"}`, &batchRecord{}},
		{`{"id":"x"}`, &abandonmentRecord{}}, {`{"submission_id":"x"}`, &attemptRecord{}},
		{`{"id":"x"}`, &settlementRecord{}},
	} {
		if err := validateRetainedJSON([]byte(tc.raw), tc.value); err == nil {
			t.Fatalf("accepted incomplete %T: %s", tc.value, tc.raw)
		}
	}
	if _, err := readCount(filepath.Join(t.TempDir(), "missing"), strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
}

func TestReview_SettlementUsesOnlyActuallySubmittedIndices(t *testing.T) {
	recipe, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	s := Submission{ID: strings.Repeat("b", 32), KeyVersion: keyVersion, PriceVersion: batchPriceVersion, Recipe: recipe,
		Items:   []Item{{Key: "replay", Local: true, LocalReason: keyReplay}, {Key: "held", Local: true, LocalReason: keyHeld}, {Key: "unknown", Local: true, LocalReason: keyUnknown}, {Key: "sent", RequestHash: strings.Repeat("d", 64)}},
		Results: map[string]Outcome{"replay": {Outcome: "ok"}, "held": {Outcome: "invalid_response"}, "unknown": {Outcome: "invalid_response"}},
	}
	refs := []providerRequestEvidence{{CustomID: customID(s.ID, 3), RequestHash: strings.Repeat("d", 64)}}
	if err = verifyRequestScope(s, refs); err != nil {
		t.Fatalf("rejected actual submitted scope: %v", err)
	}
	if err = verifyRequestScope(s, []providerRequestEvidence{{CustomID: customID(s.ID, 1), RequestHash: strings.Repeat("d", 64)}}); err == nil {
		t.Fatal("accepted local item in sent scope")
	}
	result := batchapi.Result{CustomID: customID(s.ID, 3), Type: "expired", RawJSON: `{"type":"expired"}`}
	download := batchDownload{results: []batchapi.Result{result}, proofs: []captureReference{{Path: "synthetic", Hash: strings.Repeat("a", 64)}}}
	indexes, err := matchedDownloadIndexes(s, download.results)
	if err != nil || len(indexes) != 1 || indexes[result.CustomID] != 3 {
		t.Fatalf("matched indices %+v error %v", indexes, err)
	}
	cost, outcomes, err := priceCompleteDownload(s, download)
	if err != nil || cost != 0 || len(outcomes) != 4 {
		t.Fatalf("mixed settlement cost %d outcomes %d error %v", cost, len(outcomes), err)
	}
	if _, err = matchedDownloadIndexes(s, []batchapi.Result{{CustomID: customID(s.ID, 1)}}); err == nil {
		t.Fatal("accepted result for unsent local item")
	}
}

func TestReview_ResponseDefectsAreUnpricedButEnvelopeDefectsFail(t *testing.T) {
	recipe, err := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	s := Submission{ID: strings.Repeat("b", 32), BatchID: "batch", Recipe: recipe, PriceVersion: batchPriceVersion, Items: []Item{{RequestHash: strings.Repeat("c", 64)}}}
	id := customID(s.ID, 0)
	for _, body := range []string{
		`{"type":"succeeded","message":{"usage":{"input_tokens":1,"INPUT_TOKENS":2,"output_tokens":1}}}`,
		`{"type":"succeeded","message":{"usage":{"input_tokens":1,"input_tokens":2,"output_tokens":1}}}`,
		`{"type":"succeeded","type":"expired"}`,
		`{"type":"succeeded","message":{"usage":"invalid"}}`,
		`null`,
	} {
		wire := `{"custom_id":"` + id + `","result":` + body + `}`
		line, verifyErr := verifyCapturedLine(capturedResult{CustomID: id, Type: "succeeded", Body: body, Wire: wire})
		if verifyErr != nil {
			t.Fatalf("response defect blocked store: %s: %v", body, verifyErr)
		}
		attempt, _, parseErr := parseAttempt(s, 0, line.RawJSON)
		if parseErr != nil || attempt.Cost != nil || attempt.Outcome != "failed" || attempt.Raw != body {
			t.Fatalf("response not retained as unpriced: %s: %+v %v", body, attempt, parseErr)
		}
	}
	bad := `{"custom_id":"` + id + `","CUSTOM_ID":"wrong","result":{}}`
	if _, err = verifyCapturedLine(capturedResult{CustomID: id, Body: `{}`, Wire: bad}); err == nil {
		t.Fatal("accepted ambiguous envelope identity")
	}
}

func TestReview_CapturePreservesMalformedResponseValue(t *testing.T) {
	r, _, client, _ := screenFixture(t)
	id := strings.Repeat("a", 32)
	custom := id + "-000000"
	wire := `{"custom_id":"` + custom + `","result":null}`
	fault := &providerFault{fakeClient: client, stream: func(_ context.Context, _ string, visit func(batchapi.Result) error) error {
		return visit(batchapi.Result{CustomID: custom, WireJSON: wire})
	}}
	r.Client = fault
	if err := r.captureResults(context.Background(), id, "candidate", time.Time{}); err != nil {
		t.Fatal(err)
	}
	dirs, err := os.ReadDir(filepath.Join(r.IntentDir, "evidence", id, captureScope("candidate")))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("capture directories %d: %v", len(dirs), err)
	}
	var record capturedResult
	if err = readRetainedFile(filepath.Join(r.IntentDir, "evidence", id, captureScope("candidate"), dirs[0].Name(), captureRecordName(0)), &record); err != nil {
		t.Fatal(err)
	}
	if record.Body != "null" {
		t.Fatalf("lost raw malformed response: %q", record.Body)
	}
	if _, err = verifyCapturedLine(record); err != nil {
		t.Fatalf("response defect blocked evidence: %v", err)
	}
}

func TestReview_UnknownStatusResponseIsRetainedWithoutReceipt(t *testing.T) {
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	raw := `{"id":"batch_unverified","created_at":"2099-01-01T00:00:00Z","processing_status":"in_progress"}`
	r.Client = &statusResponseClient{fakeClient: client, raw: raw, status: 0, failure: errors.New("synthetic interrupted transport")}
	if _, err := r.Screen(context.Background(), o, documents(), families()); err == nil {
		t.Fatal("expected transport error")
	}
	if len(ledger.rows) != 1 {
		t.Fatal("missing retained intent")
	}
	dir := filepath.Join(r.IntentDir, "evidence", ledger.rows[0].ID, "responses")
	captures, err := readProviderResponses(dir, ledger.rows[0].ID)
	if err != nil || len(captures) != 1 || captures[0].Status != 0 || string(captures[0].Body) != raw {
		t.Fatalf("unknown-status body not retained: count=%d error=%v", len(captures), err)
	}
	if _, err = os.Stat(filepath.Join(r.IntentDir, "submissions", ledger.rows[0].ID+".batch")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unknown-status response became receipt")
	}
}

func TestReview_CreateReceiptRequiresBoundScopeAndCreatedAt(t *testing.T) {
	for _, scenario := range []string{"custom id", "request hash", "account", "created at"} {
		t.Run(scenario, func(t *testing.T) {
			r, ledger, _, o := screenFixture(t)
			if _, err := r.Screen(context.Background(), o, documents(), families()); err != nil {
				t.Fatal(err)
			}
			s := ledger.rows[0]
			dir := filepath.Join(r.IntentDir, "evidence", s.ID, "responses")
			files, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			changed := false
			var modified providerResponse
			for _, file := range files {
				path := filepath.Join(dir, file.Name())
				var response providerResponse
				if err = readRetainedFile(path, &response); err != nil {
					t.Fatal(err)
				}
				if response.Operation != "create" {
					continue
				}
				switch scenario {
				case "custom id":
					response.Requests[0].CustomID = strings.Repeat("f", 32) + "-000000"
				case "request hash":
					response.Requests[0].RequestHash = strings.Repeat("f", 64)
				case "account":
					response.AccountFingerprint = strings.Repeat("f", 32)
				case "created at":
					var body map[string]any
					if err = json.Unmarshal(response.Body, &body); err != nil {
						t.Fatal(err)
					}
					delete(body, "created_at")
					response.Body, err = json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
				}
				b, marshalErr := json.Marshal(response)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				if err = os.WriteFile(path, b, 0600); err != nil {
					t.Fatal(err)
				}
				modified = response
				changed = true
				break
			}
			if !changed {
				t.Fatal("missing create response capture")
			}
			_, err = r.verifyStore(ledger.rows, nil)
			if scenario == "created at" {
				if err != nil {
					t.Fatalf("nonidentity timestamp blocked saved receipt: %v", err)
				}
				if _, found, _, _, verifyErr := verifyReceipt(s, nil, []providerResponse{modified}); verifyErr != nil || found {
					t.Fatalf("missing creation time established receipt: found=%t error=%v", found, verifyErr)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted unbound create receipt")
			}
		})
	}
}

func TestReview_NullRootNeverPassesThroughPointerTargets(t *testing.T) {
	var number *int64
	var list []int
	for _, value := range []any{&number, &list} {
		if err := validateRetainedJSON([]byte(`null`), value); err == nil {
			t.Fatalf("accepted null root for %T", value)
		}
	}
}

func TestReview_LiveReceiptWriteUsesSameVerification(t *testing.T) {
	r, _, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	submitted := s.CreatedAt.Add(-2 * time.Minute)
	deadline := submitted.Add(26 * time.Hour)
	s.BatchID = "batch_invalid"
	s.SubmittedAt = &submitted
	s.DeadlineAt = &deadline
	if err := r.writeBatch(s); err == nil {
		t.Fatal("wrote receipt rejected by shared verifier")
	}
}

func TestReview_AllRequiredRecordFieldsRejectMissingNullAndWrongType(t *testing.T) {
	values := []any{
		&Submission{Items: []Item{}}, &Recipe{}, &ArticleRecipe{}, &Item{}, &Family{},
		&ArticleRequest{Articles: []Article{}, Nodes: []Node{}}, &Article{}, &Node{}, &DocumentSelection{DocumentKeys: []string{}},
		&FamilyArtifact{Documents: map[string]Outcome{}}, &ArticleArtifact{Articles: []ArticleRecord{}}, &ArticleMetadata{}, &ArticleRecord{}, &Outcome{}, &ArticleOutcome{}, &CodeConfidence{},
		&captureStart{BatchID: "candidate"}, &captureEnd{}, &capturedResult{}, &creatorRecord{}, &batchRecord{}, &abandonmentRecord{}, &settlementRecord{}, &attemptRecord{}, &tokenUsage{}, &countedRequest{}, &providerResponse{Body: []byte{}}, &providerRequestEvidence{},
	}
	for _, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = validateRetainedJSON(raw, value); err != nil {
			t.Fatalf("invalid %T baseline: %v", value, err)
		}
		for _, key := range retainedRequired(reflect.TypeOf(value).Elem()) {
			for _, mode := range []string{"missing", "null", "wrong type"} {
				var fields map[string]json.RawMessage
				if err = json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "missing":
					delete(fields, key)
				case "null":
					fields[key] = json.RawMessage("null")
				case "wrong type":
					fields[key] = json.RawMessage(`{"unexpected":true}`)
				}
				mutated, marshalErr := json.Marshal(fields)
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				if err = validateRetainedJSON(mutated, value); err == nil {
					t.Fatalf("%T accepted %s %s", value, mode, key)
				}
			}
		}
	}
	var start captureStart
	if err := validateRetainedJSON([]byte(`{"intent_id":"x","capture_id":"y","after":null}`), &start); err == nil {
		t.Fatal("accepted null optional pointer")
	}
}

func TestReview_ResponseDefectCannotHideOuterRecordTypeMismatch(t *testing.T) {
	id := strings.Repeat("a", 32) + "-000000"
	body := `{"type":"succeeded","message":{"usage":{"input_tokens":1,"INPUT_TOKENS":2,"output_tokens":1}}}`
	wire := `{"custom_id":"` + id + `","result":` + body + `}`
	if _, err := verifyCapturedLine(capturedResult{CustomID: id, Type: "expired", Body: body, Wire: wire}); err == nil {
		t.Fatal("response defect hid inconsistent captured type")
	}
}

func TestReview_StoredGetIdentityMismatchStopsStore(t *testing.T) {
	r, ledger, client, o := screenFixture(t)
	raw := `{"id":"other_batch","created_at":"2099-01-01T00:00:00Z","processing_status":"ended"}`
	r.Client = &rawGetFixture{fakeClient: client, raw: raw}
	if _, err := r.Screen(context.Background(), o, documents(), families()); err == nil {
		t.Fatal("expected mismatched get response")
	}
	if _, err := r.verifyStore(ledger.rows, nil); err == nil {
		t.Fatal("stored batch identity mismatch did not stop store")
	}
}

func TestReview_UnownedReceiptDoesNotHideInvalidResultEnvelope(t *testing.T) {
	r, ledger, _, o := screenFixture(t)
	s := seedUndispatchedScreen(t, r, o, documents())
	c, err := r.startCapture(captureStart{Owner: s.ID, BatchID: "batch_unclaimed", AccountFingerprint: s.AccountFingerprint})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"type":"expired"}`
	wire := `{"custom_id":"other-000000","result":` + body + `}`
	record := capturedResult{CustomID: customID(s.ID, 0), Type: "expired", Body: body, Wire: wire}
	if err = r.writeCapture(filepath.Join(c.dir, captureRecordName(0)), record); err != nil {
		t.Fatal(err)
	}
	if err = r.endCapture(c, 1, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = r.verifyStore(ledger.rows, nil); err == nil {
		t.Fatal("invalid result envelope escaped global consistency check")
	}
}

func TestReview_ConditionalRecordFieldsAreRequiredBySharedReader(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		value any
	}{
		{`{"intent_id":"x","capture_id":"y","operation":"create","account_fingerprint":"a","http_status":200,"body":""}`, &providerResponse{}},
		{`{"intent_id":"x","capture_id":"y","operation":"get","account_fingerprint":"a","http_status":200,"body":""}`, &providerResponse{}},
		{`{"intent_id":"x","capture_id":"y"}`, &captureStart{}},
		{`{"id":"x","provider_job_id":"b","state":"completed","cost_micro_usd":0,"price_version":"p","evidence_kind":"provider_results"}`, &settlementRecord{}},
	} {
		if err := validateRetainedJSON([]byte(tc.raw), tc.value); err == nil {
			t.Fatalf("accepted missing conditional fields for %T", tc.value)
		}
	}
}
