package pdftext

import (
	"strings"
	"testing"
)

func TestParseBBox_RejectsExternalReferences(t *testing.T) {
	for _, data := range []string{
		`<?xml-stylesheet href="https://example.invalid/a"?><html xmlns="http://www.w3.org/1999/xhtml"><head/><body><doc><page width="10" height="20"/></doc></body></html>`,
		`<html xmlns="http://www.w3.org/1999/xhtml"><head/><body><doc><page width="10" height="20" href="file:///etc/passwd"/></doc></body></html>`,
		`<html xmlns="http://www.w3.org/1999/xhtml"><head/><body><doc><page width="10" height="20">&ext;</page></doc></body></html>`,
	} {
		if _, err := parseBBox([]byte(data)); err == nil {
			t.Fatalf("accepted external reference: %q", data)
		}
	}
}

func TestParseBBox_InvalidCoordinates(t *testing.T) {
	const body = `<html xmlns="http://www.w3.org/1999/xhtml"><head/><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">Hello</word></line></block></flow></page></doc></body></html>`
	for _, xml := range []string{
		strings.Replace(body, `width="10"`, `width="NaN"`, 1),
		strings.Replace(body, `xMin="1"`, `xMin="invalid"`, 1),
		strings.Replace(body, `xMax="3"`, `xMax="0"`, 1),
		strings.Replace(body, `yMin="2"`, "", 1),
	} {
		if _, err := parseBBox([]byte(xml)); err == nil {
			t.Fatalf("accepted invalid coordinates: %s", xml)
		}
	}
}

func TestParseBBox(t *testing.T) {
	const prefix = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">`
	const body = `<html xmlns="http://www.w3.org/1999/xhtml"><head/><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">Hello</word></line></block></flow></page></doc></body></html>`
	pages, err := parseBBox([]byte(prefix + body))
	if err != nil || len(pages) != 1 || len(pages[0].Words) != 1 || pages[0].Words[0].Text != "Hello" {
		t.Fatalf("unexpected pages: %#v, error: %v", pages, err)
	}
	escaped, err := parseBBox([]byte(prefix + strings.Replace(body, "Hello", "A&amp;B&#32;C", 1)))
	if err != nil || len(escaped) != 1 || len(escaped[0].Words) != 1 || escaped[0].Words[0].Text != "A&B C" {
		t.Fatalf("XML escapes rejected: %#v, %v", escaped, err)
	}
	for _, input := range []string{
		`<!DOCTYPE x [<!ENTITY e "secret">]><x>&e;</x>`,
		`<!DOCTYPE x SYSTEM "file:///etc/passwd"><x/>`,
		strings.Replace(prefix, "Transitional//EN", "Other//EN", 1) + body,
		strings.Replace(prefix, "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd", "file:///etc/passwd", 1) + body,
		strings.TrimSuffix(prefix, ">") + ` [<!ENTITY e "x">]>` + body,
		prefix + prefix + body,
		prefix + strings.Replace(body, "Hello", "&evil;", 1),
		prefix + strings.Replace(body, "Hello", "&#x01;", 1),
	} {
		if _, err := parseBBox([]byte(input)); err == nil {
			t.Fatalf("accepted invalid XML: %q", input)
		}
	}
}
