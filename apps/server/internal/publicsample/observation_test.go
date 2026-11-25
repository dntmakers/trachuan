package publicsample

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFlights_AttemptObserver(t *testing.T) {
	for _, tc := range []struct {
		name      string
		searchErr error
		want      Reason
	}{
		{"success", nil, ReasonSuccess},
		{"failure", errors.New("private details"), ReasonSearchError},
		{"timeout", context.DeadlineExceeded, ReasonTimeout},
		{"projection", ReasonNoProvenCut, ReasonNoProvenCut},
		{"unknown reason", Reason("private details"), ReasonSearchError},
		{"canceled", context.Canceled, ReasonCanceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan Reason, 2)
			f := NewFlights(context.Background(), func(context.Context) (Activation, error) {
				return Activation{Database: "x", ActivatedAt: time.Unix(1, 0)}, nil
			}, func(Activation) bool { return true }, func(context.Context, Activation, time.Time) ([]byte, error) {
				if tc.searchErr != nil {
					return nil, tc.searchErr
				}
				return []byte("ok"), nil
			}, time.Now).WithObserver(func(reason Reason) { seen <- reason })
			defer f.Close()
			_, _, _ = f.Get(context.Background())
			_, _, _ = f.Get(context.Background())
			if got := <-seen; got != tc.want {
				t.Fatalf("reason %q want %q", got, tc.want)
			}
			select {
			case got := <-seen:
				t.Fatalf("duplicate observation %q", got)
			default:
			}
		})
	}
}
