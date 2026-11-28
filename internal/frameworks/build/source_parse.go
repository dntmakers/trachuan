package build

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
	"danny.vn/trachuan/internal/pdftext"
)

type PDFExtractor interface {
	Extract(context.Context, []byte, bool, bool) (parse.PDFInputs, error)
}

type LazyPDFRuntime struct {
	ctx       context.Context
	once      sync.Once
	extractor *pdftext.Extractor
	err       error
}

func NewLazyPDFRuntime(ctx context.Context) *LazyPDFRuntime { return &LazyPDFRuntime{ctx: ctx} }

func (r *LazyPDFRuntime) get() (*pdftext.Extractor, error) {
	r.once.Do(func() { r.extractor, r.err = pdftext.NewExtractor(r.ctx, pdftext.NewLimiter()) })
	return r.extractor, r.err
}

func (r *LazyPDFRuntime) RuntimeIdentity() string {
	e, err := r.get()
	if err != nil {
		return ""
	}
	return e.RuntimeIdentity()
}

func (r *LazyPDFRuntime) VerifyRuntime(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e, err := r.get()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if ctxErr := r.ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return pdftext.ErrConfiguration
	}
	return e.VerifyRuntime(ctx)
}

func (r *LazyPDFRuntime) Extract(ctx context.Context, data []byte, layout, bbox bool) (parse.PDFInputs, error) {
	if err := ctx.Err(); err != nil {
		return parse.PDFInputs{}, err
	}
	e, err := r.get()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return parse.PDFInputs{}, ctxErr
		}
		if ctxErr := r.ctx.Err(); ctxErr != nil {
			return parse.PDFInputs{}, ctxErr
		}
		return parse.PDFInputs{}, pdftext.ErrConfiguration
	}
	doc, cancel := e.Open(ctx, data)
	defer cancel()
	var inputs parse.PDFInputs
	if layout {
		inputs.Layout, err = doc.Layout()
		if err != nil {
			return inputs, err
		}
	}
	if bbox {
		inputs.BBox, err = doc.BBox()
	}
	return inputs, err
}

func ParseSource(ctx context.Context, v registry.Framework, data []byte, extractor PDFExtractor) (*parse.Result, error) {
	return ParseSourceWithTable(ctx, v, data, extractor, nil)
}

func ParseSourceWithTable(ctx context.Context, v registry.Framework, data []byte, extractor PDFExtractor, table parse.Table) (*parse.Result, error) {
	table = table.Clone()
	if err := table.Validate(); err != nil {
		return nil, err
	}
	entry, ok := table.Requirements(v.Parser)
	if !ok {
		return nil, fmt.Errorf("unknown parser")
	}
	file, err := v.SelectedSourceWithTable(table)
	if err != nil {
		return nil, hide("select source", err)
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != file.Size || fmt.Sprintf("%x", sum) != file.SHA256 {
		return nil, fmt.Errorf("source integrity mismatch")
	}
	if v.ExpectedControls == nil || *v.ExpectedControls <= 0 {
		return nil, fmt.Errorf("expected control count unset")
	}
	expected := *v.ExpectedControls
	callbackExpected := expected
	if !table.NeedsPDF(v.Parser) {
		result, parseErr := table.ParseCatalog(v.Parser, data, v.Code, v.Version, &callbackExpected)
		if parseErr != nil {
			return result, parseErr
		}
		return result, parse.ValidateCount(result, &expected)
	}
	if extractor == nil || (reflect.ValueOf(extractor).Kind() == reflect.Pointer && reflect.ValueOf(extractor).IsNil()) {
		return nil, fmt.Errorf("PDF extractor configuration error")
	}
	inputs, err := extractor.Extract(ctx, data, entry.Layout, entry.BBox)
	if errors.Is(err, pdftext.ErrConfiguration) {
		return nil, fmt.Errorf("PDF extractor configuration error")
	}
	if err != nil {
		return nil, hide("PDF extraction failed", err)
	}
	result, err := table.ParsePDF(v.Parser, inputs, fmt.Sprintf("%x", sum), &callbackExpected)
	if err != nil {
		return result, hide("parse source", err)
	}
	if err := parse.ValidateCount(result, &expected); err != nil {
		return result, err
	}
	return result, nil
}
