package evidence

import (
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

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
