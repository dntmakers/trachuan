package parse

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestISO27001Amd1Pages(t *testing.T) {
	const sha = "9a17aec8d8a1d79839bbe0833cc48202b65ed71e478fee0c78e6ba4b2c6dc6a7"
	const page = "  ISO/IEC 27001:2022/Amd. 1:2024(en)\n\nPreamble\n\n4.1\nAdd the following sentence at the end of each clause:\nInvented   statement.\n\n4.2\naDD the following NOTE at the END of each clause:\nNOTE 2  Invented note.\n\n  \uFEFF\n  © ISO/IEC 2024 – All rights reserved\n\n  1\n"
	count := 2
	want := []Node{{Citation: "4.1", CitationNorm: "4.1", Kind: "amendment", Parent: -1, Ordinal: 0, Body: "Invented statement.", Status: "active"}, {Citation: "4.2", CitationNorm: "4.2", Kind: "amendment", Parent: -1, Ordinal: 1, Body: "NOTE 2 Invented note.", Status: "active"}}
	t.Run("valid", func(t *testing.T) {
		got, err := parseISO27001Amd1Pages([]string{"unrelated", page}, sha, &count)
		if err != nil {
			t.Fatal(err)
		}
		again, err := parseISO27001Amd1Pages([]string{"unrelated", page}, sha, &count)
		if err != nil || !reflect.DeepEqual(got, again) {
			t.Fatal("nondeterministic result")
		}
		if !reflect.DeepEqual(got.Nodes, want) {
			t.Fatalf("nodes mismatch: %#v", got.Nodes)
		}
	})
	cases := map[string]string{
		"instruction unicode separator": strings.Replace(page, "the following sentence", "t\u2028he following sentence", 1),
		"missing instruction word":      strings.Replace(page, "Add the following sentence at the end", "Add following sentence at the end", 1),
		"inserted operation word":       strings.Replace(page, "Add the following sentence at the end", "Add the following sentence delete at the end", 1),
		"missing footer":                strings.Replace(page, "© ISO/IEC 2024 – All rights reserved", "missing", 1),
		"missing page number":           strings.Replace(page, "  1\n", "", 1),
		"missing target":                strings.Replace(page, "\n4.2\n", "\n4.3\n", 1),
		"repeated target":               strings.Replace(page, "\n4.2\n", "\n4.1\n", 1),
		"extra block":                   strings.Replace(page, "\n  \uFEFF", "\n4.3\nAdd the following sentence at the end of each clause:\nExtra.\n  \uFEFF", 1),
		"extra insertion":               strings.Replace(page, "Invented   statement.", "Invented statement.\nAnother line.", 1),
		"indented insertion":            strings.Replace(page, "Invented   statement.", " Invented statement.", 1),
		"multiple operations":           strings.Replace(page, "Add the following sentence at the end", "Add the following sentence and delete text at the end", 1),
		"out of order block":            strings.Replace(page, "\n4.1\n", "\n4.2\nAdd a note at the end:\nNOTE 9 invented.\n4.1\n", 1),
		"non ASCII indentation":         strings.Replace(page, "Invented   statement.", "\u00a0Invented statement.", 1),
		"unicode line separator":        strings.Replace(page, "Invented   statement.", "Invented\u2028statement.", 1),
		"wrong operation":               strings.Replace(page, "Add the following sentence", "Delete the following sentence", 1),
		"wrong word order":              strings.Replace(page, "Add the following sentence at the end", "Add the following end of the sentence", 1),
		"missing note":                  strings.Replace(page, "NOTE 2  Invented note.", "Invented note.", 1),
		"missing period":                strings.Replace(page, "Invented   statement.", "Invented statement", 1),
		"wrong header":                  strings.Replace(page, "(en)", "(fr)", 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			input = strings.Replace(input, "Preamble", "SENTINEL_INPUT_MARKER", 1)
			_, err := parseISO27001Amd1Pages([]string{input}, sha, &count)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != "invalid amendment blocks or footer" && err.Error() != "missing amendment page" {
				t.Fatalf("unexpected error: %q", err.Error())
			}
		})
	}
	t.Run("duplicate page", func(t *testing.T) {
		if _, err := parseISO27001Amd1Pages([]string{page, page}, sha, &count); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("wrong fingerprint", func(t *testing.T) {
		if _, err := parseISO27001Amd1Pages([]string{page}, "wrong", &count); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("wrong count", func(t *testing.T) {
		n := 3
		if _, err := parseISO27001Amd1Pages([]string{page}, sha, &n); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("bounded allocation", func(t *testing.T) {
		input := strings.Repeat("\n", 10_000_000)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err := parseISO27001Amd1Pages([]string{input}, sha, &count)
		runtime.ReadMemStats(&after)
		if err == nil {
			t.Fatal("expected error")
		}
		if after.TotalAlloc-before.TotalAlloc > 20_000_000 {
			t.Fatal("excessive allocation")
		}
	})

	t.Run("bounded insertion allocation", func(t *testing.T) {
		body := strings.Repeat("x ", 5_000_000) + "x."
		input := strings.Replace(page, "Invented   statement.", body, 1)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		_, err := parseISO27001Amd1Pages([]string{input}, sha, &count)
		runtime.ReadMemStats(&after)
		if err != nil {
			t.Fatal(err)
		}
		if after.TotalAlloc-before.TotalAlloc > 30_000_000 {
			t.Fatal("excessive insertion allocation")
		}
	})

}
