package testpg

import (
	"context"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestMarkerRunKey(t *testing.T) {
	for _, tc := range []struct{ marker, want string }{
		{`{"run_key":"vn/20991215"}`, "vn/20991215"},
		{`{"run_key":"vn/20991215","run_key":"vn/20991215"}`, ""},
		{`{"RUN_KEY":"vn/20991215"}`, ""},
		{`{"run_key":"vn/20991215_01"}`, ""},
		{`{"run_key":"vn/20991215_00"}`, ""},
		{`{"run_key":"vn/20991215","other":true}`, ""},
		{`{"run_key":"vn/20991215","instance":null}`, ""},
		{`{"run_key":"vn/20991215","build_input_hash":"x","build_recipe_hash":"y","instance":"built"}`, "vn/20991215"},
		{`{"run_key":"vn/20261215"}`, ""},
		{`{"run_key":"vn/20991399"}`, ""},
		{`not json`, ""},
		{`{"run_key":42}`, ""},
	} {
		if got := MarkerRunKey(&tc.marker); got != tc.want {
			t.Errorf("MarkerRunKey(%q) = %q, want %q", tc.marker, got, tc.want)
		}
	}
	if got := MarkerRunKey(nil); got != "" {
		t.Errorf("nil marker = %q", got)
	}
}

func TestSameMarkerIdentity(t *testing.T) {
	original := DatabaseMarker{OID: 42, Marker: `{"run_key":"vn/20991215"}`}
	for _, tc := range []struct {
		current DatabaseMarker
		want    bool
	}{
		{original, true},
		{DatabaseMarker{OID: 43, Marker: original.Marker}, false},
		{DatabaseMarker{OID: 42, Marker: `{"run_key":"vn/20991216"}`}, false},
	} {
		if got := sameMarkerIdentity(original, tc.current); got != tc.want {
			t.Errorf("sameMarkerIdentity(%+v) = %t, want %t", tc.current, got, tc.want)
		}
	}
}

type busyDrop struct{ calls int }

func (d *busyDrop) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	d.calls++
	return pgconn.CommandTag{}, &pgconn.PgError{Code: "55006"}
}

func TestDropOnce_DoesNotRetryBusyDatabase(t *testing.T) {
	d := &busyDrop{}
	if err := dropOnce(context.Background(), d, "test"); err == nil || d.calls != 1 {
		t.Fatalf("calls=%d error=%v", d.calls, err)
	}
}

func TestProtectedPruneDatabase(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"test_template_example", true},
		{"corpus_tpl_910e3eb3f5e4", true},
		{"corpus_vn_20991215", false},
	} {
		if got := protectedPruneDatabase(tc.name); got != tc.want {
			t.Errorf("protectedPruneDatabase(%q) = %t, want %t", tc.name, got, tc.want)
		}
	}
}
