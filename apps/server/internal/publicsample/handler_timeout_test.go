package publicsample

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/publicsummary"
)

func TestHandler_SearchTimeout(t *testing.T) {
	activation := Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}
	deadlines := make(chan time.Duration, 1)
	attempts := 0
	f := newFlights(context.Background(), func(context.Context) (Activation, error) {
		return activation, nil
	}, func(Activation) bool { return true }, func(ctx context.Context, _ Activation, _ time.Time) ([]byte, error) {
		attempts++
		deadline, ok := ctx.Deadline()
		if !ok {
			deadlines <- 0
		} else {
			deadlines <- time.Until(deadline)
		}
		return nil, context.DeadlineExceeded
	}, time.Now)
	defer f.Close()
	h := NewHandler(true, publicsummary.NewAdmission())
	h.flights = f
	for range 2 {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/sample", nil))
		if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "unavailable") {
			t.Fatalf("status %d body %s", w.Code, w.Body.String())
		}
	}
	if d := <-deadlines; d <= 4*time.Second || d > 5*time.Second {
		t.Fatalf("search deadline remaining %v, want within 5 seconds", d)
	}
	if attempts != 1 {
		t.Fatalf("search attempts %d, want 1", attempts)
	}
}
