package pdftext

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseBBox_Structure(t *testing.T) {
	input := `<html xmlns="http://www.w3.org/1999/xhtml"><head></head><body><doc><page width="100" height="100"><flow><block xMin="1" yMin="2" xMax="30" yMax="40"><line xMin="1" yMin="2" xMax="20" yMax="10"><word xMin="1" yMin="2" xMax="10" yMax="10">one</word><word xMin="11" yMin="2" xMax="20" yMax="10">two</word></line><line xMin="1" yMin="12" xMax="10" yMax="20"><word xMin="1" yMin="12" xMax="10" yMax="20">three</word></line></block></flow></page></doc></body></html>`
	one := Word{XMin: "1", YMin: "2", XMax: "10", YMax: "10", Text: "one"}
	two := Word{XMin: "11", YMin: "2", XMax: "20", YMax: "10", Text: "two"}
	three := Word{XMin: "1", YMin: "12", XMax: "10", YMax: "20", Text: "three"}
	want := []Page{{Width: "100", Height: "100", Words: []Word{one, two, three}, Blocks: []Block{{XMin: "1", YMin: "2", XMax: "30", YMax: "40", Lines: []Line{{XMin: "1", YMin: "2", XMax: "20", YMax: "10", Words: []Word{one, two}}, {XMin: "1", YMin: "12", XMax: "10", YMax: "20", Words: []Word{three}}}}}}}
	got, err := parseBBox([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("structure mismatch: got %#v, want %#v", got, want)
	}
}

func TestParseBBox_RejectsOrphanElements(t *testing.T) {
	for name, input := range map[string]string{
		"word outside line":  strings.ReplaceAll(strings.ReplaceAll(validXML, "<line>", ""), "</line>", ""),
		"line outside block": strings.ReplaceAll(strings.ReplaceAll(validXML, "<block>", ""), "</block>", ""),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseBBox([]byte(input)); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}
