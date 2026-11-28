package parse

import (
	"strings"
	"testing"

	"danny.vn/trachuan/internal/pdftext"
)

func TestCOBIT_PracticeTitleWrap(t *testing.T) {
	for _, tc := range []struct {
		name, id            string
		missing             bool
		wantTitle, wantBody string
		wantErr             bool
	}{
		{name: "listed wraps", id: "APO01.02", wantTitle: "Invented Invented description", wantBody: "Following"},
		{name: "listed missing continuation", id: "APO01.02", missing: true, wantErr: true},
		{name: "unlisted stays one line", id: "APO01.01", wantTitle: "Invented", wantBody: "Invented description Following"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, w := cobitFixture()
			l[28] = strings.ReplaceAll(l[28], "EDM01", "APO01")
			for i := range w[28].Words {
				w[28].Words[i].Text = strings.ReplaceAll(w[28].Words[i].Text, "EDM01.01", tc.id)
			}
			if tc.missing {
				w[28].Words = append(w[28].Words[:9], w[28].Words[11:]...)
			} else {
				w[28].Words = append(w[28].Words, cobitWord("40", "34", "Following"))
			}
			expected := 3
			result, err := parseCOBIT(l, w, 1994, cobitSHA, &expected)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "missing title continuation") {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Nodes[2].PublisherTitle != tc.wantTitle || result.Nodes[2].Body != tc.wantBody {
				t.Fatalf("unexpected node: %#v", result.Nodes[2])
			}
		})
	}
}

func TestCOBIT_BlankTitleContinuation(t *testing.T) {
	l, w := cobitFixture()
	l[28] = strings.ReplaceAll(l[28], "EDM01", "APO01")
	w[28].Words[7].Text = "APO01.02"
	w[28].Words[9].Text = " "
	w[28].Words[10].Text = ""
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil || !strings.Contains(err.Error(), "missing title continuation") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCOBIT_TitleContinuationRejectsInvalidCell(t *testing.T) {
	l, w := cobitFixture()
	l[28] = strings.ReplaceAll(l[28], "EDM01", "APO01")
	for i := range w[28].Words {
		if w[28].Words[i].Text == "EDM01.01" {
			w[28].Words[i].Text = "APO01.02"
		}
	}
	w[28].Words[9].Text = "�"
	w[28].Words = append(w[28].Words, pdftext.Word{XMin: "40", XMax: "41", YMin: "34", YMax: "35", Text: "Following"})
	expected := 3
	if _, err := parseCOBIT(l, w, 1994, cobitSHA, &expected); err == nil {
		t.Fatal("accepted invalid title text")
	}
}
