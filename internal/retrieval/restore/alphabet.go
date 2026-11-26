package restore

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func letter(r rune) (rune, byte, bool) {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A'), byte(r + ('a' - 'A')), true
	}
	if r >= 'a' && r <= 'z' {
		return r, byte(r), true
	}
	const lower = "àáảãạăằắẳẵặâầấẩẫậèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵđ"
	const upper = "ÀÁẢÃẠĂẰẮẲẴẶÂẦẤẨẪẬÈÉẺẼẸÊỀẾỂỄỆÌÍỈĨỊÒÓỎÕỌÔỒỐỔỖỘƠỜỚỞỠỢÙÚỦŨỤƯỪỨỬỮỰỲÝỶỸỴĐ"
	const folds = "aaaaaaaaaaaaaaaaaeeeeeeeeeeeiiiiiooooooooooooooooouuuuuuuuuuuyyyyyd"
	for i, l := range []rune(lower) {
		if r == l || r == []rune(upper)[i] {
			return l, folds[i], true
		}
	}
	return 0, 0, false
}

type token struct {
	start, end  int
	adjacent    bool
	score, fold string
	ascii       bool
}

func tokenize(text []byte) []token {
	var out []token
	start := -1
	for pos := 0; pos < len(text); {
		r, size := utf8.DecodeRune(text[pos:])
		member := unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r)
		if member && start < 0 {
			start = pos
		}
		if !member && start >= 0 {
			out = appendToken(out, text, start, pos)
			start = -1
		}
		pos += size
	}
	if start >= 0 {
		out = appendToken(out, text, start, len(text))
	}
	return out
}

func appendToken(out []token, text []byte, start, end int) []token {
	t := token{start: start, end: end, ascii: true}
	if len(out) > 0 {
		t.adjacent = adjacent(text[out[len(out)-1].end:start])
	}
	var score, fold strings.Builder
	for _, r := range string(text[start:end]) {
		l, f, ok := letter(r)
		if !ok {
			t.score = string(text[start:end])
			t.ascii = false
			return append(out, t)
		}
		if r > 127 {
			t.ascii = false
		}
		score.WriteRune(l)
		fold.WriteByte(f)
	}
	t.score, t.fold = score.String(), fold.String()
	return append(out, t)
}

func adjacent(separator []byte) bool {
	if len(separator) == 0 {
		return false
	}
	for _, r := range string(separator) {
		if r == '\n' || r == '\r' || r == ' ' || r == ' ' || !unicode.Is(unicode.White_Space, r) {
			return false
		}
	}
	return true
}
