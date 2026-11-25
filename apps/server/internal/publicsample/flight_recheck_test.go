package publicsample

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFlights_FailedCompletionRecheckDoesNotRetry(t *testing.T) {
	a := Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	reads, searches := 0, 0
	f := newFlights(context.Background(), func(context.Context) (Activation, error) {
		reads++
		if reads == 2 {
			return Activation{}, errors.New("metadata unavailable")
		}
		return a, nil
	}, func(Activation) bool { return true }, func(context.Context, Activation, time.Time) ([]byte, error) {
		searches++
		return []byte("result"), nil
	}, time.Now)
	defer f.Close()
	for range 2 {
		if _, _, err := f.Get(context.Background()); !errors.Is(err, errUnavailable) {
			t.Fatalf("error %v", err)
		}
	}
	if searches != 1 {
		t.Fatalf("searches %d", searches)
	}
}

func TestFlights_RollbackObservedWithoutCaller(t *testing.T) {
	var mu sync.Mutex
	current := Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	started, finish := make(chan struct{}), make(chan struct{})
	reasons := make(chan Reason, 1)
	f := newFlights(context.Background(), func(context.Context) (Activation, error) { mu.Lock(); defer mu.Unlock(); return current, nil }, func(Activation) bool { return true }, func(context.Context, Activation, time.Time) ([]byte, error) {
		close(started)
		<-finish
		return []byte("old"), nil
	}, time.Now).WithObserver(func(r Reason) { reasons <- r })
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() { defer close(returned); _, _, _ = f.Get(ctx) }()
	<-started
	f.mu.Lock()
	job := f.job
	f.mu.Unlock()
	cancel()
	<-returned
	mu.Lock()
	current.ActivatedAt = current.ActivatedAt.Add(time.Second)
	mu.Unlock()
	close(finish)
	<-job.done
	if reason := <-reasons; reason == ReasonSuccess {
		t.Fatal("obsolete result logged as success")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cached {
		t.Fatal("obsolete activation cached")
	}
}
