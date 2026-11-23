package openaibatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

const maxRequests = 50000
const maxInputBytes = 200000000

type Item struct {
	ID   string
	Body []byte
}

func Encode(items []Item) ([]byte, error) {
	if len(items) == 0 || len(items) > maxRequests {
		return nil, errors.New("invalid request count")
	}
	var out bytes.Buffer
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		body := bytes.TrimSpace(item.Body)
		_, duplicate := seen[item.ID]
		if item.ID == "" || !utf8.ValidString(item.ID) || duplicate || !utf8.Valid(item.Body) || bytes.ContainsAny(item.Body, "\r\n") || len(body) == 0 || body[0] != '{' || !json.Valid(item.Body) {
			return nil, errors.New("invalid batch item")
		}
		seen[item.ID] = struct{}{}
		id, _ := json.Marshal(item.ID)
		size := len(id) + len(item.Body) + len(`{"custom_id":,"method":"POST","url":"/v1/responses","body":}`) + 1
		if size > maxInputBytes-out.Len() {
			return nil, errors.New("batch input too large")
		}
		out.WriteString(`{"custom_id":`)
		out.Write(id)
		out.WriteString(`,"method":"POST","url":"/v1/responses","body":`)
		out.Write(item.Body)
		out.WriteString("}\n")
	}
	return out.Bytes(), nil
}
