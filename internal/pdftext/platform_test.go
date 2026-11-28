package pdftext

import (
	"errors"
	"runtime"
	"testing"
)

func TestExtractor_RequiresLimiter(t *testing.T) {
	for _, limiter := range []*Limiter{nil, {}} {
		_, err := NewExtractor(limiter)
		if !errors.Is(err, ErrConfiguration) {
			t.Fatalf("invalid limiter accepted: %v", err)
		}
	}
}

func TestExtractor_UnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin refusal is exercised on other platforms")
	}
	_, err := NewExtractor(NewLimiter())
	if !errors.Is(err, ErrConfiguration) {
		t.Fatalf("expected configuration error: %v", err)
	}
}
