package publicsample

import (
	"context"
	"errors"
	"testing"
	"time"

	"danny.vn/trachuan/internal/retrieval"
)

type stalledCleanupEngine struct {
	deadline time.Time
	started  chan time.Time
}

func (e stalledCleanupEngine) Search(ctx context.Context, _ retrieval.Request) (retrieval.Response, error) {
	workDeadline, _ := ctx.Deadline()
	e.started <- workDeadline
	<-ctx.Done()
	cleanup, cancel := context.WithDeadline(context.WithoutCancel(ctx), e.deadline)
	defer cancel()
	<-cleanup.Done()
	return retrieval.Response{}, errors.New("rollback timed out")
}
func (stalledCleanupEngine) Present(context.Context, retrieval.Response, retrieval.Detail) (retrieval.SearchResult, error) {
	return retrieval.SearchResult{}, errors.New("unexpected present")
}

type deadlineEngine struct{ deadline chan time.Time }

func (e deadlineEngine) Search(ctx context.Context, _ retrieval.Request) (retrieval.Response, error) {
	deadline, _ := ctx.Deadline()
	e.deadline <- deadline
	return retrieval.Response{}, errors.New("stop")
}

func (deadlineEngine) Present(context.Context, retrieval.Response, retrieval.Detail) (retrieval.SearchResult, error) {
	return retrieval.SearchResult{}, errors.New("unexpected present")
}

func TestSampleSearch_DefaultCleanupReserve(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	observed := make(chan time.Time, 1)
	_, _ = searchSample(ctx, deadlineEngine{deadline: observed}, "20990101", time.Now())
	if got := <-observed; !got.Equal(deadline.Add(-time.Second)) {
		t.Fatalf("work deadline %v, want %v", got, deadline.Add(-time.Second))
	}
}

func TestSampleFlight_StuckRollbackEndsByDeadline(t *testing.T) {
	activation := Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	started := make(chan time.Time, 1)
	f := newFlights(context.Background(), func(context.Context) (Activation, error) { return activation, nil }, func(Activation) bool { return true }, func(ctx context.Context, _ Activation, now time.Time) ([]byte, error) {
		deadline, _ := ctx.Deadline()
		return searchSampleWithReserve(ctx, stalledCleanupEngine{deadline: deadline, started: started}, "20990101", now, 30*time.Millisecond)
	}, time.Now)
	f.timeout = 70 * time.Millisecond
	defer f.Close()
	go func() { _, _, _ = f.Get(context.Background()) }()
	<-started
	f.mu.Lock()
	job := f.job
	f.mu.Unlock()
	select {
	case <-job.done:
	case <-time.After(95 * time.Millisecond):
		t.Fatal("job retained its slot beyond the deadline")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.job != nil {
		t.Fatal("job slot was not released")
	}
}

func TestSampleSearch_ReservesCleanupInsideDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
	defer cancel()
	deadline, _ := ctx.Deadline()
	engine := stalledCleanupEngine{deadline: deadline, started: make(chan time.Time, 1)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = searchSampleWithReserve(ctx, engine, "20990101", time.Now(), 30*time.Millisecond)
	}()
	workDeadline := <-engine.started
	if !workDeadline.Equal(deadline.Add(-30 * time.Millisecond)) {
		t.Fatalf("work deadline %v, want %v", workDeadline, deadline.Add(-30*time.Millisecond))
	}
	select {
	case <-done:
	case <-time.After(95 * time.Millisecond):
		t.Fatal("sample cleanup exceeded its deadline")
	}
	if time.Since(deadline) > 25*time.Millisecond {
		t.Fatal("sample job exceeded deadline")
	}
}
