package openaibatch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
)

func capture(resp *http.Response) (Response, error) {
	result := Response{Status: resp.StatusCode}
	if resp.Body == nil {
		return result, errors.New("invalid provider response")
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxControlBytes+1))
	result.Body = data
	if len(data) > maxControlBytes {
		result.Body = data[:maxControlBytes]
		return result, errors.New("provider response too large")
	}
	if err != nil {
		return result, errors.New("provider response read failed")
	}
	return result, nil
}

func validFraming(resp *http.Response) bool {
	if resp.Uncompressed {
		return false
	}
	for _, coding := range resp.Header.Values("Content-Encoding") {
		if !strings.EqualFold(strings.TrimSpace(coding), "identity") {
			return false
		}
	}
	if len(resp.TransferEncoding) != 0 {
		return len(resp.TransferEncoding) == 1 && strings.EqualFold(resp.TransferEncoding[0], "chunked")
	}
	return resp.ContentLength >= 0
}
func streamLength(resp *http.Response) int64 {
	if len(resp.TransferEncoding) != 0 {
		return -1
	}
	return resp.ContentLength
}
func validLength(resp *http.Response, length int64) bool {
	expected := streamLength(resp)
	return expected < 0 || expected == length
}

type safeStream struct {
	ctx    context.Context
	body   io.ReadCloser
	length int64
	read   int64
}

func (s *safeStream) Read(p []byte) (int, error) {
	if s.ctx.Err() != nil {
		return 0, errors.New("provider content read cancelled")
	}
	n, err := s.body.Read(p)
	s.read += int64(n)
	if s.length >= 0 && (s.read > s.length || err == io.EOF && s.read != s.length) {
		return n, errors.New("provider content length mismatch")
	}
	if err != nil && err != io.EOF {
		return n, errors.New("provider content read failed")
	}
	return n, err
}
func (s *safeStream) Close() error {
	if err := s.body.Close(); err != nil {
		return errors.New("provider content close failed")
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if r.ctx.Err() != nil {
		return 0, errors.New("upload cancelled")
	}
	return r.reader.Read(p)
}
