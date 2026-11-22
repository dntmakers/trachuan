package ocrstore

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestVerifyStoredCheckCanonical(t *testing.T) {
	for _, tc := range []struct{ canonical, json string }{
		{`{"same_document":true,"pages":1,"pdf_pages":1,"items":[],"verdict":"clean","dropped_equal":0}`, `{"verdict":"clean","items":[],"dropped_equal":0,"pdf_pages":1,"pages":1,"same_document":true}`},
		{`{"same_document":true,"pages":1,"pdf_pages":1,"items":[],"verdict":"clean"}`, `{"verdict":"clean","items":[],"pdf_pages":1,"pages":1,"same_document":true}`},
	} {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.canonical)))
		if err := VerifyStoredCheck([]byte(tc.json), hash, 0); err != nil {
			t.Fatalf("valid: %v", err)
		}
		if err := VerifyStoredCheck([]byte(tc.json), fmt.Sprintf("%064d", 0), 0); err == nil {
			t.Fatal("accepted wrong hash")
		}
		if err := VerifyStoredCheck([]byte(tc.json), hash, 1); err == nil {
			t.Fatal("accepted wrong count")
		}
	}
}
