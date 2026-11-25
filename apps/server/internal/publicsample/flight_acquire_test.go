package publicsample

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlights_ReservedLeaseAndColdJoiners(t *testing.T) {
	a := Activation{Database: "db", ActivatedAt: time.Unix(1, 0)}
	started, release := make(chan struct{}), make(chan struct{})
	joined := make(chan struct{})
	var resolves, searches, observations atomic.Int32
	var available atomic.Bool
	f := NewFlights(context.Background(), func(context.Context) (Activation, error) {
		if observations.Add(1) == 3 {
			close(joined)
		}
		return a, nil
	}, nil, nil, time.Now).WithAcquire(func(Activation) (func(context.Context, time.Time) ([]byte, error), bool) {
		resolves.Add(1)
		if !available.Load() {
			return nil, false
		}
		return func(context.Context, time.Time) ([]byte, error) {
			searches.Add(1)
			close(started)
			<-release
			return []byte("ok"), nil
		}, true
	})
	defer f.Close()
	if _, _, err := f.Get(context.Background()); !errors.Is(err, errBusy) {
		t.Fatalf("not ready: %v", err)
	}
	available.Store(true)
	results := make(chan error, 2)
	go func() { _, _, err := f.Get(context.Background()); results <- err }()
	<-started
	go func() { _, _, err := f.Get(context.Background()); results <- err }()
	<-joined
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if searches.Load() != 1 || resolves.Load() != 2 {
		t.Fatalf("searches %d resolves %d", searches.Load(), resolves.Load())
	}
}
