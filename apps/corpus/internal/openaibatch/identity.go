package openaibatch

import (
	"crypto/sha256"
	"encoding/hex"
)

func (c *Client) ProjectID() string { return c.project }
func (c *Client) AccountFingerprint() string {
	h := sha256.Sum256([]byte(c.key))
	return hex.EncodeToString(h[:16])
}
