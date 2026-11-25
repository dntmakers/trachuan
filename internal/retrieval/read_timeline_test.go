package retrieval

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type timelineEmptyTx struct{ pgx.Tx }

func (timelineEmptyTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if sql == timelineSQL {
		return &cacheRows{}, nil
	}
	return nil, errors.New("unexpected provision query")
}

type timelineErrorRow struct{}

func (timelineErrorRow) Scan(...any) error { return errors.New("unexpected document query") }

func (timelineEmptyTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if sql == `SELECT id FROM document WHERE key=$1` {
		return cacheRow{int64(1)}
	}
	return timelineErrorRow{}
}

func TestReadTimeline_ScopeLoadsOnlyTimelineData(t *testing.T) {
	flag := true
	e := &Engine{}
	_, code, err := e.readScope(t.Context(), timelineEmptyTx{}, cursor{Item: ReadItem{Handle: "vn:1", Timeline: &flag}})
	if err != nil || code != "" {
		t.Fatalf("readScope = %q, %v", code, err)
	}
}

func TestReadTimeline_EmptyDoesNotLoadProvisions(t *testing.T) {
	_, err := readTimeline(t.Context(), timelineEmptyTx{}, 1, "vn:1", 0)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReadTimeline_DecodeRejectsSchemaErrors(t *testing.T) {
	for _, input := range []string{
		`{"handle":"vn:1","timeline":"true"}`,
		`{"handle":"vn:1","timeline":null}`,
		`{"handle":"vn:1","unknown":true}`,
	} {
		var item ReadItem
		if err := json.Unmarshal([]byte(input), &item); err == nil {
			t.Errorf("accepted %s", input)
		}
	}
}

func TestReadTimeline_PageBoundaries(t *testing.T) {
	flag := true
	unit := Unit{Kind: "timeline", Event: &TimelineEvent{Kind: "issued"}}
	seq := readSequence{doc: "vn:1", units: []Unit{unit, unit, unit}, complete: true}
	cap := readSize(unit)
	c := cursor{V: 1, Release: "20990101", Item: ReadItem{Handle: "vn:1", Timeline: &flag}, PageCap: cap, PartSize: cap}
	first, size, err := readPage(seq, c, cap, true)
	if err != nil || size != cap || len(first.Units) != 1 || first.Next == "" || first.Complete {
		t.Fatalf("first page = %+v, size %d, error %v", first, size, err)
	}
	next, code := readCursor(ReadItem{Next: first.Next}, c.Release)
	if code != "" {
		t.Fatal(code)
	}
	deferred, size, err := readPage(seq, next, cap-1, false)
	if err != nil || size != 0 || deferred.NotRead != "call_cap" || deferred.Next != first.Next {
		t.Fatalf("deferred = %+v, size %d, error %v", deferred, size, err)
	}
	second, _, err := readPage(seq, next, cap, true)
	if err != nil || len(second.Units) != 1 || second.Next == "" {
		t.Fatalf("second page = %+v, error %v", second, err)
	}
	lastCursor, code := readCursor(ReadItem{Next: second.Next}, c.Release)
	if code != "" {
		t.Fatal(code)
	}
	last, _, err := readPage(seq, lastCursor, cap, true)
	if err != nil || len(last.Units) != 1 || last.Next != "" || !last.Complete {
		t.Fatalf("last page = %+v, error %v", last, err)
	}
}

func TestReadTimeline_LegacyZeroFields(t *testing.T) {
	for _, input := range []string{
		`{"handle":"vn:1","relations":false}`,
		`{"handle":"vn:1","number":"","from":"","to":"","next":""}`,
		`{"number":"N1","relations":false,"handle":""}`,
	} {
		var item ReadItem
		if err := json.Unmarshal([]byte(input), &item); err != nil || !validReadItem(item) {
			t.Errorf("legacy item rejected: %s, %v", input, err)
		}
	}
}

func TestReadTimeline_ItemValidation(t *testing.T) {
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{`{"handle":"vn:1","timeline":true}`, true},
		{`{"handle":"vn:1","timeline":false}`, false},
		{`{"handle":"vn:1#p1","timeline":true}`, false},
		{`{"handle":"vn:1","timeline":true,"relations":true}`, false},
		{`{"handle":"vn:1","timeline":true,"from":"p1","to":"p2"}`, false},
	} {
		var item ReadItem
		if err := json.Unmarshal([]byte(tc.input), &item); err != nil {
			t.Fatal(err)
		}
		if got := validReadItem(item); got != tc.valid {
			t.Errorf("validReadItem(%s) = %v, want %v", tc.input, got, tc.valid)
		}
	}
}
