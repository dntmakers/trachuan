package releasechange

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

func TestProjectRecords_GeneratedGolden(t *testing.T) {
	in := newChangesCase(t).inputs()
	f, err := os.Open("testdata/changes.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	want, err := ProjectChanges(f)
	if err != nil {
		t.Fatal(err)
	}
	var got []releasechanges.Row
	err = ProjectRecords(false, func(yield func(any) error) error {
		return Generate(ChangeInputs{Current: in.current, Predecessor: in.predecessor, Members: in.Members, Bindings: in.bindings}, yield)
	}, func(row releasechanges.Row) error {
		got = append(got, row)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("generated projection differs from golden: got %v, want %v", got, want)
	}
}

func TestGenerate_BindingOptionAndEmitError(t *testing.T) {
	in := newChangesCase(t).inputs()
	in.bindings.ReportSHA256 = "different"
	stop := errors.New("stop")
	if err := generate(in, func(any) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("unchecked bindings: got %v, want callback error", err)
	}
	in.checkCurrentBindings = true
	if err := generate(in, func(any) error { return stop }); err == nil || errors.Is(err, stop) {
		t.Fatalf("checked bindings: got %v, want binding error", err)
	}
}

func TestProjectRecords_ChangesBetweenPasses(t *testing.T) {
	for _, baseline := range []bool{true, false} {
		calls, emitted := 0, 0
		source := func(yield func(any) error) error {
			calls++
			if calls == 1 && baseline {
				return nil
			}
			key := "a"
			if calls == 2 {
				key = "b"
			}
			return yield(documentChange{Type: changeDocument, Key: key, Change: "added", Cause: "membership"})
		}
		err := ProjectRecords(baseline, source, func(releasechanges.Row) error { emitted++; return nil })
		if err == nil || emitted != 0 {
			t.Fatalf("baseline=%v err=%v emitted=%d", baseline, err, emitted)
		}
	}
}
