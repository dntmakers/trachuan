package openaibatch

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := f(r)
	if resp != nil && resp.ContentLength == 0 && len(resp.TransferEncoding) == 0 {
		resp.TransferEncoding = []string{"chunked"}
	}
	return resp, err
}
func TestEncode_PreservesBodyAndID(t *testing.T) {
	b, err := Encode([]Item{{ID: "a\"b", Body: []byte(` { "x": 1 } `)}})
	if err != nil || string(b) != "{\"custom_id\":\"a\\\"b\",\"method\":\"POST\",\"url\":\"/v1/responses\",\"body\": { \"x\": 1 } }\n" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err = Encode([]Item{{ID: "x", Body: []byte(`{`)}}); err == nil {
		t.Fatal("invalid body accepted")
	}
}
func TestClient_Requests(t *testing.T) {
	calls := 0
	c := NewClient("secret", "proj", roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "api.openai.com" || r.Header.Get("Authorization") != "Bearer secret" || r.Header.Get("OpenAI-Project") != "proj" {
			t.Fatal("wrong destination or headers")
		}
		switch calls {
		case 1:
			if r.URL.Path != "/v1/responses/input_tokens" || r.Method != "POST" {
				t.Fatal(r.URL)
			}
		case 2:
			if r.URL.EscapedPath() != "/v1/batches/a%2Fb" {
				t.Fatal(r.URL)
			}
		case 3:
			if r.URL.Query().Get("after") != "a&b" {
				t.Fatal(r.URL)
			}
		case 4:
			if r.URL.Path != "/v1/batches" {
				t.Fatal(r.URL)
			}
			b, _ := io.ReadAll(r.Body)
			if string(b) != `{"input_file_id":"file","endpoint":"/v1/responses","completion_window":"24h","metadata":{"submission_id":"sub"}}` {
				t.Fatal(string(b))
			}
		case 5:
			if err := r.ParseMultipartForm(1024); err != nil {
				t.Fatal(err)
			}
			if r.FormValue("purpose") != "batch" {
				t.Fatal(r.Form)
			}
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			b, _ := io.ReadAll(f)
			if string(b) != "line\n" {
				t.Fatal(string(b))
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("bad json")), Header: make(http.Header)}, nil
	}))
	ctx := context.Background()
	if r, e := c.Count(ctx, []byte(`{}`)); e != nil || string(r.Body) != "bad json" {
		t.Fatal(r, e)
	}
	if _, e := c.Retrieve(ctx, "a/b"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.List(ctx, "a&b"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Create(ctx, "file", "sub"); e != nil {
		t.Fatal(e)
	}
	if _, e := c.Upload(ctx, strings.NewReader("line\n")); e != nil {
		t.Fatal(e)
	}
}

type badReader struct{}

func (badReader) Read(p []byte) (int, error) { copy(p, "part"); return 4, errors.New("secret") }
func TestClient_RetainsPartialAndRedactsErrors(t *testing.T) {
	c := NewClient("secret", "project", roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(badReader{}), Header: make(http.Header)}, nil
	}))
	r, err := c.Count(context.Background(), []byte(`{}`))
	if err == nil || strings.Contains(err.Error(), "secret") || r.Status != 503 || string(r.Body) != "part" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestClient_RejectsDotAndEmptyIDs(t *testing.T) {
	c := NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("invalid ID reached transport"); return nil, nil }))
	for _, id := range []string{"", ".", ".."} {
		if _, err := c.Retrieve(context.Background(), id); err == nil {
			t.Fatalf("accepted %q", id)
		}
		if _, err := c.Cancel(context.Background(), id); err == nil {
			t.Fatalf("accepted %q", id)
		}
		if _, err := c.Content(context.Background(), id); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
}

func TestClient_BoundsAndPreservesResponse(t *testing.T) {
	c := NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxControlBytes+1)))}, nil
	}))
	r, err := c.List(context.Background(), "")
	if err == nil || r.Status != 429 || len(r.Body) != maxControlBytes {
		t.Fatalf("status %d length %d err %v", r.Status, len(r.Body), err)
	}
}
func TestClient_RedirectIsNotFollowed(t *testing.T) {
	calls := 0
	c := NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://example.org/"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
	}))
	r, err := c.List(context.Background(), "")
	if err == nil || calls != 1 || r.Status != 302 || string(r.Body) != "redirect" {
		t.Fatalf("%+v %v calls %d", r, err, calls)
	}
}
func TestClient_StreamsContentAndCancel(t *testing.T) {
	c := NewClient("key", "project", roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.EscapedPath() == "/v1/batches/a%2Fb/cancel" && r.Method == "POST" {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}
		if r.URL.EscapedPath() != "/v1/files/a%2Fb/content" || r.Method != "GET" {
			t.Fatal(r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("one\ntwo\n"))}, nil
	}))
	if _, err := c.Cancel(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
	r, err := c.Content(context.Background(), "a/b")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Stream.Close() }()
	b, err := io.ReadAll(r.Stream)
	if err != nil || string(b) != "one\ntwo\n" {
		t.Fatalf("%q %v", b, err)
	}
}
func TestEncode_RequestLimit(t *testing.T) {
	if _, err := Encode(make([]Item, 50001)); err == nil {
		t.Fatal("accepted too many requests")
	}
	if _, err := Encode(nil); err == nil {
		t.Fatal("accepted empty input")
	}
}

func TestEncode_RejectsInvalidUTF8ID(t *testing.T) {
	if _, err := Encode([]Item{{ID: string([]byte{0xff}), Body: []byte(`{}`)}}); err == nil {
		t.Fatal("accepted an ID that JSON encoding changes")
	}
}

func TestEncode_RejectsInvalidEnvelopes(t *testing.T) {
	tests := []struct {
		name  string
		items []Item
	}{
		{"null body", []Item{{ID: "one", Body: []byte(`null`)}}},
		{"array body", []Item{{ID: "one", Body: []byte(`[]`)}}},
		{"raw newline", []Item{{ID: "one", Body: []byte("{\n\"x\":1}")}}},
		{"raw carriage return", []Item{{ID: "one", Body: []byte("{\r\"x\":1}")}}},
		{"invalid utf8", []Item{{ID: "one", Body: []byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}}}},
		{"duplicate ID", []Item{{ID: "one", Body: []byte(`{}`)}, {ID: "one", Body: []byte(`{}`)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Encode(tt.items); err == nil {
				t.Fatal("accepted invalid batch item")
			}
		})
	}
}
func TestEncode_AllowsEscapedNewline(t *testing.T) {
	body := []byte(`{"x":"line\nnext"}`)
	b, err := Encode([]Item{{ID: "one", Body: body}})
	if err != nil || !strings.Contains(string(b), string(body)) {
		t.Fatalf("%q %v", b, err)
	}
}

type trackedBody struct {
	io.Reader
	closed   bool
	closeErr error
}

func (b *trackedBody) Close() error { b.closed = true; return b.closeErr }
func TestClient_FailClosedResponses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		headers http.Header
	}{
		{"partial", 206, nil}, {"range header", 200, http.Header{"Content-Range": []string{"bytes 0-2/9"}}}, {"malformed redirect", 302, http.Header{"Location": []string{":bad"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &trackedBody{Reader: strings.NewReader("evidence")}
			calls := 0
			c := NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: tc.headers, Body: b}, nil
			}))
			r, err := c.Content(context.Background(), "file")
			if err == nil || r.Status != tc.status || string(r.Body) != "evidence" || r.Stream != nil || !b.closed || calls != 1 {
				t.Fatalf("%+v %v closed=%v calls=%d", r, err, b.closed, calls)
			}
		})
	}
	c := NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 206, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("partial"))}, nil
	}))
	r, err := c.Create(context.Background(), "file", "sub")
	if err == nil || r.Status != 206 || string(r.Body) != "partial" {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestClient_InvalidCredentialsAndCreateIDs(t *testing.T) {
	for _, tc := range []struct{ key, project, file, sub string }{{" ", "p", "f", "s"}, {"k", "\t", "f", "s"}, {"k", "p", "", "s"}, {"k", "p", "f", ""}, {"k", "p", string([]byte{255}), "s"}, {"k", "p", "f", string([]byte{255})}} {
		c := NewClient(tc.key, tc.project, roundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("invalid input dispatched"); return nil, nil }))
		if _, err := c.Create(context.Background(), tc.file, tc.sub); err == nil {
			t.Fatalf("accepted invalid input")
		}
	}
}
func TestContent_SanitizesReadAndCloseErrors(t *testing.T) {
	body := &trackedBody{Reader: badReader{}, closeErr: errors.New("secret")}
	c := NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
	}))
	r, err := c.Content(context.Background(), "file")
	if err != nil {
		t.Fatal(err)
	}
	p := make([]byte, 10)
	n, err := r.Stream.Read(p)
	if n != 4 || err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("%d %v", n, err)
	}
	if err = r.Stream.Close(); err == nil || strings.Contains(err.Error(), "secret") || !body.closed {
		t.Fatalf("%v %v", err, body.closed)
	}
}

type cancelReader struct {
	cancel context.CancelFunc
	calls  int
}

func (r *cancelReader) Read(p []byte) (int, error) {
	r.calls++
	r.cancel()
	copy(p, "a")
	return 1, nil
}
func TestUpload_CancellationBetweenReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := &cancelReader{cancel: cancel}
	c := NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) { t.Fatal("cancelled upload dispatched"); return nil, nil }))
	if _, err := c.Upload(ctx, input); err == nil || input.calls != 1 {
		t.Fatalf("%v calls=%d", err, input.calls)
	}
}

func TestEncode_ValidItemsOverLimits(t *testing.T) {
	items := make([]Item, 50001)
	for i := range items {
		items[i] = Item{ID: strconv.Itoa(i), Body: []byte(`{}`)}
	}
	if _, err := Encode(items); err == nil {
		t.Fatal("accepted too many valid items")
	}
	body := append([]byte(`{"x":"`), bytes.Repeat([]byte("a"), maxInputBytes)...)
	body = append(body, []byte(`"}`)...)
	if _, err := Encode([]Item{{ID: "one", Body: body}}); err == nil {
		t.Fatal("accepted too many bytes")
	}
}
func TestContent_CancellationBeforeRead(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	body := &trackedBody{Reader: strings.NewReader("private")}
	c := NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
	}))
	r, err := c.Content(ctx, "file")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	p := make([]byte, 20)
	n, err := r.Stream.Read(p)
	if n != 0 || err == nil {
		t.Fatalf("%d %v", n, err)
	}
	if err = r.Stream.Close(); err != nil || !body.closed {
		t.Fatalf("%v closed=%v", err, body.closed)
	}
}
func TestClient_ControlRetainsMalformedRedirect(t *testing.T) {
	b := &trackedBody{Reader: strings.NewReader("evidence")}
	c := NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{":invalid"}}, Body: b}, nil
	}))
	r, err := c.List(context.Background(), "")
	if err == nil || r.Status != 302 || string(r.Body) != "evidence" || !b.closed {
		t.Fatalf("%+v %v closed=%v", r, err, b.closed)
	}
}

func TestClient_Framing(t *testing.T) {
	tests := []struct {
		name     string
		length   int64
		encoding []string
		coding   string
		body     string
		wantOK   bool
	}{
		{"close delimited", -1, nil, "", "abc", false},
		{"short length", 5, nil, "", "abc", false},
		{"exact length", 3, nil, "", "abc", true},
		{"chunked", -1, []string{"chunked"}, "", "abc", true},
		{"compressed", 3, nil, "gzip", "abc", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			factory := func() *Client {
				return NewClient("key", "project", roundTrip(func(*http.Request) (*http.Response, error) {
					h := make(http.Header)
					if tc.coding != "" {
						h.Set("Content-Encoding", tc.coding)
					}
					return &http.Response{StatusCode: 200, ContentLength: tc.length, TransferEncoding: tc.encoding, Header: h, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
				}))
			}
			r, err := factory().Count(context.Background(), []byte(`{}`))
			if (err == nil) != tc.wantOK || r.Status != 200 || string(r.Body) != "abc" {
				t.Fatalf("control %+v %v", r, err)
			}
			stream, err := factory().Content(context.Background(), "file")
			if tc.length < 0 && len(tc.encoding) == 0 || tc.coding != "" {
				if err == nil || stream.Stream != nil || string(stream.Body) != "abc" {
					t.Fatalf("content %+v %v", stream, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			b, readErr := io.ReadAll(stream.Stream)
			closeErr := stream.Stream.Close()
			if string(b) != "abc" || (readErr == nil) != tc.wantOK || closeErr != nil {
				t.Fatalf("stream %q %v %v", b, readErr, closeErr)
			}
		})
	}
}
