package classify

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEvidence_RejectsCaseVariantProtocolFields(t *testing.T) {
	cases := []string{
		`{"data":[],"HAS_MORE":false}`,
		`{"data":[],"has_more":false,"HAS_MORE":true}`,
		`{"data":[{"id":"b","created_at":"2099-01-01T00:00:00Z","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"SUCCEEDED":0,"errored":0,"canceled":0,"expired":0}}],"has_more":false,"first_id":"b","last_id":"b"}`,
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			var pager listingPager
			if _, err := pager.accept(raw); err == nil {
				t.Fatal("accepted case-variant protocol field")
			}
		})
	}
	body := `{"type":"expired","TYPE":"canceled"}`
	wire := fmt.Sprintf(`{"custom_id":"other","result":%s}`, body)
	line, err := verifyCapturedLine(capturedResult{CustomID: "other", Type: "expired", Body: body, Wire: wire})
	if err != nil || line.Type != "" {
		t.Fatalf("ambiguous result must remain unpriced: %v", err)
	}
	body = `{"type":"expired"}`
	wire = fmt.Sprintf(`{"custom_id":"other","CUSTOM_ID":"wrong","result":%s}`, body)
	if _, err := verifyCapturedLine(capturedResult{CustomID: "other", Type: "expired", Body: body, Wire: wire}); err == nil {
		t.Fatal("accepted custom ID case variant")
	}
	r, _ := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
	_, cost := priceUsage(Submission{Recipe: r, PriceVersion: batchPriceVersion}, `{"input_tokens":10,"output_tokens":5,"CACHE_READ_INPUT_TOKENS":50}`)
	if cost != nil {
		t.Fatal("priced case-variant usage")
	}
}

func TestEvidence_ListingMustStartAfterCreatorQuietWindow(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	client.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain create")
	}
	s := ledger.rows[0]
	now := s.CreatedAt.Add(24*time.Hour + time.Minute)
	r.Now = func() time.Time { return now }
	hook := &hookedReconcileClient{fakeClient: client, listHook: func() error { now = s.CreatedAt.Add(25 * time.Hour); return nil }}
	r.Client = hook
	if err := r.Reconcile(ctx, s.ID, true); err == nil {
		t.Fatal("abandoned from listing begun before quiet period ended")
	}
	if ledger.rows[0].State == "abandoned" {
		t.Fatal("released early listing")
	}
}

func TestEvidence_ListingCannotStartWhileCreatorLockHeld(t *testing.T) {
	ctx := context.Background()
	r, ledger, client, o := screenFixture(t)
	client.beforeCreate = nil
	client.createErr = context.DeadlineExceeded
	if _, err := r.Screen(ctx, o, documents(), families()); err == nil {
		t.Fatal("expected uncertain create")
	}
	s := ledger.rows[0]
	r.Now = func() time.Time { return s.CreatedAt.Add(25 * time.Hour) }
	guard, err := r.lockCreator(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guard.close() }()
	calls := 0
	r.Client = &hookedReconcileClient{fakeClient: client, listHook: func() error { calls++; return nil }}
	if err = r.Reconcile(ctx, s.ID, true); err == nil {
		t.Fatal("abandoned during live creator")
	}
	if calls != 0 {
		t.Fatal("listing started while creator lock was held")
	}
}

func TestEvidence_EveryProtocolFieldRejectsCaseVariants(t *testing.T) {
	base := `{"data":[{"id":"batch","created_at":"2099-01-01T00:00:00Z","processing_status":"ended","request_counts":{"processing":0,"succeeded":1,"errored":0,"canceled":0,"expired":0}}],"has_more":false,"first_id":"batch","last_id":"batch"}`
	for _, key := range []string{"data", "has_more", "first_id", "last_id", "id", "created_at", "processing_status", "request_counts", "processing", "succeeded", "errored", "canceled", "expired"} {
		t.Run(key, func(t *testing.T) {
			variant := strings.Replace(base, `"`+key+`":`, `"`+strings.ToUpper(key)+`":`, 1)
			var pager listingPager
			if _, err := pager.accept(variant); err == nil {
				t.Fatalf("accepted case variant of %s", key)
			}
		})
	}
	for _, key := range []string{"input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens", "cache_creation"} {
		t.Run("usage/"+key, func(t *testing.T) {
			raw := `{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0}}`
			raw = strings.Replace(raw, `"`+key+`":`, `"`+strings.ToUpper(key)+`":`, 1)
			recipe, _ := NewRecipe("claude-haiku-5-5", strings.Repeat("a", 64))
			if _, cost := priceUsage(Submission{Recipe: recipe, PriceVersion: batchPriceVersion}, raw); cost != nil {
				t.Fatalf("priced case variant of %s", key)
			}
		})
	}
	for _, raw := range []string{
		`{"TYPE":"expired"}`,
		`{"type":"succeeded","MESSAGE":{"usage":{"input_tokens":1,"output_tokens":1}}}`,
	} {
		if _, err := providerEvidenceBody([]byte(raw)); err == nil {
			t.Fatalf("accepted provider evidence %s", raw)
		}
	}
	if err := verifyMessageEvidence([]byte(`{"content":[{"TYPE":"text","text":"x"}]}`)); err == nil {
		t.Fatal("accepted case-variant content evidence")
	}
}

func TestEvidence_RetainedMetadataRejectsCaseVariants(t *testing.T) {
	cases := []struct {
		raw   string
		value any
	}{
		{`{"captured_at":"2099-01-01T00:00:00Z","CAPTURED_AT":"2099-01-02T00:00:00Z"}`, &captureStart{}},
		{`{"records":0,"RECORDS":1}`, &captureEnd{}},
		{`{"provider_job_id":"a","PROVIDER_JOB_ID":"b"}`, &batchRecord{}},
		{`{"custom_id":"a","CUSTOM_ID":"b"}`, &capturedResult{}},
		{`{"at":"2099-01-01T00:00:00Z","AT":"2099-01-02T00:00:00Z"}`, &creatorRecord{}},
		{`{"usage":{"input_tokens":1,"INPUT_TOKENS":2}}`, &attemptRecord{}},
	}
	for _, tc := range cases {
		if err := validateRetainedJSON([]byte(tc.raw), tc.value); err == nil {
			t.Fatalf("accepted metadata %s", tc.raw)
		}
	}
	body := `{"type":"expired"}`
	wire := fmt.Sprintf(`{"custom_id":"other","result":%s,"RESULT":%s}`, body, body)
	if _, err := verifyCapturedLine(capturedResult{CustomID: "other", Type: "expired", Body: body, Wire: wire}); err == nil {
		t.Fatal("accepted result key case variant")
	}
}

func TestEvidence_RetainedMetadataGuardsEveryTaggedField(t *testing.T) {
	values := []any{&captureStart{BatchID: "candidate"}, &captureEnd{}, &capturedResult{}, &providerResponse{Body: []byte{}}, &abandonmentRecord{}, &creatorRecord{}, &batchRecord{}, &settlementRecord{}, &attemptRecord{}, &Submission{Items: []Item{}}}
	var tags func(reflect.Type) []string
	tags = func(typ reflect.Type) []string {
		var found []string
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Anonymous && field.Type.Kind() == reflect.Struct {
				found = append(found, tags(field.Type)...)
				continue
			}
			if field.PkgPath != "" {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")[0]
			if tag != "" && tag != "-" {
				found = append(found, tag)
			}
		}
		return found
	}
	for _, value := range values {
		baseline, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = validateRetainedJSON(baseline, value); err != nil {
			t.Fatalf("invalid %T test baseline: %v", value, err)
		}
		for _, key := range tags(reflect.TypeOf(value).Elem()) {
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(baseline, &fields); err != nil {
				t.Fatal(err)
			}
			variant := fields[key]
			if variant == nil {
				variant = json.RawMessage("null")
			}
			fields[strings.ToUpper(key)] = variant
			raw, marshalErr := json.Marshal(fields)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if err = validateRetainedJSON(raw, value); err == nil {
				t.Fatalf("%T accepted case variant for %s", value, key)
			}
		}
	}
}
