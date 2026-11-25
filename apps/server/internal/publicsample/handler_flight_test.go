package publicsample

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/publicsummary"
	"danny.vn/trachuan/internal/retrieval"
)

func TestHandler_FlightProjection(t *testing.T) {
	var calls atomic.Int64
	raw, presented := sample()
	f := newFlights(context.Background(), func(context.Context) (Activation, error) {
		return Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}, nil
	}, func(Activation) bool { return true }, func(ctx context.Context, _ Activation, date time.Time) ([]byte, error) {
		calls.Add(1)
		return searchSample(ctx, sampleEngine{t: t, instant: date, raw: raw, presented: presented}, "20990101", date)
	}, func() time.Time { return time.Date(2099, 1, 1, 20, 0, 0, 0, time.UTC) })
	defer f.Close()
	h := NewHandler(true, publicsummary.NewAdmission())
	h.flights = f
	for range 2 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/public/sample", nil))
		if w.Code != 200 || w.Header().Get("X-Sample-Status-Date") != "2099-01-02" || strings.Contains(w.Body.String(), "SECRET") {
			t.Fatalf("status %d headers %v body %s", w.Code, w.Header(), w.Body.String())
		}
		validateSampleSchema(t, w.Body.Bytes())
	}
	if calls.Load() != 1 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestHandler_FlightRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*retrieval.Response, *retrieval.SearchResult)
		fail   bool
	}{
		{"abstain", func(r *retrieval.Response, _ *retrieval.SearchResult) { r.Abstain = true }, false},
		{"wrong document", func(r *retrieval.Response, _ *retrieval.SearchResult) { r.Hits[0].Identity.DocumentKey = "wrong" }, false},
		{"unproven cut", func(r *retrieval.Response, _ *retrieval.SearchResult) { r.Hits[0].Fragments = nil }, false},
		{"search failure", func(*retrieval.Response, *retrieval.SearchResult) {}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			raw, presented := sample()
			tc.change(&raw, &presented)
			f := newFlights(context.Background(), func(context.Context) (Activation, error) {
				return Activation{Database: "x", ActivatedAt: time.Unix(1, 0)}, nil
			}, func(Activation) bool { return true }, func(context.Context, Activation, time.Time) ([]byte, error) {
				calls.Add(1)
				if tc.fail {
					return nil, errors.New("failure")
				}
				out, reason := Project(raw, presented, "20990101")
				if reason != "" {
					return nil, errors.New(string(reason))
				}
				return json.Marshal(out)
			}, time.Now)
			defer f.Close()
			h := NewHandler(true, publicsummary.NewAdmission())
			h.flights = f
			for range 2 {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/public/sample", nil))
				if w.Code != 503 || !strings.Contains(w.Body.String(), "unavailable") {
					t.Fatalf("status %d body %s", w.Code, w.Body.String())
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("calls %d", calls.Load())
			}
		})
	}
}
