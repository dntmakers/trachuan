package publicsample

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlights_DeadlineWithBoundedCleanup(t *testing.T) {
	a := Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	started := make(chan struct{})
	cleanupStarted := make(chan struct{})
	f := newFlights(context.Background(), func(context.Context) (Activation, error) { return a, nil }, func(Activation) bool { return true }, func(ctx context.Context, _ Activation, _ time.Time) ([]byte, error) {
		close(started)
		<-ctx.Done()
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Millisecond)
		defer cancel()
		close(cleanupStarted)
		<-cleanup.Done()
		return nil, ctx.Err()
	}, time.Now)
	f.timeout = 20 * time.Millisecond
	defer f.Close()
	result := make(chan error, 1)
	go func() { _, _, err := f.Get(context.Background()); result <- err }()
	<-started
	f.mu.Lock()
	job := f.job
	f.mu.Unlock()
	if err := <-result; !errors.Is(err, errUnavailable) {
		t.Fatalf("error %v", err)
	}
	<-cleanupStarted
	select {
	case <-job.done:
	case <-time.After(70 * time.Millisecond):
		t.Fatal("bounded cleanup did not complete")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.job != nil {
		t.Fatal("job slot still held")
	}
}

func TestFlights_FinalRecheckUsesJobDeadline(t *testing.T) {
	a := Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	started, finish, bailout := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	f := newFlights(context.Background(), func(ctx context.Context) (Activation, error) {
		if reads.Add(1) == 2 {
			select {
			case <-ctx.Done():
				return Activation{}, ctx.Err()
			case <-bailout:
				return a, nil
			}
		}
		return a, nil
	}, func(Activation) bool { return true }, func(context.Context, Activation, time.Time) ([]byte, error) {
		close(started)
		<-finish
		return []byte("result"), nil
	}, time.Now)
	f.timeout = 20 * time.Millisecond
	defer f.Close()
	go func() { _, _, _ = f.Get(context.Background()) }()
	<-started
	f.mu.Lock()
	job := f.job
	f.mu.Unlock()
	close(finish)
	select {
	case <-job.done:
	case <-time.After(80 * time.Millisecond):
		close(bailout)
		<-job.done
		t.Fatal("completion metadata exceeded job deadline")
	}
}

func TestFlights_DeadlineReleasesCallersButNotJob(t *testing.T) {
	a := Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	start, stop := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	f := newFlights(context.Background(), func(context.Context) (Activation, error) { return a, nil }, func(Activation) bool { return true }, func(context.Context, Activation, time.Time) ([]byte, error) {
		calls.Add(1)
		close(start)
		<-stop
		return nil, errors.New("done")
	}, time.Now)
	f.timeout = 20 * time.Millisecond
	defer f.Close()
	results := make(chan error, 2)
	go func() { _, _, err := f.Get(context.Background()); results <- err }()
	<-start
	go func() { _, _, err := f.Get(context.Background()); results <- err }()
	for range 2 {
		if err := <-results; !errors.Is(err, errUnavailable) {
			t.Fatalf("error %v", err)
		}
	}
	f.mu.Lock()
	job := f.job
	f.mu.Unlock()
	if job == nil || calls.Load() != 1 {
		t.Fatalf("job %v calls %d", job, calls.Load())
	}
	close(stop)
	<-job.done
}
