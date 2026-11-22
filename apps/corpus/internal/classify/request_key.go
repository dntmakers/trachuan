package classify

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
)

const keyVersion = 1

func requestKey(request *batchapi.Request) (string, error) {
	body, err := request.Encode()
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte(batchapi.Provider + "\x00" + batchapi.Endpoint + "\x00"))
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func validKey(key string) bool {
	b, err := hex.DecodeString(key)
	return err == nil && len(b) == sha256.Size && strings.ToLower(key) == key
}
