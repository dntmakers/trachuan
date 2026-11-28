package pdftext

import (
	"bytes"
	"context"
	"strings"
	"time"
)

type Document struct {
	ctx       context.Context
	extractor *Extractor
	data      []byte
	invalid   bool
}

func LayoutFlags() []string { return []string{"-layout", "-enc", "UTF-8", "-eol", "unix", "-", "-"} }
func BBoxFlags() []string   { return []string{"-bbox-layout", "-enc", "UTF-8", "-eol", "unix", "-", "-"} }

func (e *Extractor) Open(ctx context.Context, data []byte) (*Document, context.CancelFunc) {
	return e.open(ctx, data, 5*time.Minute)
}

func (e *Extractor) open(ctx context.Context, data []byte, timeout time.Duration) (*Document, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	doc := &Document{ctx: ctx, extractor: e}
	if len(data) > maxInput {
		doc.invalid = true
	} else {
		doc.data = bytes.Clone(data)
	}
	return doc, cancel
}

func (d *Document) Layout() ([]string, error) {
	if d.invalid {
		return nil, ErrFile
	}
	output, err := d.extractor.runPDF(d.ctx, LayoutFlags(), d.data)
	if err != nil {
		return nil, err
	}
	if len(output) == 0 || output[len(output)-1] != '\f' {
		return nil, ErrFile
	}
	pages := strings.Split(string(output[:len(output)-1]), "\f")
	if d.ctx.Err() != nil {
		return nil, ErrFile
	}
	return pages, nil
}

func (d *Document) BBox() ([]Page, error) {
	if d.invalid {
		return nil, ErrFile
	}
	output, err := d.extractor.runPDF(d.ctx, BBoxFlags(), d.data)
	if err != nil {
		return nil, err
	}
	return parseBBoxContext(d.ctx, output)
}
