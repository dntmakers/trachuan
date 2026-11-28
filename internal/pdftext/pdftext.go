package pdftext

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
)

var ErrFile = errors.New("pdf extraction failed")

type Page struct {
	Width  string `xml:"width,attr"`
	Height string `xml:"height,attr"`
	Words  []Word `xml:"flow>block>line>word"`
}

type Word struct {
	XMin string `xml:"xMin,attr"`
	YMin string `xml:"yMin,attr"`
	XMax string `xml:"xMax,attr"`
	YMax string `xml:"yMax,attr"`
	Text string `xml:",chardata"`
}

type contextReader struct {
	ctx    context.Context
	reader *bytes.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func parseBBox(data []byte) ([]Page, error) {
	return parseBBoxContext(context.Background(), data)
}

func parseBBoxContext(ctx context.Context, data []byte) ([]Page, error) {
	clean, _, err := scanBBox(ctx, data, false)
	if err != nil {
		return nil, ErrFile
	}
	return parseBBoxDecode(ctx, clean)
}

func parseBBoxWithReplacement(ctx context.Context, data []byte) ([]Page, int, error) {
	clean, count, err := scanBBox(ctx, data, true)
	if err != nil {
		return nil, 0, ErrFile
	}
	pages, err := parseBBoxDecode(ctx, clean)
	if err != nil {
		return nil, 0, err
	}
	return pages, count, nil
}

func parseBBoxDecode(ctx context.Context, data []byte) ([]Page, error) {
	const prefix = `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">`
	data = bytes.TrimPrefix(data, []byte(prefix))
	decoder := xml.NewDecoder(&contextReader{ctx: ctx, reader: bytes.NewReader(data)})
	var pages []Page
	var stack []string
	var word Word
	var wordText strings.Builder
	root, head, body, doc := false, false, false, false
	for {
		if ctx.Err() != nil {
			return nil, ErrFile
		}
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrFile
		}
		switch value := token.(type) {
		case xml.Directive, xml.ProcInst:
			return nil, ErrFile
		case xml.StartElement:
			if len(stack) >= 64 || value.Name.Space != "http://www.w3.org/1999/xhtml" {
				return nil, ErrFile
			}
			name := value.Name.Local
			parent := ""
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			if !allowedChild(parent, name) || !validAttributes(name, value.Attr) {
				return nil, ErrFile
			}
			switch name {
			case "html":
				if root {
					return nil, ErrFile
				}
				root = true
			case "head":
				if head || body {
					return nil, ErrFile
				}
				head = true
			case "body":
				if !head || body {
					return nil, ErrFile
				}
				body = true
			case "doc":
				if doc {
					return nil, ErrFile
				}
				doc = true
			case "page":
				page := Page{Width: attrValue(value.Attr, "width"), Height: attrValue(value.Attr, "height")}
				w, okW := coordinate(page.Width)
				h, okH := coordinate(page.Height)
				if !okW || !okH || w <= 0 || h <= 0 {
					return nil, ErrFile
				}
				pages = append(pages, page)
			case "word":
				wordText.Reset()
				word = Word{XMin: attrValue(value.Attr, "xMin"), YMin: attrValue(value.Attr, "yMin"), XMax: attrValue(value.Attr, "xMax"), YMax: attrValue(value.Attr, "yMax")}
				xMin, ok1 := coordinate(word.XMin)
				yMin, ok2 := coordinate(word.YMin)
				xMax, ok3 := coordinate(word.XMax)
				yMax, ok4 := coordinate(word.YMax)
				if !ok1 || !ok2 || !ok3 || !ok4 || xMin > xMax || yMin > yMax {
					return nil, ErrFile
				}
			}
			stack = append(stack, name)
		case xml.EndElement:
			if len(stack) == 0 || value.Name.Space != "http://www.w3.org/1999/xhtml" || stack[len(stack)-1] != value.Name.Local {
				return nil, ErrFile
			}
			if value.Name.Local == "word" {
				word.Text = wordText.String()
				pages[len(pages)-1].Words = append(pages[len(pages)-1].Words, word)
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) == 0 {
				if strings.TrimSpace(string(value)) != "" {
					return nil, ErrFile
				}
				continue
			}
			switch stack[len(stack)-1] {
			case "word":
				_, _ = wordText.Write(value)
			case "title":
			default:
				if strings.TrimSpace(string(value)) != "" {
					return nil, ErrFile
				}
			}
		}
	}
	if ctx.Err() != nil || len(stack) != 0 || !root || !body || !doc || len(pages) == 0 {
		return nil, ErrFile
	}
	return pages, nil
}

func allowedChild(parent, name string) bool {
	switch parent {
	case "":
		return name == "html"
	case "html":
		return name == "head" || name == "body"
	case "head":
		return name == "title" || name == "meta"
	case "body":
		return name == "doc"
	case "doc":
		return name == "page"
	case "page":
		return name == "flow"
	case "flow":
		return name == "block"
	case "block":
		return name == "line"
	case "line":
		return name == "word"
	default:
		return false
	}
}

func validAttributes(name string, attrs []xml.Attr) bool {
	seen := make(map[string]bool, len(attrs))
	for _, attr := range attrs {
		key := attr.Name.Space + "\x00" + attr.Name.Local
		if seen[key] {
			return false
		}
		seen[key] = true
		if name == "html" && attr.Name.Space == "" && attr.Name.Local == "xmlns" && attr.Value == "http://www.w3.org/1999/xhtml" {
			continue
		}
		if attr.Name.Space != "" {
			return false
		}
		switch name {
		case "meta":
			if attr.Name.Local != "name" && attr.Name.Local != "content" {
				return false
			}
		case "page":
			if attr.Name.Local != "width" && attr.Name.Local != "height" {
				return false
			}
		case "flow", "block", "line", "word":
			switch attr.Name.Local {
			case "xMin", "yMin", "xMax", "yMax":
			default:
				return false
			}
		default:
			return false
		}
	}
	return true
}

func attrValue(attrs []xml.Attr, name string) string {
	for _, attr := range attrs {
		if attr.Name.Space == "" && attr.Name.Local == name {
			return attr.Value
		}
	}
	return ""
}

func coordinate(value string) (float64, bool) {
	n, err := strconv.ParseFloat(value, 64)
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
