//go:build darwin

package pdftext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func pagePDF(texts ...string) []byte { return pagePDFSubject("", texts...) }

func pagePDFSubject(subject string, texts ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	object := func(n int, body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n, body)
	}
	var kids strings.Builder
	for i := range texts {
		fmt.Fprintf(&kids, "%d 0 R ", 4+i*2)
	}
	object(1, "<< /Type /Catalog /Pages 2 0 R >>")
	object(2, fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(texts), kids.String()))
	object(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	for i, text := range texts {
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
		object(4+i*2, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+i*2))
		object(5+i*2, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}
	info := ""
	if subject != "" {
		n := 4 + len(texts)*2
		object(n, fmt.Sprintf("<< /Subject (%s) >>", subject))
		info = fmt.Sprintf(" /Info %d 0 R", n)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R%s >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), info, start)
	return b.Bytes()
}

func cmapPDF() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	stream := "BT /F1 12 Tf 72 720 Td <93FA> Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Count 1 /Kids [3 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type0 /BaseFont /HeiseiMin-W3 /Encoding /90ms-RKSJ-H /DescendantFonts [6 0 R] >>",
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /HeiseiMin-W3 /CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >> >>",
	}
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), start)
	return b.Bytes()
}

func TestLayout_UsesBundledCMap(t *testing.T) {
	e, err := NewExtractor(context.Background(), NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	doc, cancel := e.Open(context.Background(), cmapPDF())
	defer cancel()
	pages, err := doc.Layout()
	if err != nil || len(pages) != 1 || !strings.Contains(pages[0], "日") {
		t.Fatalf("CMap extraction failed: %v", err)
	}
}

func TestLayout_PagesAndDeterminism(t *testing.T) {
	e, err := NewExtractor(context.Background(), NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	pdf := pagePDF("First", "", "Third")
	doc, cancel := e.Open(context.Background(), pdf)
	defer cancel()
	pages, err := doc.Layout()
	if err != nil || len(pages) != 3 || !strings.Contains(pages[0], "First") || strings.TrimSpace(pages[1]) != "" || !strings.Contains(pages[2], "Third") {
		t.Fatalf("invalid pages %#v, err %v", pages, err)
	}
	again, err := doc.Layout()
	if err != nil || strings.Join(pages, "\f") != strings.Join(again, "\f") {
		t.Fatalf("not deterministic: %v", err)
	}
}

func TestBBox_MetadataFromPDF(t *testing.T) {
	e, err := NewExtractor(context.Background(), NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	doc, cancel := e.Open(context.Background(), pagePDFSubject("100% complete", "Hello"))
	defer cancel()
	pages, err := doc.BBox()
	if err != nil || len(pages) != 1 || len(pages[0].Words) == 0 || pages[0].Words[0].Text != "Hello" {
		t.Fatalf("metadata PDF failed: %v", err)
	}
}

func TestBBox_PagesAndCoordinates(t *testing.T) {
	e, err := NewExtractor(context.Background(), NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	pdf := pagePDF("First", "", "Third")
	doc, cancel := e.Open(context.Background(), pdf)
	defer cancel()
	pages, err := doc.BBox()
	if err != nil || len(pages) != 3 || len(pages[0].Words) == 0 || pages[0].Words[0].Text != "First" || len(pages[1].Words) != 0 || pages[0].Words[0].XMin == "" {
		t.Fatalf("invalid pages %#v, err %v", pages, err)
	}
	again, err := doc.BBox()
	a, _ := json.Marshal(pages)
	b, _ := json.Marshal(again)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatalf("not deterministic: %v", err)
	}
}

func TestLayout_FailureHidesInput(t *testing.T) {
	e, err := NewExtractor(context.Background(), NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	doc, cancel := e.Open(context.Background(), []byte("publisher-sentinel"))
	defer cancel()
	_, err = doc.Layout()
	if !errors.Is(err, ErrFile) || strings.Contains(err.Error(), "publisher-sentinel") {
		t.Fatalf("unexpected error %v", err)
	}
	_, err = doc.BBox()
	if !errors.Is(err, ErrFile) || strings.Contains(err.Error(), "publisher-sentinel") {
		t.Fatalf("unexpected bbox error %v", err)
	}
}

func TestDocument_SharedDeadline(t *testing.T) {
	e, err := NewExtractor(context.Background(), NewLimiter())
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	doc, cancel := e.open(context.Background(), pagePDF("First"), time.Hour)
	after := time.Now()
	defer cancel()
	deadline, ok := doc.ctx.Deadline()
	if !ok || deadline.Before(before.Add(time.Hour)) || deadline.After(after.Add(time.Hour)) {
		t.Fatalf("configured document deadline: %v, %v", deadline, ok)
	}
	if _, err := doc.Layout(); err != nil {
		t.Fatal(err)
	}
	if got, ok := doc.ctx.Deadline(); !ok || !got.Equal(deadline) {
		t.Fatalf("document deadline: %v, %v; want %v", got, ok, deadline)
	}
	cancel()
	if _, err := doc.BBox(); !errors.Is(err, ErrFile) {
		t.Fatalf("cancelled document: %v", err)
	}
}

func TestDocument_CancelBeforeStart(t *testing.T) {
	e := &Extractor{path: "/nonexistent", runtime: testRuntime(), limiter: NewLimiter()}
	doc, cancel := e.Open(context.Background(), pagePDF("First"))
	cancel()
	if _, err := doc.Layout(); !errors.Is(err, ErrFile) {
		t.Fatalf("cancelled document: %v", err)
	}
}

func TestSampleRSS_CurrentProcess(t *testing.T) {
	rss, err := sampleRSS(context.Background(), os.Getpid())
	if err != nil || rss == 0 {
		t.Fatalf("RSS sample: %d, %v", rss, err)
	}
}

func TestWatchdog_SamplerFailure(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start(context.Background(), []string{"-c", `while :; do :; done`}, nil, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	p.sample = func(context.Context, int) (uint64, error) { return 0, errors.New("unavailable") }
	_, _, err = p.wait(context.Background())
	if !errors.Is(err, ErrConfiguration) {
		t.Fatalf("sampler failure: %v", err)
	}
	if err = syscall.Kill(-p.cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group remains: %v", err)
	}
}

func TestWatchdog_ExceedsMemory(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start(context.Background(), []string{"-c", `while :; do :; done`}, nil, 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	p.sample = func(context.Context, int) (uint64, error) { return 1<<30 + 1, nil }
	_, _, err = p.wait(context.Background())
	if !errors.Is(err, ErrMemoryLimit) {
		t.Fatalf("memory limit: %v", err)
	}
	if err = syscall.Kill(-p.cmd.Process.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("process group remains: %v", err)
	}
}

func TestWatchdog_ContinuesAfterSample(t *testing.T) {
	e := &Extractor{path: "/bin/bash", runtime: testRuntime(), limiter: NewLimiter()}
	p, err := e.start(context.Background(), []string{"-c", `while :; do :; done`}, []byte("input"), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	p.sample = func(context.Context, int) (uint64, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return 0, nil
	}
	_, _, err = p.wait(ctx)
	if !errors.Is(err, ErrFile) || calls != 2 {
		t.Fatalf("sample calls: %d, error: %v", calls, err)
	}
}
