package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
)

func testDigest(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}
