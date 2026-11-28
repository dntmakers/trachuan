package pdftext

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestBBoxReplacement_RejectsInvalidContext(t *testing.T) {
	const xml = `<html xmlns="http://www.w3.org/1999/xhtml"><head><meta content="VALUE"/></head><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">text</word></line></block></flow></page></doc></body></html>`
	for _, tc := range []struct{ name, replacement string }{{"control in attribute", "\a"}, {"surrogate reference", "&#xD800;"}} {
		t.Run(tc.name, func(t *testing.T) {
			input := strings.Replace(xml, "VALUE", tc.replacement, 1)
			if _, _, err := parseBBoxReplacement(context.Background(), []byte(input), '\a'); err == nil {
				t.Fatal("accepted invalid XML")
			}
		})
	}
}

func TestBBoxReplacement_XMLReferences(t *testing.T) {
	const body = `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>TITLE</title></head><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">WORD</word></line></block></flow></page></doc></body></html>`
	for _, tc := range []struct{ name, input string }{
		{"surrogate in title", strings.Replace(body, "TITLE", "&#55296;", 1)},
		{"surrogate in word", strings.Replace(body, "WORD", "&#xDFFF;", 1)},
		{"control in title", strings.Replace(body, "TITLE", "\a", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := parseBBoxReplacement(context.Background(), []byte(tc.input), '\a'); err == nil {
				t.Fatal("accepted invalid input")
			}
		})
	}
	for _, input := range []string{strings.Replace(body, "WORD", "&amp;#xD800;", 1), strings.Replace(body, "WORD", "<![CDATA[&#xD800;]]>", 1)} {
		if _, _, err := parseBBoxReplacement(context.Background(), []byte(input), '\a'); err != nil {
			t.Fatal("rejected literal reference")
		}
	}
}

type cancelDuringScan struct {
	context.Context
	calls int
}

func (c *cancelDuringScan) Err() error {
	c.calls++
	if c.calls >= 2 {
		return context.Canceled
	}
	return nil
}

func TestBBoxScan_InvalidCommentsAndCancellation(t *testing.T) {
	const body = `<html xmlns="http://www.w3.org/1999/xhtml"><head><!--VALUE--></head><body><doc><page width="10" height="20"/></doc></body></html>`
	for _, value := range []string{"\x01", string([]byte{0xff})} {
		input := []byte(strings.Replace(body, "VALUE", value, 1))
		if _, err := parseBBox(input); err == nil {
			t.Fatal("ordinary decoder accepted invalid comment")
		}
		if _, _, err := parseBBoxReplacement(context.Background(), input, '\a'); err == nil {
			t.Fatal("replacement decoder accepted invalid comment")
		}
	}
	ctx := &cancelDuringScan{Context: context.Background()}
	if _, _, err := scanBBox(ctx, []byte(strings.Repeat("x", 1_000_000)), false); err == nil {
		t.Fatal("scanner did not check cancellation during long text")
	}
}

func TestBBoxReplacement_LongNumericReference(t *testing.T) {
	const xml = `<html xmlns="http://www.w3.org/1999/xhtml"><head/><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">VALUE</word></line></block></flow></page></doc></body></html>`
	data := []byte(strings.Replace(xml, "VALUE", "&#"+strings.Repeat("0", 100)+"65;", 1))
	if _, err := parseBBox(data); err != nil {
		t.Fatal("rejected valid numeric reference")
	}
}

func TestBBoxReplacement_DELParity(t *testing.T) {
	const body = `<html xmlns="http://www.w3.org/1999/xhtml"><head/><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">VALUE</word></line></block></flow></page></doc></body></html>`
	for _, value := range []string{"\x7f", "&#127;"} {
		input := []byte(strings.Replace(body, "VALUE", value, 1))
		plain, err := parseBBox(input)
		if err != nil {
			t.Fatal(err)
		}
		replaced, count, err := parseBBoxReplacement(context.Background(), input, '\a')
		if err != nil || count != 0 || !reflect.DeepEqual(plain, replaced) {
			t.Fatal("DEL parity failed")
		}
	}
}

func TestBBoxReplacement_WordContext(t *testing.T) {
	const body = `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>TITLE</title></head><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">WORD</word></line></block></flow></page></doc></body></html>`
	for _, input := range []string{strings.Replace(body, "TITLE", "\a", 1), strings.Replace(body, "WORD", "&#7;", 1), strings.Replace(body, "WORD", "&#x07;", 1)} {
		if _, _, err := parseBBoxReplacement(context.Background(), []byte(input), '\a'); err == nil {
			t.Fatal("accepted invalid control")
		}
	}
	input := strings.Replace(body, "WORD", "<![CDATA[A\aB]]>", 1)
	p, n, e := parseBBoxReplacement(context.Background(), []byte(input), '\a')
	if e != nil || n != 1 || p[0].Words[0].Text != "A�B" {
		t.Fatal("CDATA replacement failed")
	}
}

func TestBBoxReplacement(t *testing.T) {
	const body = `<html xmlns="http://www.w3.org/1999/xhtml"><head/><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">A` + "\a" + `B</word></line></block></flow></page></doc></body></html>`
	pages, count, err := parseBBoxReplacement(context.Background(), []byte(body), '\a')
	if err != nil || count != 1 || len(pages) != 1 || pages[0].Words[0].Text != "A�B" {
		t.Fatalf("replacement failed: %v %d %#v", err, count, pages)
	}
	if _, err := parseBBox([]byte(body)); err == nil {
		t.Fatal("ordinary decoder accepted control character")
	}
	for _, input := range []string{strings.Replace(body, "\a", "\x01", 1), strings.Replace(body, "\a", "\a\x01", 1)} {
		if _, _, err := parseBBoxReplacement(context.Background(), []byte(input), '\a'); err == nil {
			t.Fatal("accepted another invalid character")
		}
	}
	if _, _, err := parseBBoxReplacement(context.Background(), []byte(body), 'A'); err == nil {
		t.Fatal("accepted unsupported replacement")
	}
}
