package openaibatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"
)

const maxControlBytes = 4 << 20

type Client struct {
	key, project string
	transport    http.RoundTripper
}
type Response struct {
	Status int
	Body   []byte
}

func NewClient(key, project string, transport http.RoundTripper) *Client {
	if transport == nil {
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		transport = &http.Transport{DisableKeepAlives: true, DisableCompression: true, Protocols: protocols}
	}
	return &Client{key: key, project: project, transport: transport}
}
func (c *Client) request(ctx context.Context, method, path, query, contentType string, body io.Reader) (*http.Response, error) {
	if strings.TrimSpace(c.key) == "" || strings.TrimSpace(c.project) == "" {
		return nil, errors.New("invalid provider credentials")
	}
	if ctx.Err() != nil {
		return nil, errors.New("provider request cancelled")
	}
	u := "https://api.openai.com" + path
	if query != "" {
		u += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, errors.New("invalid provider request")
	}
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("OpenAI-Project", c.project)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.transport.RoundTrip(req)
	if req.Body != nil {
		_ = req.Body.Close()
	}
	if err != nil {
		return resp, errors.New("provider request failed")
	}
	if resp == nil || resp.Body == nil {
		return resp, errors.New("invalid provider response")
	}
	return resp, nil
}
func (c *Client) control(ctx context.Context, method, path, query, contentType string, body io.Reader) (Response, error) {
	resp, err := c.request(ctx, method, path, query, contentType, body)
	if resp == nil {
		return Response{}, err
	}
	result, captureErr := capture(resp)
	if captureErr != nil {
		return result, captureErr
	}
	if err != nil {
		return result, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.StatusCode == 206 || resp.Header.Get("Content-Range") != "" {
		return result, errors.New("provider request returned unusable status")
	}
	if !validFraming(resp) || !validLength(resp, int64(len(result.Body))) {
		return result, errors.New("provider response framing invalid")
	}
	return result, nil
}
func (c *Client) Count(ctx context.Context, body []byte) (Response, error) {
	return c.control(ctx, "POST", "/v1/responses/input_tokens", "", "application/json", bytes.NewReader(body))
}
func (c *Client) Create(ctx context.Context, fileID, submissionID string) (Response, error) {
	if strings.TrimSpace(fileID) == "" || strings.TrimSpace(submissionID) == "" || !utf8.ValidString(fileID) || !utf8.ValidString(submissionID) {
		return Response{}, errors.New("invalid batch identifiers")
	}
	body, err := json.Marshal(struct {
		Input    string            `json:"input_file_id"`
		Endpoint string            `json:"endpoint"`
		Window   string            `json:"completion_window"`
		Metadata map[string]string `json:"metadata"`
	}{fileID, "/v1/responses", "24h", map[string]string{"submission_id": submissionID}})
	if err != nil {
		return Response{}, errors.New("invalid batch create")
	}
	return c.control(ctx, "POST", "/v1/batches", "", "application/json", bytes.NewReader(body))
}
func (c *Client) Retrieve(ctx context.Context, id string) (Response, error) {
	if id == "" || id == "." || id == ".." {
		return Response{}, errors.New("invalid provider ID")
	}
	return c.control(ctx, "GET", "/v1/batches/"+url.PathEscape(id), "", "", nil)
}
func (c *Client) List(ctx context.Context, after string) (Response, error) {
	q := url.Values{}
	if after != "" {
		q.Set("after", after)
	}
	return c.control(ctx, "GET", "/v1/batches", q.Encode(), "", nil)
}
func (c *Client) Cancel(ctx context.Context, id string) (Response, error) {
	if id == "" || id == "." || id == ".." {
		return Response{}, errors.New("invalid provider ID")
	}
	return c.control(ctx, "POST", "/v1/batches/"+url.PathEscape(id)+"/cancel", "", "", nil)
}

type ContentResponse struct {
	Status int
	Body   []byte
	Stream io.ReadCloser
}

func (c *Client) Content(ctx context.Context, id string) (ContentResponse, error) {
	if id == "" || id == "." || id == ".." {
		return ContentResponse{}, errors.New("invalid provider ID")
	}
	resp, err := c.request(ctx, "GET", "/v1/files/"+url.PathEscape(id)+"/content", "", "", nil)
	if resp == nil {
		return ContentResponse{}, err
	}
	if err != nil || resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Range") != "" || !validFraming(resp) {
		result, captureErr := capture(resp)
		out := ContentResponse{Status: result.Status, Body: result.Body}
		if captureErr != nil {
			return out, captureErr
		}
		if err != nil {
			return out, err
		}
		return out, errors.New("provider content response unusable")
	}
	return ContentResponse{Status: resp.StatusCode, Stream: &safeStream{ctx: ctx, body: resp.Body, length: streamLength(resp)}}, nil
}
func (c *Client) Upload(ctx context.Context, input io.Reader) (Response, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("purpose", "batch"); err != nil {
		return Response{}, errors.New("cannot encode upload")
	}
	f, err := w.CreateFormFile("file", "batch.jsonl")
	if err != nil {
		return Response{}, errors.New("cannot encode upload")
	}
	n, err := io.Copy(f, io.LimitReader(&contextReader{ctx: ctx, reader: input}, maxInputBytes+1))
	if err != nil {
		return Response{}, errors.New("cannot read upload")
	}
	if n > maxInputBytes {
		return Response{}, errors.New("batch input too large")
	}
	if err = w.Close(); err != nil {
		return Response{}, errors.New("cannot encode upload")
	}
	return c.control(ctx, "POST", "/v1/files", "", w.FormDataContentType(), &buf)
}
