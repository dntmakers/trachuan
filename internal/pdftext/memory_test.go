//go:build darwin

package pdftext

import (
	"context"
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"
)

type testWatchdogClock struct {
	mu            sync.Mutex
	now           time.Time
	ticks         chan time.Time
	stale         chan time.Time
	resets        chan time.Duration
	expireOnTimer bool
}

func newTestWatchdogClock() *testWatchdogClock {
	return &testWatchdogClock{now: time.Now(), ticks: make(chan time.Time, 1), stale: make(chan time.Time, 1), resets: make(chan time.Duration, 1)}
}
func (c *testWatchdogClock) Now() time.Time                   { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testWatchdogClock) Tick() (<-chan time.Time, func()) { return c.ticks, func() {} }
func (c *testWatchdogClock) Timer(time.Duration) (<-chan time.Time, func(time.Duration), func()) {
	if c.expireOnTimer {
		c.stale <- c.advance(time.Second)
	}
	return c.stale, func(d time.Duration) { c.resets <- d }, func() {}
}
func (c *testWatchdogClock) advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

func TestWatchdog_StalledSample(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start(context.Background(), []string{"-c", `while :; do :; done`}, []byte("input"), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	clock := newTestWatchdogClock()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	p.sample = func(ctx context.Context, _ int) (uint64, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return 0, ctx.Err()
	}
	done := make(chan error, 1)
	go func() { _, _, err := p.waitWithClock(context.Background(), clock); done <- err }()
	<-started
	clock.stale <- clock.advance(time.Second)
	<-cancelled
	select {
	case err := <-done:
		t.Fatalf("returned before sampler reaped: %v", err)
	default:
	}
	close(release)
	if err := <-done; !errors.Is(err, ErrConfiguration) {
		t.Fatalf("stalled sample: %v", err)
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group remains: %v", err)
	}
}

func TestWatchdog_ExitAtDeadline(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start(context.Background(), []string{"-c", "exit 0"}, nil, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	result := <-p.done
	ready := make(chan processResult, 1)
	ready <- result
	p.done = ready
	clock := newTestWatchdogClock()
	clock.expireOnTimer = true
	_, _, err = p.waitWithClock(context.Background(), clock)
	if !errors.Is(err, ErrConfiguration) {
		t.Fatalf("exit at deadline: %v", err)
	}
}

func TestWatchdog_SlowSampleContinues(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start(context.Background(), []string{"-c", `while :; do :; done`}, []byte("input"), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	clock := newTestWatchdogClock()
	started := make(chan struct{})
	release := make(chan struct{})
	p.sample = func(context.Context, int) (uint64, error) {
		close(started)
		<-release
		return 0, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, _, err := p.waitWithClock(ctx, clock); done <- err }()
	<-started
	clock.advance(900 * time.Millisecond)
	close(release)
	<-clock.resets
	cancel()
	if err := <-done; !errors.Is(err, ErrFile) {
		t.Fatalf("slow sample: %v", err)
	}
}

type cancelOnResetClock struct {
	watchdogClock
	cancel context.CancelFunc
}

func (c cancelOnResetClock) Timer(d time.Duration) (<-chan time.Time, func(time.Duration), func()) {
	ch, reset, stop := c.watchdogClock.Timer(d)
	return ch, func(d time.Duration) { reset(d); c.cancel() }, stop
}

func TestWatchdog_CancelBeforeInput(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start(context.Background(), []string{"-c", `read x; printf '%s' "$x"`}, []byte("sentinel\n"), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	p.sample = func(context.Context, int) (uint64, error) { return 0, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, _, err = p.waitWithClock(ctx, cancelOnResetClock{watchdogClock: systemWatchdogClock{}, cancel: cancel})
	if !errors.Is(err, ErrFile) || p.writerDone != nil || p.stdout.buf.Len() != 0 {
		t.Fatalf("cancelled input: err=%v writer=%v output=%q", err, p.writerDone != nil, p.stdout.buf.String())
	}
}
