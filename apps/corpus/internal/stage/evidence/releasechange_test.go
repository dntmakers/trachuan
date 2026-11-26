package evidence

import (
	"os"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/stage/releasechange"
)

type summaryDocument = releasechange.SummaryDocument
type summaryProvision = releasechange.SummaryProvision
type summaryRelation = releasechange.SummaryRelation
type taxonomyNode = releasechange.TaxonomyNode
type assignment = releasechange.Assignment
type validityRow = releasechange.ValidityRow

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

func row(id int64, parent int64, anchor string, assignments ...assignment) summaryProvision {
	p := summaryProvision{ID: id, Anchor: anchor, TextSHA256: hashPrefix + strings.Repeat("0", 64), Assignments: assignments}
	if parent != 0 {
		p.ParentID = &parent
	}
	return p
}
