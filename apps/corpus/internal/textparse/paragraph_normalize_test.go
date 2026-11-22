package textparse

import (
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func oldNormalize(s string) string { return norm.NFC.String(strings.Join(strings.Fields(s), " ")) }

func TestNormalizeUnicodeAndManyWords(t *testing.T) {
	for _, s := range []string{"", " \t\n  ", " A\t é B  C ", "x\xffy  z", strings.Repeat("a b  ", 5000)} {
		if got, want := normalize(s), oldNormalize(s); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	}
}

func TestNormalizeManyWordsAllocation(t *testing.T) {
	input := strings.Repeat("word ", 5000)
	measure := func(f func(string) string) int64 {
		return testing.Benchmark(func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = f(input)
			}
		}).AllocedBytesPerOp()
	}
	old, current := measure(oldNormalize), measure(normalize)
	t.Logf("old=%d bytes/op current=%d bytes/op", old, current)
	if current >= old {
		t.Fatalf("normalization did not reduce allocated bytes")
	}
}

func TestTextParagraphBlankLocatorsAndLimit(t *testing.T) {
	p, err := ParagraphsFromText("\nfirst\n\nsecond\n")
	if err != nil || len(p) != 2 || p[0].Ordinal != 1 || p[0].Locator != 2 || p[1].Ordinal != 2 || p[1].Locator != 4 {
		t.Fatalf("paragraphs=%+v err=%v", p, err)
	}
	if _, err := ParagraphsFromText(strings.Repeat("x\n", maxParagraphs+1)); err == nil {
		t.Fatal("paragraph limit not enforced")
	}
}
