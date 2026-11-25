package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/publicsample"
	"danny.vn/trachuan/apps/server/internal/publicsummary"
	"danny.vn/trachuan/apps/server/internal/release"
)

func TestSampleAttemptLogger_FixedReason(t *testing.T) {
	var output bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&output, nil))
	sampleAttemptLogger(log)(publicsample.ReasonTimeout)
	if !strings.Contains(output.String(), `"reason":"timeout"`) || strings.Count(output.String(), "\n") != 1 {
		t.Fatalf("log %q", output.String())
	}
}

func TestSampleMetadata_Activation(t *testing.T) {
	want := publicsummary.Release{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	read := sampleMetadata(func(context.Context) (publicsummary.Release, error) { return want, nil })
	got, err := read(context.Background())
	if err != nil || got.Database != want.Database || !got.ActivatedAt.Equal(want.ActivatedAt) {
		t.Fatalf("activation %v, error %v", got, err)
	}
	read = sampleMetadata(func(context.Context) (publicsummary.Release, error) {
		return publicsummary.Release{}, errors.New("failed")
	})
	if _, err := read(context.Background()); err == nil {
		t.Fatal("missing metadata error")
	}
}

func TestSamplePool_ActivationReplacement(t *testing.T) {
	var created, closed int
	p := &samplePools{open: func(context.Context, string) (samplePool, error) {
		created++
		return &fakeSamplePool{close: func() { closed++ }}, nil
	}}
	a := publicsample.Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	p.observe(a)
	one, err := p.get(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	two, err := p.get(context.Background(), a)
	if err != nil || one != two || created != 1 {
		t.Fatalf("pool reuse: created %d, error %v", created, err)
	}
	a.ActivatedAt = a.ActivatedAt.Add(time.Second)
	p.observe(a)
	<-p.retiring
	_, err = p.get(context.Background(), a)
	if err != nil || created != 2 || closed != 1 {
		t.Fatalf("replacement: created %d closed %d error %v", created, closed, err)
	}
	a.Database = "corpus_vn_20990102"
	p.observe(a)
	<-p.retiring
	if _, err = p.get(context.Background(), a); err != nil || created != 3 || closed != 2 {
		t.Fatalf("database replacement: created %d closed %d error %v", created, closed, err)
	}
	p.Close()
	if closed != 3 {
		t.Fatalf("closed %d", closed)
	}
}

func TestSamplePool_RetirementPreventsOverlap(t *testing.T) {
	allowClose := make(chan struct{})
	closing := make(chan struct{})
	p := &samplePools{open: func(context.Context, string) (samplePool, error) {
		return &fakeSamplePool{close: func() { close(closing); <-allowClose }}, nil
	}}
	a := publicsample.Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	p.observe(a)
	if _, err := p.get(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	a.ActivatedAt = a.ActivatedAt.Add(time.Second)
	p.observe(a)
	<-closing
	if p.available() {
		t.Fatal("pool available before retirement")
	}
	if _, err := p.get(context.Background(), a); err == nil {
		t.Fatal("opened during retirement")
	}
	close(allowClose)
	<-p.retiring
	if !p.available() {
		t.Fatal("pool unavailable after retirement")
	}
	p.Close()
}

func TestSampleAcquire_UsesReservedLease(t *testing.T) {
	a := publicsample.Activation{Database: "corpus_vn_20990101", ActivatedAt: time.Unix(1, 0)}
	p := &samplePools{open: func(context.Context, string) (samplePool, error) { return &fakeSamplePool{close: func() {}}, nil }}
	p.observe(a)
	defer p.Close()
	calls := 0
	resolve := func(string) *release.Lease {
		calls++
		if calls > 1 {
			return nil
		}
		return &release.Lease{Release: "20990101"}
	}
	acquire := sampleAcquire(p, resolve, func(_ context.Context, lease *release.Lease, _ samplePool, _ time.Time) ([]byte, error) {
		return []byte(lease.Release), nil
	})
	search, ok := acquire(a)
	if !ok {
		t.Fatal("lease rejected")
	}
	body, err := search(context.Background(), time.Now())
	if err != nil || string(body) != "20990101" || calls != 1 {
		t.Fatalf("body %q err %v resolves %d", body, err, calls)
	}
	if _, ok = acquire(a); ok {
		t.Fatal("missing lease accepted")
	}
}

type fakeSamplePool struct{ close func() }

func (p *fakeSamplePool) Close() { p.close() }
