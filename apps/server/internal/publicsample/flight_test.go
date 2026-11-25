package publicsample

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlights_FailureCachedUntilActivationChanges(t *testing.T) {
	current := Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	var calls atomic.Int64
	f := newFlights(context.Background(), func(context.Context) (Activation, error) { return current, nil }, func(Activation) bool { return true }, func(context.Context, Activation, time.Time) ([]byte, error) {
		calls.Add(1)
		return nil, errors.New("failure")
	}, time.Now)
	defer f.Close()
	for range 2 {
		if _, _, err := f.Get(context.Background()); !errors.Is(err, errUnavailable) {
			t.Fatalf("error: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("attempts: %d", calls.Load())
	}
	current.ActivatedAt = current.ActivatedAt.Add(time.Second)
	if _, _, err := f.Get(context.Background()); !errors.Is(err, errUnavailable) {
		t.Fatalf("error: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("attempts after activation: %d", calls.Load())
	}
}

func TestFlights_NotReadyDoesNotConsumeAttempt(t *testing.T) {
	ready := false
	var calls atomic.Int64
	f := newFlights(context.Background(), func(context.Context) (Activation, error) {
		return Activation{Database: "x", ActivatedAt: time.Unix(1, 0)}, nil
	}, func(Activation) bool { return ready }, func(context.Context, Activation, time.Time) ([]byte, error) {
		calls.Add(1)
		return []byte("result"), nil
	}, func() time.Time { return time.Date(2099, 1, 1, 20, 0, 0, 0, time.UTC) })
	defer f.Close()
	if _, _, err := f.Get(context.Background()); !errors.Is(err, errBusy) {
		t.Fatalf("error: %v", err)
	}
	ready = true
	body, date, err := f.Get(context.Background())
	if err != nil || string(body) != "result" || date != "2099-01-02" || calls.Load() != 1 {
		t.Fatalf("body %q date %s calls %d error %v", body, date, calls.Load(), err)
	}
}
