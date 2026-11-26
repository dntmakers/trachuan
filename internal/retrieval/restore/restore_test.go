package restore

import (
	"context"
	"errors"
	"testing"
	"unicode"
)

func TestRestore_Contract(t *testing.T) {
	tests := []struct {
		name, query string
		candidates  []Candidate
		cap         int
		want        string
		changed     bool
	}{
		{"pair wins", "thong tin", []Candidate{{Text: "thống thống thống thông tin"}}, 8, "thông tin", true},
		{"byte tie", "ma", []Candidate{{Text: "má mà"}}, 8, "mà", true},
		{"cap includes typed", "a", []Candidate{{Text: "á á à à ả ả ã ã ạ ạ ă ă ắ ắ â"}}, 8, "à", true},
		{"fixed and separators", "Thong tu 09/2023/TT-NHNN", []Candidate{{Text: "thông tư 09 2023 TT NHNN"}}, 8, "Thông tư 09/2023/TT-NHNN", true},
		{"outside alphabet", "cañon abc9", []Candidate{{Text: "canon abc"}}, 8, "cañon abc9", false},
		{"decomposed", "á", []Candidate{{Text: "á"}}, 8, "á", false},
		{"no fold match", "xyz", []Candidate{{Text: "thông tin"}}, 8, "xyz", false},
		{"multi token tie", "ma ta", []Candidate{{Text: "má tà mà tá"}}, 8, "mà tá", true},
		{"cut boundary", "thong", []Candidate{{Text: "thông", PrefixCut: true}}, 8, "thong", false},
		{"cut after separator", "thong", []Candidate{{Text: "thông ", PrefixCut: true}}, 8, "thông", true},
		{"no pair across chunks", "thong tin", []Candidate{{Text: "thông"}, {Text: "tin"}, {Text: "thống tin"}}, 8, "thống tin", true},
		{"case deduplication", "a tin", []Candidate{{Text: "Á Á á À tin"}}, 3, "à tin", true},
		{"no candidates", "thong tin", nil, 8, "thong tin", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := Restore(context.Background(), []byte(tt.query), tt.candidates, tt.cap)
			if err != nil || string(got) != tt.want || changed != tt.changed {
				t.Fatalf("got %q, %v, %v; want %q, %v", got, changed, err, tt.want, tt.changed)
			}
		})
	}
}

func TestUnicode_RecipeVersion(t *testing.T) {
	if unicode.Version != "17.0.0" {
		t.Fatalf("Unicode version %s: recipe classifications must be re-verified", unicode.Version)
	}
}

func TestOptions_CachedPerFold(t *testing.T) {
	c := counts{singles: map[string]int{"á": 2, "à": 1}, forms: map[string]map[string]struct{}{"a": {"á": {}, "à": {}}}}
	cache := optionCache{counts: c, cap: 3, byFold: make(map[string][]string)}
	first := cache.options(token{score: "a", fold: "a", ascii: true})
	c.singles["à"] = 3
	second := cache.options(token{score: "a", fold: "a", ascii: true})
	if len(cache.byFold) != 1 || first[1] != "á" || second[1] != "á" {
		t.Fatalf("cache not reused: %v %v", first, second)
	}
}

func TestOptions_CapIncludesTyped(t *testing.T) {
	c := counts{singles: map[string]int{}, forms: map[string]map[string]struct{}{"a": {}}}
	for _, form := range []string{"á", "à", "ả", "ã", "ạ", "ă", "ắ", "â"} {
		c.forms["a"][form] = struct{}{}
		c.singles[form] = 1
	}
	cache := optionCache{counts: c, cap: 8, byFold: make(map[string][]string)}
	got := cache.options(token{score: "a", fold: "a", ascii: true})
	if len(got) != 8 || got[0] != "a" || got[7] != "ả" {
		t.Fatalf("unexpected options: %q", got)
	}
}

func TestRestore_Cancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := Restore(ctx, []byte("a"), nil, 8)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestRestore_Determinism(t *testing.T) {
	for i := 0; i < 50; i++ {
		got, _, err := Restore(context.Background(), []byte("ma"), []Candidate{{Text: "má mà"}}, 8)
		if err != nil || string(got) != "mà" {
			t.Fatalf("iteration %d: %q %v", i, got, err)
		}
	}
}

func TestAlphabet_Forms(t *testing.T) {
	rows := []struct {
		lower, upper string
		fold         byte
	}{{"aàáảãạăằắẳẵặâầấẩẫậ", "AÀÁẢÃẠĂẰẮẲẴẶÂẦẤẨẪẬ", 'a'}, {"eèéẻẽẹêềếểễệ", "EÈÉẺẼẸÊỀẾỂỄỆ", 'e'}, {"iìíỉĩị", "IÌÍỈĨỊ", 'i'}, {"oòóỏõọôồốổỗộơờớởỡợ", "OÒÓỎÕỌÔỒỐỔỖỘƠỜỚỞỠỢ", 'o'}, {"uùúủũụưừứửữự", "UÙÚỦŨỤƯỪỨỬỮỰ", 'u'}, {"yỳýỷỹỵ", "YỲÝỶỸỴ", 'y'}, {"đ", "Đ", 'd'}}
	for _, row := range rows {
		for _, r := range row.lower + row.upper {
			_, f, ok := letter(r)
			if !ok || f != row.fold {
				t.Errorf("%c: %c %v", r, f, ok)
			}
		}
	}
}

func TestTokenize_Boundaries(t *testing.T) {
	ts := tokenize([]byte("a b,c\nd\re f g"))
	if len(ts) != 7 {
		t.Fatalf("got %d tokens", len(ts))
	}
	for i := 1; i < len(ts); i++ {
		if ts[i].adjacent != (i == 1) {
			t.Errorf("token %d adjacent=%v", i, ts[i].adjacent)
		}
	}
}
