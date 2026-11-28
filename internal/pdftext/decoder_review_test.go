package pdftext

import (
	"context"
	"strings"
	"testing"
	"time"
)

const validXML = `<html xmlns="http://www.w3.org/1999/xhtml"><head><meta name="Subject" content="100% complete"/></head><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">word</word></line></block></flow></page></doc></body></html>`

func TestBBox_MetadataPlainText(t *testing.T) {
	pages, err := parseBBox([]byte(validXML))
	if err != nil || len(pages) != 1 || len(pages[0].Words) != 1 {
		t.Fatalf("metadata rejected: %v", err)
	}
}

func TestBBox_RejectsUnexpectedStructure(t *testing.T) {
	base := strings.Replace(validXML, "100% complete", "Complete", 1)
	bad := []string{
		strings.Replace(base, `<flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">word</word></line></block></flow>`, `<word xMin="1" yMin="2" xMax="3" yMax="4">word</word>`, 1),
		strings.Replace(base, `<head><meta name="Subject" content="Complete"/></head>`, "", 1),
		strings.Replace(base, `http://www.w3.org/1999/xhtml`, `https://example.invalid/other`, 1),
		strings.Replace(strings.Replace(base, `<html `, `<other `, 1), `</html>`, `</other>`, 1),
		strings.Replace(strings.Replace(base, "<flow>", "<flow>"+strings.Repeat("<x>", 65), 1), "</flow>", strings.Repeat("</x>", 65)+"</flow>", 1),
	}
	for _, data := range bad {
		if _, err := parseBBox([]byte(data)); err == nil {
			t.Fatal("accepted invalid structure")
		}
	}
}

type cancelAfterChecks struct{ calls int }

func (*cancelAfterChecks) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAfterChecks) Done() <-chan struct{}       { return nil }
func (c *cancelAfterChecks) Err() error {
	c.calls++
	if c.calls >= 8 {
		return context.Canceled
	}
	return nil
}
func (*cancelAfterChecks) Value(any) any { return nil }

func TestBBox_CancellationDuringDecode(t *testing.T) {
	ctx := &cancelAfterChecks{}
	_, err := parseBBoxContext(ctx, []byte(validXML))
	if err == nil || ctx.calls < 8 {
		t.Fatalf("decode ignored cancellation: %v", err)
	}
}
