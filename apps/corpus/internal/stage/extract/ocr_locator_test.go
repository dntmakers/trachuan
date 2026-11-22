package extract

import (
	"strings"
	"testing"
)

func locatorFixture() string {
	return "Điều 1. Mở đầu\n" + strings.Repeat("nội dung chung ", 30) + "\ncuối\n\nPHỤ LỤC\nĐiều 5\n42/2026/NĐ-CP\nA A A chung\nĐiều 5\n"
}

func TestLocateOCRReadingPositions(t *testing.T) {
	readings := []string{"Điều 5", "42/2026", "chung", "cuối PHỤ LỤC", "A A", "MISSING", "Điều   5"}
	got := locateOCRReadings(locatorFixture(), readings, 100_000_000)
	want := []string{"tail", "unlocated", "ambiguous", "ambiguous", "tail", "unlocated", "tail"}
	for i := range want {
		if got[i].Position != want[i] {
			t.Errorf("%q position=%s want=%s", readings[i], got[i].Position, want[i])
		}
	}
	location := got[0].Location
	if location == nil || location.TailScopeIndex != 1 || location.StartParagraph != 5 || location.EndParagraph != 5 || location.StartLine != 6 || location.EndLine != 6 {
		t.Fatalf("first location=%+v", location)
	}
	if got[6].Location == nil || *got[6].Location != *location {
		t.Fatalf("normalized duplicate=%+v", got[6])
	}
	if got[4].Location == nil || got[4].Location.StartLine != 8 {
		t.Fatalf("overlap=%+v", got[4])
	}
}

func TestLocateOCRReadingNeedsMain(t *testing.T) {
	for _, text := range []string{strings.Repeat("chung ", 80) + "\nPHỤ LỤC\nĐiều 5", "Điều 1. ngắn\nPHỤ LỤC\nĐiều 5"} {
		got := locateOCRReadings(text, []string{"Điều 5"}, 100_000_000)
		if got[0].Position != "unlocated" {
			t.Fatalf("got=%+v", got)
		}
	}
}

func TestLocateOCRReadingBoundariesAndMain(t *testing.T) {
	text := "Điều 3. Nội dung\n" + strings.Repeat("dài ", 80) + "\nĐiều 50\n42/2026/NĐ-CP 142/2026 foo-bar\nPHỤ LỤC\nkhác"
	readings := []string{"Điều 5", "42/2026", "foo", "Điều 50", "khác"}
	got := locateOCRReadings(text, readings, 100_000_000)
	want := []string{"unlocated", "unlocated", "unlocated", "not_tail", "tail"}
	for i := range want {
		if got[i].Position != want[i] {
			t.Errorf("%q got=%+v want=%s", readings[i], got[i], want[i])
		}
	}
}

func TestLocateOCRReadingBudgetSortedAndShared(t *testing.T) {
	text := "Điều 1. A B\n" + strings.Repeat("body ", 80)
	flat := strings.ReplaceAll(text, "\n", " ")
	got := locateOCRReadings(text, []string{"B", "A", " A ", strings.Repeat("x", 4097)}, int64(len(flat)+1))
	want := []string{"unlocated", "not_tail", "not_tail", "unlocated"}
	for i := range want {
		if got[i].Position != want[i] {
			t.Errorf("index=%d got=%+v want=%s", i, got[i], want[i])
		}
	}
}
