package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"danny.vn/trachuan/apps/server/internal/publicsample"
	"danny.vn/trachuan/apps/server/internal/publicsummary"
	"danny.vn/trachuan/apps/server/internal/release"
	"github.com/jackc/pgx/v5/pgxpool"
)

type samplePool interface{ Close() }

type samplePools struct {
	mu       sync.Mutex
	open     func(context.Context, string) (samplePool, error)
	active   publicsample.Activation
	pool     samplePool
	retiring <-chan struct{}
	closed   bool
}

func sameSampleActivation(a, b publicsample.Activation) bool {
	return a.Database == b.Database && a.ActivatedAt.Equal(b.ActivatedAt)
}

func (p *samplePools) observe(a publicsample.Activation) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sameSampleActivation(p.active, a) {
		return
	}
	p.active = a
	if p.pool != nil {
		pool := p.pool
		p.pool = nil
		done := make(chan struct{})
		p.retiring = done
		go func() { pool.Close(); close(done) }()
	}
}

func (p *samplePools) available() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.availableLocked()
}

func (p *samplePools) availableLocked() bool {
	if p.closed {
		return false
	}
	if p.retiring != nil {
		select {
		case <-p.retiring:
			p.retiring = nil
		default:
			return false
		}
	}
	return true
}

func (p *samplePools) get(ctx context.Context, a publicsample.Activation) (samplePool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.availableLocked() || !sameSampleActivation(p.active, a) {
		return nil, errors.New("sample pool unavailable")
	}
	if p.pool == nil {
		pool, err := p.open(ctx, a.Database)
		if err != nil {
			return nil, err
		}
		p.pool = pool
	}
	return p.pool, nil
}

func (p *samplePools) Close() {
	p.mu.Lock()
	p.closed = true
	pool, retiring := p.pool, p.retiring
	p.pool = nil
	p.mu.Unlock()
	if pool != nil {
		pool.Close()
	}
	if retiring != nil {
		<-retiring
	}
}

func sampleMetadata(read func(context.Context) (publicsummary.Release, error)) func(context.Context) (publicsample.Activation, error) {
	return func(ctx context.Context) (publicsample.Activation, error) {
		r, err := read(ctx)
		if err != nil {
			return publicsample.Activation{}, err
		}
		return publicsample.Activation{Database: r.Database, ActivatedAt: r.ActivatedAt}, nil
	}
}

func sampleAcquire(pools *samplePools, resolve func(string) *release.Lease, run func(context.Context, *release.Lease, samplePool, time.Time) ([]byte, error)) func(publicsample.Activation) (func(context.Context, time.Time) ([]byte, error), bool) {
	return func(a publicsample.Activation) (func(context.Context, time.Time) ([]byte, error), bool) {
		if !pools.available() {
			return nil, false
		}
		lease := resolve(a.Database)
		if lease == nil {
			return nil, false
		}
		return func(ctx context.Context, instant time.Time) ([]byte, error) {
			defer lease.Done()
			pool, err := pools.get(ctx, a)
			if err != nil {
				return nil, err
			}
			return run(ctx, lease, pool, instant)
		}, true
	}
}

func sampleAttemptLogger(logger *slog.Logger) func(publicsample.Reason) {
	return func(reason publicsample.Reason) { logger.Info("public sample attempt", "reason", reason) }
}

func newPublicSample(ctx context.Context, metadataDB publicsummary.MetadataDB, template string, resolver *release.Resolver, clock func() time.Time, logger *slog.Logger) (*publicsample.Flights, func()) {
	pools := &samplePools{open: func(ctx context.Context, database string) (samplePool, error) {
		cfg, err := samplePoolConfig(template, database)
		if err != nil {
			return nil, err
		}
		return pgxpool.NewWithConfig(ctx, cfg)
	}}
	read := sampleMetadata(publicsummary.Metadata(metadataDB))
	metadata := func(ctx context.Context) (publicsample.Activation, error) {
		a, err := read(ctx)
		if err == nil && a.Database != "" && !a.ActivatedAt.IsZero() {
			pools.observe(a)
		}
		return a, err
	}
	acquire := sampleAcquire(pools, resolver.ReadyLaw, func(ctx context.Context, lease *release.Lease, pool samplePool, instant time.Time) ([]byte, error) {
		return publicsample.SearchSample(ctx, lease.Engine.WithPool(pool.(*pgxpool.Pool)), lease.Release, instant)
	})
	flights := publicsample.NewFlights(ctx, metadata, nil, nil, clock).WithAcquire(acquire).WithObserver(sampleAttemptLogger(logger))
	return flights, func() { flights.Close(); pools.Close() }
}
