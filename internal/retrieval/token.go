package retrieval

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// ReadItem describes one document, provision, range, relation list or continuation.
type ReadItem struct {
	Handle    string `json:"handle,omitempty"`
	Number    string `json:"number,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Relations bool   `json:"relations,omitempty"`
	Timeline  *bool  `json:"timeline,omitempty"`
	Next      string `json:"next,omitempty"`
	fields    uint8
}

func readItemField(name string) uint8 {
	switch name {
	case "handle":
		return 1
	case "number":
		return 2
	case "from":
		return 4
	case "to":
		return 8
	case "relations":
		return 16
	case "timeline":
		return 32
	case "next":
		return 64
	}
	return 0
}

func (item *ReadItem) UnmarshalJSON(data []byte) error {
	type plain ReadItem
	dec := json.NewDecoder(bytes.NewReader(data))
	start, err := dec.Token()
	if err != nil || start != json.Delim('{') {
		return errInvalidNext
	}
	var present uint8 = 128
	var zero bool
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return errInvalidNext
		}
		name, ok := key.(string)
		if !ok {
			return errInvalidNext
		}
		bit := readItemField(name)
		var raw json.RawMessage
		if bit == 0 || present&bit != 0 || dec.Decode(&raw) != nil || bytes.Equal(raw, []byte("null")) {
			return errInvalidNext
		}
		zero = zero || bytes.Equal(raw, []byte(`""`)) || bytes.Equal(raw, []byte("false"))
		present |= bit
	}
	if _, err := dec.Token(); err != nil {
		return errInvalidNext
	}
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*item = ReadItem(value)
	if zero {
		item.fields = present
	}
	return nil
}

func (item ReadItem) has(name string, fallback bool) bool {
	if item.fields != 0 {
		return item.fields&readItemField(name) != 0
	}
	return fallback
}

type cursor struct {
	V             int
	Release       string
	Item          ReadItem
	Unit          int
	Part          int
	PageCap       int  `json:",omitempty"`
	PartSize      int  `json:",omitempty"`
	WholeDocument bool `json:",omitempty"`
}

var errInvalidNext = errors.New("invalid next token")

func encodeNext(c cursor) string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeNext(s string) (cursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || !utf8.Valid(data) {
		return cursor{}, errInvalidNext
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c cursor
	if err := dec.Decode(&c); err != nil {
		return cursor{}, errInvalidNext
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return cursor{}, errInvalidNext
	}
	if c.PageCap < 0 || c.PartSize < 0 || c.partSize() > c.pageCap() || c.V != 1 || c.Unit < 0 || c.Part < 0 || c.Item.Next != "" || (c.Item.Handle != "" && c.Item.Number != "") {
		return cursor{}, errInvalidNext
	}
	return c, nil
}
