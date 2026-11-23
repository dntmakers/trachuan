package classify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"danny.vn/trachuan/apps/corpus/internal/batchapi"
	"danny.vn/trachuan/apps/corpus/internal/openaibatch"
)

type openAISubmitClient interface {
	ProjectID() string
	AccountFingerprint() string
	Upload(context.Context, io.Reader) (openaibatch.Response, error)
	Create(context.Context, string, string) (openaibatch.Response, error)
}

type openAISubmitEvidence interface {
	Check(Submission) error
	Input(Submission, string, []string) error
	UploadResponse(Submission, openaibatch.Response) error
	UploadVerified(Submission, string, string) error
	Dispatch(Submission, string, string) error
	CreateResponse(Submission, openaibatch.Response) error
	Receipt(Submission, string) error
}

func openAISubmit(ctx context.Context, s Submission, bodies [][]byte, client openAISubmitClient, evidence openAISubmitEvidence) (string, error) {
	fail := func() (string, error) { return "", fmt.Errorf("OpenAI submission evidence unavailable") }
	if client == nil || evidence == nil || !validID(s.ID) || s.Provider != "openai" || s.Recipe.Provider != "openai" || !validID(s.AccountFingerprint) || client.ProjectID() != s.ProjectID || client.AccountFingerprint() != s.AccountFingerprint || validateRecovery(s, s.ID) != nil {
		return fail()
	}
	byKey := make(map[string][]byte, len(bodies))
	for _, body := range bodies {
		key := openAICountKey(body)
		if _, ok := byKey[key]; ok {
			return fail()
		}
		byKey[key] = body
	}
	var items []openaibatch.Item
	var ids []string
	for i, item := range s.Items {
		if item.Local {
			continue
		}
		body, ok := byKey[item.RequestHash]
		if !ok {
			return fail()
		}
		delete(byKey, item.RequestHash)
		id := customID(s.ID, i)
		ids = append(ids, id)
		items = append(items, openaibatch.Item{ID: id, Body: body})
	}
	if len(byKey) != 0 {
		return fail()
	}
	input, err := openaibatch.Encode(items)
	if err != nil {
		return fail()
	}
	hash := digest(input)
	if evidence.Check(s) != nil || evidence.Input(s, hash, ids) != nil {
		return fail()
	}
	upload, uploadErr := client.Upload(ctx, bytes.NewReader(input))
	if evidence.UploadResponse(s, upload) != nil {
		return fail()
	}
	if uploadErr != nil || upload.Status < 200 || upload.Status >= 300 || upload.Status == 206 {
		return fail()
	}
	fileID, err := openAIUploadID(upload.Body)
	if err != nil {
		return fail()
	}
	if evidence.UploadVerified(s, fileID, hash) != nil || evidence.Dispatch(s, fileID, s.ID) != nil {
		return fail()
	}
	create, createErr := client.Create(ctx, fileID, s.ID)
	if evidence.CreateResponse(s, create) != nil {
		return fail()
	}
	if createErr != nil || create.Status < 200 || create.Status >= 300 || create.Status == 206 {
		return fail()
	}
	id, err := openAICreateID(create.Body, fileID, s.ID)
	if err != nil {
		return fail()
	}
	if evidence.Receipt(s, id) != nil {
		return fail()
	}
	return id, nil
}

func openAIUploadID(raw []byte) (string, error) {
	fields, err := batchapi.DecodeEvidenceProjection(raw, "id", "object", "purpose", "error", "status")
	if err != nil || len(fields["error"]) > 0 && string(fields["error"]) != "null" {
		return "", fmt.Errorf("invalid upload evidence")
	}
	id, e := openAIEvidenceID(fields["id"])
	object, o := evidenceString(fields["object"])
	purpose, p := evidenceString(fields["purpose"])
	if e != nil || !openAIUsableID(id) || o != nil || object != "file" || p != nil || purpose != "batch" {
		return "", fmt.Errorf("invalid upload evidence")
	}
	if rawStatus, ok := fields["status"]; ok {
		status, err := evidenceString(rawStatus)
		if err != nil || (status != "uploaded" && status != "processed") {
			return "", fmt.Errorf("invalid upload evidence")
		}
	}
	return id, nil
}

func openAICreateID(raw []byte, fileID, submissionID string) (string, error) {
	fail := func() (string, error) { return "", fmt.Errorf("invalid create evidence") }
	fields, err := batchapi.DecodeEvidenceProjection(raw, "id", "object", "endpoint", "input_file_id", "metadata", "error", "request_counts")
	if err != nil || len(fields["error"]) > 0 && string(fields["error"]) != "null" {
		return fail()
	}
	id, e := openAIEvidenceID(fields["id"])
	object, o := evidenceString(fields["object"])
	endpoint, p := evidenceString(fields["endpoint"])
	input, i := openAIEvidenceID(fields["input_file_id"])
	metadata, m := batchapi.DecodeEvidenceProjection(fields["metadata"], "submission_id")
	sub, z := openAIEvidenceID(metadata["submission_id"])
	if e != nil || !openAIUsableID(id) || o != nil || object != "batch" || p != nil || endpoint != "/v1/responses" || i != nil || input != fileID || m != nil || z != nil || sub != submissionID {
		return fail()
	}
	if counts := fields["request_counts"]; len(counts) > 0 {
		c, err := batchapi.DecodeEvidenceProjection(counts, "total", "completed", "failed")
		if err != nil {
			return fail()
		}
		total, a := batchapi.TokenInteger(c["total"], 0)
		completed, b := batchapi.TokenInteger(c["completed"], 0)
		failed, d := batchapi.TokenInteger(c["failed"], 0)
		if !a || !b || !d || completed > total || failed > total-completed {
			return fail()
		}
	}
	return id, nil
}

func openAIUsableID(id string) bool {
	if id == "" || id == "." || id == ".." || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func openAIEvidenceID(raw []byte) (string, error) {
	if !utf8.Valid(raw) {
		return "", fmt.Errorf("invalid evidence identity")
	}
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw)-1 {
			return "", fmt.Errorf("invalid evidence identity")
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return "", fmt.Errorf("invalid evidence identity")
		}
		n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return "", fmt.Errorf("invalid evidence identity")
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return "", fmt.Errorf("invalid evidence identity")
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return "", fmt.Errorf("invalid evidence identity")
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return "", fmt.Errorf("invalid evidence identity")
			}
			i += 6
		}
	}
	return evidenceString(raw)
}
