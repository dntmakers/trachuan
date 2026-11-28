package pdftext

import (
	"runtime"
	"strings"
	"testing"
)

func TestBBox_ManyCDATAUsesLinearMemory(t *testing.T) {
	const start = `<html xmlns="http://www.w3.org/1999/xhtml"><head/><body><doc><page width="10" height="20"><flow><block><line><word xMin="1" yMin="2" xMax="3" yMax="4">`
	const end = `</word></line></block></flow></page></doc></body></html>`
	measure := func(n int) uint64 {
		data := []byte(start + strings.Repeat("<![CDATA[x]]>", n) + end)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		pages, err := parseBBox(data)
		runtime.ReadMemStats(&after)
		if err != nil || pages[0].Words[0].Text != strings.Repeat("x", n) {
			t.Fatal("invalid decoded word")
		}
		return after.TotalAlloc - before.TotalAlloc
	}
	small, large := measure(2000), measure(4000)
	if large > small*3 {
		t.Fatalf("CDATA allocation growth: %d %d", small, large)
	}
}
