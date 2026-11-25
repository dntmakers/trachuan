package retrieval

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSourceFileURL(t *testing.T) {
	for _, tc := range []struct {
		source, url string
		want        bool
	}{
		{"chinhphu", "https://datafiles.chinhphu.vn/a", true},
		{"chinhphu", "https://DATAFILES.CHINHPHU.VN:443/a", true},
		{"vbpl", "https://datafiles.chinhphu.vn/a", false},
		{"chinhphu", "https://datafiles.chinhphu.vn/a?", false},
		{"chinhphu", "https://datafiles.chinhphu.vn/a?sig=x", false},
		{"chinhphu", "http://datafiles.chinhphu.vn/a", false},
		{"chinhphu", "https://datafileſ.chinhphu.vn/a", false},
		{"chinhphu", "https://internal/a", false},
		{"chinhphu", "https://datafiles.chinhphu.vn./a", false},
		{"chinhphu", "https://datafiles.chinhphu.vn:444/a", false},
		{"chinhphu", "https://datafiles.chinhphu.vn/invalid<path", false},
		{"chinhphu", "https://datafiles.chinhphu.vn/invalid[path", false},
		{"chinhphu", "https://datafiles.chinhphu.vn/điều", false},
		{"chinhphu", "https://datafiles.chinhphu.vn/a%20b!$&'()*+,;=:@/~_-.pdf", true},
		{"chinhphu", "https://datafiles.chinhphu.vn/ a", false},
		{"chinhphu", "https://datafiles.chinhphu.vn", false},
		{"chinhphu", ":bad", false},
	} {
		if got := publicSourceURL(tc.source, &tc.url); got != tc.want {
			t.Errorf("%s %s: %v", tc.source, tc.url, got)
		}
	}
}

func TestFilePreviewCaps(t *testing.T) {
	files := make([]SourceFile, 25)
	for i := range files {
		files[i] = SourceFile{Name: "a", Hash: strings.Repeat("a", 64), Role: "main"}
	}
	got, omitted, present, _ := filePreview(files, len(files), ReadCallCap, cursor{}, false)
	if len(got) != 20 || omitted != 5 || !present {
		t.Fatalf("count cap: %d %d %v", len(got), omitted, present)
	}
	got, omitted, present, _ = filePreview(files[:21], 100, ReadCallCap, cursor{}, false)
	if len(got) != 20 || omitted != 80 || !present {
		t.Fatalf("bounded total: %d %d %v", len(got), omitted, present)
	}
	files[0].Name = strings.Repeat("đ", 8200)
	got, omitted, present, _ = filePreview(files, len(files), ReadCallCap, cursor{}, false)
	if len(got) != 0 || omitted != 25 || present {
		t.Fatal("code point cap")
	}
	_, omitted, present, _ = filePreview(files, len(files), 1, cursor{}, false)
	if omitted != 0 || present {
		t.Fatal("count does not fit")
	}
}

func TestSearchDocumentHasNoFiles(t *testing.T) {
	raw, err := json.Marshal(DocumentEntry{Title: "Law"})
	if err != nil || strings.Contains(string(raw), `"files`) {
		t.Fatalf("search document: %s %v", raw, err)
	}
}
