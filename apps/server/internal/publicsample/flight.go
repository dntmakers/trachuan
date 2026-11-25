package publicsample

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"
)

var (
	errBusy        = errors.New("sample busy")
	errUnavailable = errors.New("sample unavailable")
)

type Activation struct {
	Database    string
	ActivatedAt time.Time
}

func (a Activation) equal(b Activation) bool {
	return a.Database == b.Database && a.ActivatedAt.Equal(b.ActivatedAt)
}

type sampleFlight struct {
	activation Activation
	done       chan struct{}
	expired    <-chan struct{}
	cancel     context.CancelFunc
}

type flights struct {
	ctx         context.Context
	cancel      context.CancelFunc
	metadata    func(context.Context) (Activation, error)
	ready       func(Activation) bool
	search      func(context.Context, Activation, time.Time) ([]byte, error)
	acquire     func(Activation) (func(context.Context, time.Time) ([]byte, error), bool)
	clock       func() time.Time
	timeout     time.Duration
	observer    func(Reason)
	observation chan struct{}
	mu          sync.Mutex
	active      Activation
	observed    bool
	attempted   bool
	cached      bool
	body        []byte
	date        string
	job         *sampleFlight
}

type Flights = flights

func NewFlights(ctx context.Context, metadata func(context.Context) (Activation, error), ready func(Activation) bool, search func(context.Context, Activation, time.Time) ([]byte, error), clock func() time.Time) *Flights {
	return newFlights(ctx, metadata, ready, search, clock)
}

func newFlights(ctx context.Context, metadata func(context.Context) (Activation, error), ready func(Activation) bool, search func(context.Context, Activation, time.Time) ([]byte, error), clock func() time.Time) *flights {
	work, cancel := context.WithCancel(ctx)
	return &flights{ctx: work, cancel: cancel, metadata: metadata, ready: ready, search: search, clock: clock, timeout: 5 * time.Second, observation: make(chan struct{}, 1)}
}

func (f *flights) WithAcquire(acquire func(Activation) (func(context.Context, time.Time) ([]byte, error), bool)) *Flights {
	f.acquire = acquire
	return f
}

func (f *flights) WithObserver(observer func(Reason)) *Flights {
	f.observer = observer
	return f
}

func (f *flights) Close() { f.cancel() }

func (f *flights) observe(ctx context.Context) (Activation, error) {
	if err := ctx.Err(); err != nil {
		return Activation{}, err
	}
	select {
	case f.observation <- struct{}{}:
	case <-ctx.Done():
		return Activation{}, ctx.Err()
	case <-f.ctx.Done():
		return Activation{}, errUnavailable
	}
	defer func() { <-f.observation }()
	a, err := f.metadata(ctx)
	if err != nil || ctx.Err() != nil || a.Database == "" || a.ActivatedAt.IsZero() {
		return Activation{}, errUnavailable
	}
	f.mu.Lock()
	if !f.observed || !f.active.equal(a) {
		f.active, f.observed = a, true
		f.cached, f.body, f.date, f.attempted = false, nil, "", false
		if f.job != nil {
			f.job.cancel()
		}
	}
	f.mu.Unlock()
	return a, nil
}

func (f *flights) Get(ctx context.Context) ([]byte, string, error) {
	a, err := f.observe(ctx)
	if err != nil {
		return nil, "", err
	}
	f.mu.Lock()
	if !f.active.equal(a) {
		f.mu.Unlock()
		return nil, "", errBusy
	}
	if f.cached {
		body, date := bytes.Clone(f.body), f.date
		f.mu.Unlock()
		if body == nil {
			return nil, "", errUnavailable
		}
		return body, date, nil
	}
	job := f.job
	if job == nil {
		if f.attempted {
			f.mu.Unlock()
			return nil, "", errUnavailable
		}
		var search func(context.Context, time.Time) ([]byte, error)
		if f.acquire != nil {
			var ok bool
			search, ok = f.acquire(a)
			if !ok || search == nil {
				f.mu.Unlock()
				return nil, "", errBusy
			}
		} else {
			if !f.ready(a) {
				f.mu.Unlock()
				return nil, "", errBusy
			}
			search = func(ctx context.Context, now time.Time) ([]byte, error) { return f.search(ctx, a, now) }
		}
		deadlineCtx, deadlineCancel := context.WithTimeout(f.ctx, f.timeout)
		work, cancel := context.WithCancel(deadlineCtx)
		job = &sampleFlight{activation: a, done: make(chan struct{}), expired: deadlineCtx.Done(), cancel: cancel}
		f.job = job
		f.attempted = true
		now := f.clock().In(time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60))
		go func() { defer deadlineCancel(); f.run(work, job, now, search) }()
	} else if !job.activation.equal(a) {
		f.mu.Unlock()
		return nil, "", errBusy
	}
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-f.ctx.Done():
		return nil, "", errUnavailable
	case <-job.expired:
		select {
		case <-job.done:
		default:
			return nil, "", errUnavailable
		}
	case <-job.done:
	}
	current, err := f.observe(ctx)
	if err != nil {
		return nil, "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !current.equal(a) || !f.active.equal(a) {
		return nil, "", errBusy
	}
	if !f.cached || f.body == nil {
		return nil, "", errUnavailable
	}
	return bytes.Clone(f.body), f.date, nil
}

func (f *flights) run(ctx context.Context, job *sampleFlight, date time.Time, search func(context.Context, time.Time) ([]byte, error)) {
	defer job.cancel()
	body, err := search(ctx, date)
	current, checkErr := f.observe(ctx)
	if checkErr != nil || !current.equal(job.activation) {
		err = errUnavailable
	}
	if ctx.Err() != nil || (len(body) == 0 && err == nil) {
		err = errUnavailable
	}
	f.mu.Lock()
	valid := checkErr == nil && current.equal(job.activation) && f.active.equal(job.activation) && f.ctx.Err() == nil
	if f.observer != nil {
		reason := attemptReason(ctx, body, err)
		if checkErr == nil && !valid {
			reason = ReasonCanceled
		}
		f.observer(reason)
	}
	if valid {
		f.cached = true
		if err == nil {
			f.body, f.date = bytes.Clone(body), date.Format(time.DateOnly)
		}
	}
	if f.job == job {
		f.job = nil
	}
	close(job.done)
	f.mu.Unlock()
}
