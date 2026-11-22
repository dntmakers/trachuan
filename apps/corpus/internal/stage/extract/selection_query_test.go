package extract

import (
	"reflect"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/store"
)

func TestSelectionQueriesExposeFrozenFields(t *testing.T) {
	for _, value := range []any{store.InsertRunOCRSelectionParams{}, store.GetRunOCRSelectionRow{}, store.ListRunOCRSelectionsRow{}} {
		typ := reflect.TypeOf(value)
		for _, field := range []string{"SelectionVersion", "SelectionState", "TextSource", "FetchSha256"} {
			if _, ok := typ.FieldByName(field); !ok {
				t.Errorf("%s missing %s", typ.Name(), field)
			}
		}
	}
}
