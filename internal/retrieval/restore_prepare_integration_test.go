//go:build integration

package retrieval

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type restorationTrace struct {
	queries  int
	onLookup func()
}

func (t *restorationTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == restorationSQL {
		t.queries++
		if t.onLookup != nil {
			t.onLookup()
		}
	}
	return ctx
}

func (*restorationTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestPrepareRestoration(t *testing.T) {
	trace := &restorationTrace{}
	db, e, f := fixtureWithTrace(t, trace)
	r := Recipe4()
	r.Abstention = e.release.RetrievalRecipe.Abstention
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(context.Background(), `UPDATE release SET retrieval_recipe=$1::jsonb`, string(raw)); err != nil {
		t.Fatal(err)
	}
	e, err = Open(context.Background(), db, f, fixtureOptions())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := e.Prepare(ctx, Request{Query: "ngan hang", Mode: "exact"})
	if err != nil || p.EffectiveQuery != "ngân hàng" || !p.QueryChanged || p.Request.Query != "ngan hang" {
		t.Fatalf("prepare: %+v, %v", p, err)
	}
	a, err := e.Collect(ctx, p, Depths{Dense: 5, Lexical: 5})
	if err != nil || len(a.Primary.Lexical) == 0 || trace.queries != 1 {
		t.Fatalf("collect: %+v %v, lookups %d", a, err, trace.queries)
	}
	accented, err := e.Prepare(ctx, Request{Query: "ngân hàng", Mode: "exact"})
	if err != nil || trace.queries != 1 {
		t.Fatalf("accented prepare: %v", err)
	}
	accentedArms, err := e.Collect(ctx, accented, Depths{Dense: 5, Lexical: 5})
	if err != nil || len(accentedArms.Primary.Lexical) == 0 || a.Primary.Lexical[0].ChunkID != accentedArms.Primary.Lexical[0].ChunkID {
		t.Fatalf("accented collection: %v", err)
	}
	if _, err = e.Prepare(ctx, Request{Query: "50/2024/TT-NHNN"}); err != nil || trace.queries != 1 {
		t.Fatalf("unexpected document number lookup: %v", err)
	}
	for _, req := range []Request{{Query: "ngan hang", Documents: []string{"vn:3"}}, {Query: "ngan hang", InForceOnly: new(bool)}} {
		other, err := e.Prepare(ctx, req)
		if err != nil || other.EffectiveQuery != p.EffectiveQuery {
			t.Fatalf("filter: %+v %v", other, err)
		}
	}
	e.release.RetrievalRecipe = Recipe3()
	rejected, err := e.Prepare(ctx, Request{Query: "ngan hang"})
	if err != nil || rejected.Validation.Reason != "diacritics_required" {
		t.Fatalf("rejection: %+v %v", rejected, err)
	}
	e.release.RetrievalRecipe = Recipe4()
	for _, expired := range []bool{false, true} {
		before := f.calls
		var c context.Context
		var cancel context.CancelFunc
		if expired {
			c, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
		} else {
			c, cancel = context.WithCancel(ctx)
			cancel()
		}
		_, err := e.Prepare(c, Request{Query: "ngan hang"})
		cancel()
		if !errors.Is(err, ErrUnavailable) || f.calls != before {
			t.Fatalf("deadline: %v, calls %d", err, f.calls-before)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	trace.onLookup = cancel
	before, previousLookups := f.calls, trace.queries
	_, err = e.Prepare(ctx, Request{Query: "ngan hang"})
	if !errors.Is(err, ErrUnavailable) || f.calls != before || trace.queries != previousLookups+1 {
		t.Fatalf("cancel during lookup: %v, embed calls %d, lookups %d", err, f.calls-before, trace.queries-previousLookups)
	}
}

func TestRestorationContinuation(t *testing.T) {
	trace := &restorationTrace{}
	db, e, f := fixtureWithTrace(t, trace)
	if _, err := db.Exec(context.Background(), `UPDATE document SET eff_to='2025-01-02' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	e.options.Now = func() time.Time { return now }
	r := Recipe4()
	r.Abstention = e.release.RetrievalRecipe.Abstention
	e.release.RetrievalRecipe = r
	req := Request{Query: "ngan hang", Mode: "exact", Limit: 1}
	first, err := e.Search(context.Background(), req)
	if err != nil || first.NextCursor == "" || trace.queries != 1 || f.calls != 1 || f.text != "ngân hàng" {
		t.Fatalf("first page: %v, cursor %q, lookups %d, calls %d, text %q", err, first.NextCursor, trace.queries, f.calls, f.text)
	}
	now = now.Add(48 * time.Hour)
	req.Cursor = first.NextCursor
	second, err := e.Search(context.Background(), req)
	if err != nil || trace.queries != 2 || f.calls != 2 || f.text != "ngân hàng" {
		t.Fatalf("continuation: %v, lookups %d, calls %d, text %q", err, trace.queries, f.calls, f.text)
	}
	for _, page := range []Response{first, second} {
		if page.RestoredQuery != "ngân hàng" {
			t.Fatalf("restored query missing: %+v", page)
		}
		for _, detail := range []Detail{DetailCompact, DetailFull} {
			result, presentErr := e.Present(context.Background(), page, detail)
			if presentErr != nil || result.RestoredQuery != "ngân hàng" {
				t.Fatalf("presentation: %+v %v", result, presentErr)
			}
		}
	}
	req.Query = "ngân hàng"
	if _, err = e.Search(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("edited continuation: %v", err)
	}
	threshold := 1.1
	now = now.Add(-48 * time.Hour)
	e.release.RetrievalRecipe.Abstention.Threshold = &threshold
	abstained, err := e.Search(context.Background(), Request{Query: "ngan hang", Mode: "exact"})
	if err != nil || !abstained.Abstain || abstained.RestoredQuery != "ngân hàng" {
		t.Fatalf("abstention: %+v %v", abstained, err)
	}
}
