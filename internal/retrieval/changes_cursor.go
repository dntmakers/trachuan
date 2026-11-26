package retrieval

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
)

type changesCursor struct {
	V           int    `json:"v"`
	Release     string `json:"release"`
	LastOrdinal int64  `json:"last_ordinal"`
	FirstLimit  int    `json:"first_limit"`
	Digest      string `json:"digest"`
}

func changesDigest(req ChangesRequest) string {
	raw, _ := json.Marshal(struct {
		Country  string   `json:"country"`
		Kinds    []string `json:"kinds"`
		Document string   `json:"document"`
	}{req.Country, req.Kinds, req.Document})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func encodeChangesCursor(c changesCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeChangesCursor(token string) (changesCursor, error) {
	var c changesCursor
	if len(token) > 512 {
		return c, ErrInvalidRequest
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || closedChangesObject(raw, "v", "release", "last_ordinal", "first_limit", "digest") != nil {
		return c, ErrInvalidRequest
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 5 || json.Unmarshal(raw, &c) != nil {
		return changesCursor{}, ErrInvalidRequest
	}
	digest, err := hex.DecodeString(c.Digest)
	if err != nil || len(digest) != sha256.Size || c.Digest != hex.EncodeToString(digest) || c.V != 1 || !validChangesRelease(c.Release) || c.LastOrdinal < 1 || c.FirstLimit < 1 || c.FirstLimit > 50 {
		return changesCursor{}, ErrInvalidRequest
	}
	return c, nil
}
