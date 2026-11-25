package publicsample

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlights_JoinerDisconnectDoesNotCancelWork(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	f := newFlights(context.Background(), func(context.Context) (Activation, error) {
		return Activation{Database: "x", ActivatedAt: time.Unix(1, 0)}, nil
	}, func(Activation) bool { return true }, func(ctx context.Context, _ Activation, _ time.Time) ([]byte, error) {
		calls.Add(1)
		close(started)
		select {
		case <-finish:
			return []byte("result"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}, time.Now)
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, _, err := f.Get(ctx); first <- err }()
	<-started
	second := make(chan error, 1)
	go func() { _, _, err := f.Get(context.Background()); second <- err }()
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first: %v", err)
	}
	close(finish)
	if err := <-second; err != nil {
		t.Fatalf("second: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls: %d", calls.Load())
	}
}

func TestFlights_RollbackDiscardsLateCompletion(t *testing.T) {
	var mu sync.Mutex
	current := Activation{Database: "a", ActivatedAt: time.Unix(1, 0)}
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	f := newFlights(context.Background(), func(context.Context) (Activation, error) {
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	}, func(Activation) bool { return true }, func(_ context.Context, _ Activation, _ time.Time) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-finish
			return []byte("stale"), nil
		}
		return []byte("fresh"), nil
	}, time.Now)
	defer f.Close()
	first := make(chan error, 1)
	go func() { _, _, err := f.Get(context.Background()); first <- err }()
	<-started
	mu.Lock()
	current = Activation{Database: "b", ActivatedAt: time.Unix(2, 0)}
	mu.Unlock()
	if _, _, err := f.Get(context.Background()); !errors.Is(err, errBusy) {
		t.Fatalf("rollover: %v", err)
	}
	mu.Lock()
	current = Activation{Database: "a", ActivatedAt: time.Unix(3, 0)}
	mu.Unlock()
	if _, _, err := f.Get(context.Background()); !errors.Is(err, errBusy) {
		t.Fatalf("rollback: %v", err)
	}
	close(finish)
	if err := <-first; !errors.Is(err, errBusy) {
		t.Fatalf("old flight: %v", err)
	}
	body, _, err := f.Get(context.Background())
	if err != nil || string(body) != "fresh" || calls.Load() != 2 {
		t.Fatalf("body %q calls %d error %v", body, calls.Load(), err)
	}
}
