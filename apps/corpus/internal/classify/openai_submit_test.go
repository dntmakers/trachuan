package classify

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"danny.vn/trachuan/apps/corpus/internal/openaibatch"
)

type submitFake struct {
	events                   *[]string
	fail                     string
	blocked                  bool
	upload, create           openaibatch.Response
	uploadErr, createErr     error
	wire                     []byte
	pair                     []string
	verified                 []string
	dispatch                 []string
	receipt                  string
	hash                     string
	ids                      []string
	uploadSaved, createSaved openaibatch.Response
}

func (f *submitFake) ProjectID() string          { return "proj-test" }
func (f *submitFake) AccountFingerprint() string { return strings.Repeat("a", 32) }
func (f *submitFake) Upload(_ context.Context, r io.Reader) (openaibatch.Response, error) {
	*f.events = append(*f.events, "upload")
	f.wire, _ = io.ReadAll(r)
	return f.upload, f.uploadErr
}
func (f *submitFake) Create(_ context.Context, file, id string) (openaibatch.Response, error) {
	*f.events = append(*f.events, "create")
	f.pair = []string{file, id}
	return f.create, f.createErr
}
func (f *submitFake) record(s string) error {
	*f.events = append(*f.events, s)
	if f.fail == s {
		return errors.New("synthetic")
	}
	return nil
}
func (f *submitFake) Check(_ Submission) error {
	if err := f.record("check"); err != nil {
		return err
	}
	if f.blocked {
		return errors.New("blocked")
	}
	return nil
}
func (f *submitFake) Input(_ Submission, hash string, ids []string) error {
	f.hash = hash
	f.ids = append([]string(nil), ids...)
	return f.record("input")
}
func (f *submitFake) UploadResponse(_ Submission, response openaibatch.Response) error {
	f.uploadSaved = response
	return f.record("upload-response")
}
func (f *submitFake) UploadVerified(_ Submission, id, hash string) error {
	f.verified = []string{id, hash}
	return f.record("upload-verified")
}
func (f *submitFake) Dispatch(_ Submission, input, id string) error {
	f.dispatch = []string{input, id}
	return f.record("dispatch")
}
func (f *submitFake) CreateResponse(_ Submission, response openaibatch.Response) error {
	f.createSaved = response
	return f.record("create-response")
}
func (f *submitFake) Receipt(_ Submission, id string) error {
	f.receipt = id
	return f.record("receipt")
}

func submitFixture(t *testing.T) (Submission, [][]byte, *submitFake, *[]string) {
	t.Helper()
	r := articleRecipe(t)
	r.Provider = "openai"
	r.Model = "gpt-6.2-sol"
	r.Article = nil
	bodies := [][]byte{[]byte(`{"a":1}`), []byte(`{"a":2}`)}
	items := []Item{{Key: "first", RequestHash: openAICountKey(bodies[0]), InputTokens: 1}, {Key: "local", RequestHash: openAICountKey([]byte(`{"local":true}`)), Local: true}, {Key: "second", RequestHash: openAICountKey(bodies[1]), InputTokens: 1}}
	s := Submission{ID: strings.Repeat("b", 32), Provider: "openai", ProjectID: "proj-test", AccountFingerprint: strings.Repeat("a", 32), KeyVersion: keyVersion, PriceVersion: openAIBatchPriceVersion, Recipe: r, Items: items, Budget: "openai-trial", BudgetMicroUSD: 40000000, Results: map[string]Outcome{"local": {}}}
	cost, err := r.reserve(s.PriceVersion, 1)
	if err != nil {
		t.Fatal(err)
	}
	s.ReserveMicroUSD = cost * 2
	s.ItemsHash = itemsHash(items)
	events := []string{}
	f := &submitFake{events: &events, upload: openaibatch.Response{Status: 200, Body: []byte(`{"id":"file-test","object":"file","purpose":"batch"}`)}, create: openaibatch.Response{Status: 200, Body: []byte(`{"id":"batch-test","object":"batch","endpoint":"/v1/responses","input_file_id":"file-test","metadata":{"submission_id":"` + s.ID + `"}}`)}}
	return s, bodies, f, &events
}
func TestOpenAISubmitOrderAndSparseIndices(t *testing.T) {
	s, bodies, f, events := submitFixture(t)
	id, err := openAISubmit(context.Background(), s, bodies, f, f)
	if err != nil || id != "batch-test" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	if !reflect.DeepEqual(*events, []string{"check", "input", "upload", "upload-response", "upload-verified", "dispatch", "create", "create-response", "receipt"}) {
		t.Fatal(*events)
	}
	expected := `{"custom_id":"` + s.ID + `-000000","method":"POST","url":"/v1/responses","body":{"a":1}}` + "\n" + `{"custom_id":"` + s.ID + `-000002","method":"POST","url":"/v1/responses","body":{"a":2}}` + "\n"
	if !reflect.DeepEqual(f.pair, []string{"file-test", s.ID}) {
		t.Fatalf("pair %v", f.pair)
	}
	if string(f.wire) != expected {
		t.Fatalf("wire %q", f.wire)
	}
}
func TestOpenAISubmitStopsOnSinkFailure(t *testing.T) {
	for _, point := range []string{"check", "input", "upload-response", "upload-verified", "dispatch", "create-response", "receipt"} {
		t.Run(point, func(t *testing.T) {
			s, b, f, events := submitFixture(t)
			f.fail = point
			id, err := openAISubmit(context.Background(), s, b, f, f)
			if err == nil || id != "" || (*events)[len(*events)-1] != point {
				t.Fatalf("id=%q err=%v events=%v", id, err, *events)
			}
		})
	}
}
func TestOpenAISubmitRejectsUntrustedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*Submission, *[][]byte, *submitFake)
	}{
		{"blocked", func(_ *Submission, _ *[][]byte, f *submitFake) { f.blocked = true }},
		{"project", func(s *Submission, _ *[][]byte, _ *submitFake) { s.ProjectID = "proj-other" }},
		{"fingerprint", func(s *Submission, _ *[][]byte, _ *submitFake) { s.AccountFingerprint = strings.Repeat("c", 32) }},
		{"body", func(_ *Submission, b *[][]byte, _ *submitFake) { (*b)[0] = []byte(`{"changed":1}`) }},
		{"upload partial", func(_ *Submission, _ *[][]byte, f *submitFake) { f.uploadErr = errors.New("partial") }},
		{"upload purpose", func(_ *Submission, _ *[][]byte, f *submitFake) {
			f.upload.Body = []byte(`{"id":"file-test","object":"file","purpose":"other"}`)
		}},
		{"create partial", func(_ *Submission, _ *[][]byte, f *submitFake) { f.createErr = errors.New("partial") }},
		{"create wrong pair", func(_ *Submission, _ *[][]byte, f *submitFake) {
			f.create.Body = []byte(`{"id":"batch-test","endpoint":"/v1/responses","input_file_id":"wrong"}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, b, f, events := submitFixture(t)
			tc.alter(&s, &b, f)
			id, err := openAISubmit(context.Background(), s, b, f, f)
			if err == nil || id != "" {
				t.Fatalf("id=%q err=%v events=%v", id, err, *events)
			}
			for _, e := range *events {
				if e == "receipt" {
					t.Fatal(*events)
				}
			}
		})
	}
}

func TestOpenAISubmitRetainsInputScope(t *testing.T) {
	s, b, f, _ := submitFixture(t)
	if _, err := openAISubmit(context.Background(), s, b, f, f); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.verified, []string{"file-test", digest(f.wire)}) || !reflect.DeepEqual(f.dispatch, []string{"file-test", s.ID}) || f.receipt != "batch-test" {
		t.Fatalf("verified=%v dispatch=%v receipt=%q", f.verified, f.dispatch, f.receipt)
	}
	if f.hash != digest(f.wire) || !reflect.DeepEqual(f.ids, []string{customID(s.ID, 0), customID(s.ID, 2)}) {
		t.Fatalf("scope hash=%q ids=%v", f.hash, f.ids)
	}
}
func TestOpenAISubmitRetainsUnusableResponses(t *testing.T) {
	for _, operation := range []string{"upload", "create"} {
		t.Run(operation, func(t *testing.T) {
			s, b, f, _ := submitFixture(t)
			bad := openaibatch.Response{Status: 200, Body: []byte(`{"partial":`)}
			if operation == "upload" {
				f.upload = bad
				f.uploadErr = errors.New("partial")
			} else {
				f.create = bad
				f.createErr = errors.New("partial")
			}
			if _, err := openAISubmit(context.Background(), s, b, f, f); err == nil {
				t.Fatal("accepted partial response")
			}
			got := f.uploadSaved
			if operation == "create" {
				got = f.createSaved
			}
			if !reflect.DeepEqual(got, bad) {
				t.Fatalf("saved %#v", got)
			}
		})
	}
}
func TestOpenAISubmitRejectsInvalidCounts(t *testing.T) {
	s, b, f, _ := submitFixture(t)
	f.create.Body = []byte(`{"id":"batch-test","object":"batch","endpoint":"/v1/responses","input_file_id":"file-test","metadata":{"submission_id":"` + s.ID + `"},"request_counts":{"total":1,"completed":2,"failed":0}}`)
	if _, err := openAISubmit(context.Background(), s, b, f, f); err == nil {
		t.Fatal("accepted impossible counts")
	}
	if !reflect.DeepEqual(f.createSaved, f.create) {
		t.Fatal("raw response not retained")
	}
}

func TestOpenAISubmitRejectsPartialHTTPStatus(t *testing.T) {
	for _, operation := range []string{"upload", "create"} {
		t.Run(operation, func(t *testing.T) {
			s, b, f, _ := submitFixture(t)
			if operation == "upload" {
				f.upload.Status = 206
			} else {
				f.create.Status = 206
			}
			if _, err := openAISubmit(context.Background(), s, b, f, f); err == nil {
				t.Fatal("accepted partial status")
			}
		})
	}
}

func TestOpenAISubmitRejectsUnusableProviderIDs(t *testing.T) {
	for _, operation := range []string{"upload", "create"} {
		t.Run(operation, func(t *testing.T) {
			s, b, f, _ := submitFixture(t)
			if operation == "upload" {
				f.upload.Body = []byte(`{"id":" ","object":"file","purpose":"batch"}`)
			} else {
				f.create.Body = []byte(`{"id":" ","object":"batch","endpoint":"/v1/responses","input_file_id":"file-test","metadata":{"submission_id":"` + s.ID + `"}}`)
			}
			if _, err := openAISubmit(context.Background(), s, b, f, f); err == nil {
				t.Fatal("accepted unusable id")
			}
		})
	}
}

func TestOpenAISubmitRejectsLossyOrUnusableIDs(t *testing.T) {
	for _, operation := range []string{"upload", "create"} {
		for _, id := range []string{`"."`, `".."`, `"has space"`, `"x\u0000y"`, `"x\ud800y"`, `"x\udc00y"`, "\"x\xffy\""} {
			t.Run(operation+"/"+id, func(t *testing.T) {
				s, b, f, events := submitFixture(t)
				if operation == "upload" {
					f.upload.Body = []byte(`{"id":` + id + `,"object":"file","purpose":"batch"}`)
				} else {
					f.create.Body = []byte(`{"id":` + id + `,"object":"batch","endpoint":"/v1/responses","input_file_id":"file-test","metadata":{"submission_id":"` + s.ID + `"}}`)
				}
				if _, err := openAISubmit(context.Background(), s, b, f, f); err == nil {
					t.Fatal("accepted invalid id")
				}
				got, want := f.uploadSaved, f.upload
				forbidden := "create"
				if operation == "create" {
					got, want = f.createSaved, f.create
					forbidden = "receipt"
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("raw response lost")
				}
				for _, e := range *events {
					if e == forbidden {
						t.Fatalf("unexpected %s", e)
					}
				}
			})
		}
	}
}
func TestOpenAISubmitUploadFileStatus(t *testing.T) {
	for _, tc := range []struct {
		status string
		ok     bool
	}{{"", true}, {`,"status":"uploaded"`, true}, {`,"status":"processed"`, true}, {`,"status":"error"`, false}, {`,"status":"unknown"`, false}, {`,"status":null`, false}, {`,"status":3`, false}} {
		t.Run(tc.status, func(t *testing.T) {
			s, b, f, events := submitFixture(t)
			f.upload.Body = []byte(`{"id":"file-test","object":"file","purpose":"batch"` + tc.status + `}`)
			_, err := openAISubmit(context.Background(), s, b, f, f)
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
			if !reflect.DeepEqual(f.uploadSaved, f.upload) {
				t.Fatal("raw response lost")
			}
			if !tc.ok {
				for _, e := range *events {
					if e == "dispatch" || e == "create" {
						t.Fatal(*events)
					}
				}
			}
		})
	}
}
func TestOpenAISubmitCreateIdentityMismatch(t *testing.T) {
	for _, tc := range []struct{ name, old, replacement string }{
		{"batch-id", `"id":"batch-test"`, `"id":""`},
		{"endpoint", `"endpoint":"/v1/responses"`, `"endpoint":"/v1/other"`},
		{"input", `"input_file_id":"file-test"`, `"input_file_id":"file-other"`},
		{"submission", `"submission_id":"` + strings.Repeat("b", 32) + `"`, `"submission_id":"other"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, b, f, events := submitFixture(t)
			original := string(f.create.Body)
			changed := strings.Replace(original, tc.old, tc.replacement, 1)
			if changed == original {
				t.Fatal("mutation missing")
			}
			f.create.Body = []byte(changed)
			if _, err := openAISubmit(context.Background(), s, b, f, f); err == nil {
				t.Fatal("accepted mismatch")
			}
			if !reflect.DeepEqual(f.createSaved, f.create) {
				t.Fatal("raw response lost")
			}
			for _, e := range *events {
				if e == "receipt" {
					t.Fatal(*events)
				}
			}
		})
	}
}

func TestOpenAISubmitBlockedGuardDoesNotCallProvider(t *testing.T) {
	s, b, f, events := submitFixture(t)
	f.blocked = true
	if _, err := openAISubmit(context.Background(), s, b, f, f); err == nil {
		t.Fatal("accepted blocked guard")
	}
	if !reflect.DeepEqual(*events, []string{"check"}) {
		t.Fatal(*events)
	}
}

func TestOpenAISubmitPreservesValidUnicodeIDs(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{`"batch-😀"`, "batch-😀"}, {`"batch-�"`, "batch-�"}, {`"batch-\\ud800"`, `batch-\ud800`}} {
		t.Run(tc.raw, func(t *testing.T) {
			s, b, f, _ := submitFixture(t)
			f.create.Body = []byte(`{"id":` + tc.raw + `,"object":"batch","endpoint":"/v1/responses","input_file_id":"file-test","metadata":{"submission_id":"` + s.ID + `"}}`)
			id, err := openAISubmit(context.Background(), s, b, f, f)
			if err != nil || id != tc.want || f.receipt != tc.want {
				t.Fatalf("id=%q receipt=%q err=%v", id, f.receipt, err)
			}
		})
	}
}

func TestOpenAISubmitRejectsLossyCreateInputIdentity(t *testing.T) {
	s, b, f, events := submitFixture(t)
	f.upload.Body = []byte(`{"id":"file-�","object":"file","purpose":"batch"}`)
	f.create.Body = []byte(`{"id":"batch-test","object":"batch","endpoint":"/v1/responses","input_file_id":"file-\ud800","metadata":{"submission_id":"` + s.ID + `"}}`)
	if _, err := openAISubmit(context.Background(), s, b, f, f); err == nil {
		t.Fatal("accepted lossy input identity")
	}
	if !reflect.DeepEqual(f.createSaved, f.create) {
		t.Fatal("raw create response lost")
	}
	for _, e := range *events {
		if e == "receipt" {
			t.Fatal("receipt granted")
		}
	}
}
