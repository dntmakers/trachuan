package pdftext

import "context"

func (d *Document) BBoxWithReplacement(r rune) ([]Page, int, error) {
	if d.invalid || r != '\a' {
		return nil, 0, ErrFile
	}
	output, err := d.extractor.runPDF(d.ctx, BBoxFlags(), d.data)
	if err != nil {
		return nil, 0, err
	}
	return parseBBoxReplacement(d.ctx, output, r)
}

func parseBBoxReplacement(ctx context.Context, data []byte, r rune) ([]Page, int, error) {
	if r != '\a' {
		return nil, 0, ErrFile
	}
	return parseBBoxWithReplacement(ctx, data)
}
