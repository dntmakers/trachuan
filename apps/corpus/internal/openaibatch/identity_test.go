package openaibatch

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestClientIdentity(t *testing.T) {
	c := NewClient("synthetic-key", "proj-test", nil)
	h := sha256.Sum256([]byte("synthetic-key"))
	if c.ProjectID() != "proj-test" || c.AccountFingerprint() != hex.EncodeToString(h[:16]) {
		t.Fatal("identity differs")
	}
}
