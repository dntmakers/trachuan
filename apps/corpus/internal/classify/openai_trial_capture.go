package classify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type openAITrialCapture struct {
	Status   int                        `json:"status"`
	Complete bool                       `json:"complete"`
	BatchID  string                     `json:"batch_id,omitempty"`
	FileID   string                     `json:"file_id,omitempty"`
	Hash     string                     `json:"sha256"`
	Upload   *openAITrialUploadIdentity `json:"upload,omitempty"`
}

func (c openAITrialCapture) usable() bool {
	return c.Complete && c.Status >= 200 && c.Status < 300 && c.Status != 206
}

func (t *openAITrial) capture(name string, c openAITrialCapture, raw []byte) error {
	c.Hash = digest(raw)
	header, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("encode trial capture: %w", err)
	}
	if err = t.client.guard(header); err != nil {
		return err
	}
	if err = t.client.guard(raw); err != nil {
		return err
	}
	return writeTrialFile(t.path(name), func(w io.Writer) error {
		if _, err := w.Write(append(header, '\n')); err != nil {
			return err
		}
		_, err := w.Write(raw)
		return err
	})
}

func (t *openAITrial) readCapture(name string) (openAITrialCapture, []byte, bool, error) {
	c, raw, found, err := readTrialCapture(t.path(name))
	if err != nil || !found {
		return c, nil, found, err
	}
	if err := t.client.guard(raw); err != nil {
		return c, nil, false, err
	}
	return c, raw, true, nil
}

func readTrialCapture(path string) (openAITrialCapture, []byte, bool, error) {
	var c openAITrialCapture
	data, found, err := readTrialFile(path)
	if err != nil || !found {
		return c, nil, found, err
	}
	c, raw, err := decodeTrialCapture(data)
	return c, raw, err == nil, err
}

func decodeTrialCapture(data []byte) (openAITrialCapture, []byte, error) {
	var c openAITrialCapture
	header, raw, ok := bytes.Cut(data, []byte{'\n'})
	if !ok || len(header) > 4096 || len(raw) > openAITrialResultLimit || validateRetainedJSON(header, &c) != nil || json.Unmarshal(header, &c) != nil || c.Hash != digest(raw) {
		return c, nil, fmt.Errorf("invalid retained OpenAI trial capture")
	}
	return c, raw, nil
}

func (t *openAITrial) raw(name string, raw []byte) error {
	if err := t.client.guard(raw); err != nil {
		return err
	}
	return writeTrialFile(t.path(name), func(w io.Writer) error { _, err := w.Write(raw); return err })
}

func (t *openAITrial) writeJSON(name string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trial evidence: %w", err)
	}
	return t.raw(name, append(raw, '\n'))
}

func (c *OpenAITrialClient) guard(raw []byte) error {
	if len(c.credential) == 0 || containsTrialCredential(raw, c.credential, 0) {
		return fmt.Errorf("OpenAI trial evidence rejected because it contains credentials")
	}
	return nil
}

func containsTrialCredential(raw, credential []byte, depth int) bool {
	if bytes.Contains(raw, credential) {
		return true
	}
	if bytes.IndexByte(raw, '\\') < 0 {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		start, escaped := i, false
		for i++; i < len(raw) && raw[i] != '"'; i++ {
			if raw[i] == '\\' {
				escaped = true
				i++
			}
		}
		if !escaped || i >= len(raw) {
			continue
		}
		var decoded string
		if json.Unmarshal(raw[start:i+1], &decoded) == nil && (depth >= 8 || containsTrialCredential([]byte(decoded), credential, depth+1)) {
			return true
		}
	}
	return false
}

func validTrialCaptureName(name, operation string) bool {
	base, suffix := strings.CutSuffix(name, ".capture")
	attempt, prefix := strings.CutPrefix(base, operation+"-")
	return suffix && prefix && validID(attempt)
}
