package classify

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type trialWire struct {
	afterUpload                                         func()
	count                                               int64
	oddCreate                                           bool
	t                                                   *testing.T
	calls                                               []string
	out, submission, pause, status, result, errorResult string
	usage                                               string
}

func (w *trialWire) RoundTrip(req *http.Request) (*http.Response, error) {
	call := req.Method + " " + req.URL.Path
	w.calls = append(w.calls, call)
	if w.pause == call {
		return nil, errors.New("interrupted")
	}
	var raw string
	switch call {
	case "POST /v1/responses/input_tokens":
		if _, err := os.Stat(filepath.Join(w.out, "state.json")); err != nil {
			w.t.Fatal("count preceded trial evidence", err)
		}
		count := w.count
		if count == 0 {
			count = 100
		}
		raw = fmt.Sprintf(`{"object":"response.input_tokens","input_tokens":%d}`, count)
	case "POST /v1/files":
		input := trialRead(w.t, w.out, "input.jsonl")
		multipart, err := req.MultipartReader()
		if err != nil {
			w.t.Fatal(err)
		}
		purpose, err := multipart.NextPart()
		if err != nil {
			w.t.Fatal(err)
		}
		body, err := io.ReadAll(purpose)
		if err != nil || purpose.FormName() != "purpose" || string(body) != "batch" {
			w.t.Fatal("invalid upload purpose", err)
		}
		file, err := multipart.NextPart()
		if err != nil {
			w.t.Fatal(err)
		}
		body, err = io.ReadAll(file)
		if err != nil || file.FormName() != "file" || string(input) != string(body) || !bytes.Contains(body, []byte(`"custom_id":"0"`)) || !bytes.Contains(body, []byte(`"store":false`)) || bytes.Contains(body, []byte(`"tools"`)) {
			w.t.Fatal("upload did not use the durable tool-free input", err)
		}
		raw = `{"object":"file","id":"file-in","purpose":"batch","status":"processed"}`
		if w.afterUpload != nil {
			w.afterUpload()
		}
	case "POST /v1/batches":
		var body struct{ Metadata map[string]string }
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			w.t.Fatal(err)
		}
		w.submission = body.Metadata["submission_id"]
		if w.submission == "" || !bytes.Contains(trialRead(w.t, w.out, "state.json"), []byte(`"dispatched"`)) {
			w.t.Fatal("create preceded dispatch evidence")
		}
		raw = w.batch(`"status":"validating"`)
		if w.oddCreate {
			raw = strings.Replace(raw, `"id":"batch-one"`, `"id":17`, 1)
		}
	case "GET /v1/batches/batch-one":
		status := w.status
		if status == "" {
			status = "completed"
		}
		counts := `"total":1,"completed":1,"failed":0`
		errorFile := ""
		if w.errorResult != "" {
			counts, errorFile = `"total":2,"completed":1,"failed":1`, `,"error_file_id":"file-error"`
		}
		raw = w.batch(fmt.Sprintf(`"status":%q,"output_file_id":"file-result"%s,"request_counts":{%s}`, status, errorFile, counts))
	case "GET /v1/files/file-result/content":
		trialRead(w.t, w.out, "terminal.json")
		result := w.result
		if result == "" {
			result = `{"codes":["F","not-in-menu"]}`
		}
		usage := w.usage
		if usage == "" {
			usage = `{"input_tokens":101,"input_tokens_details":{"cached_tokens":0,"cache_write_tokens":0},"output_tokens":10,"output_tokens_details":{"reasoning_tokens":2},"total_tokens":111}`
		}
		raw = fmt.Sprintf(`{"custom_id":"0","response":{"status_code":200,"body":{"model":"gpt-6.2-sol","status":"completed","error":null,"usage":%s,"output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":%q}]}]}},"error":null}`+"\n", usage, result)
	case "GET /v1/files/file-error/content":
		raw = w.errorResult
	default:
		w.t.Fatalf("unexpected OpenAI request %s", call)
	}
	return &http.Response{StatusCode: 200, ContentLength: int64(len(raw)), Body: io.NopCloser(strings.NewReader(raw)), Header: http.Header{}}, nil
}

func (w *trialWire) batch(fields string) string {
	return fmt.Sprintf(`{"id":"batch-one","object":"batch","endpoint":"/v1/responses","input_file_id":"file-in","metadata":{"submission_id":%q},%s}`, w.submission, fields)
}

func trialRead(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func trialAbsent(t *testing.T, dir, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s should not exist: %v", name, err)
	}
}

func trialFixture(t *testing.T) (OpenAITrialOptions, []Document, []Family, []Node, *trialWire) {
	t.Helper()
	r, err := NewOpenAIRecipe("gpt-6.2-sol", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	o := OpenAITrialOptions{Country: "vn", Release: "20261122", LabelsHash: strings.Repeat("b", 64), Out: filepath.Join(t.TempDir(), "documents"), Recipe: r, CapMicroUSD: 40000000, Progress: io.Discard}
	return o, []Document{{Key: "vn/doc", Title: "Luật thử nghiệm"}}, []Family{{Code: "F", Name: "Family"}}, []Node{{Code: "F", Name: "Family", Active: true, Origin: "phapdien", Level: 2}}, &trialWire{t: t, out: o.Out}
}

func (w *trialWire) client() *OpenAITrialClient {
	return NewOpenAITrialClient("fixture-credential", "fixture_project", w)
}

func TestOpenAITrialDocuments_DryRunAndResume(t *testing.T) {
	o, docs, menu, nodes, wire := trialFixture(t)
	o.DryRun = true
	summary, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu)
	if err != nil || summary.Requests != 1 || summary.CountedInputTokens != 100 || summary.ReserveMicroUSD != 20605 || len(wire.calls) != 1 {
		t.Fatalf("dry-run summary=%+v calls=%v err=%v", summary, wire.calls, err)
	}
	trialAbsent(t, o.Out, "families.json")
	o.DryRun = false
	for _, pause := range []struct{ path, evidence string }{
		{"GET /v1/batches/batch-one", "create.json"},
		{"GET /v1/files/file-result/content", "terminal.json"},
	} {
		wire.pause = pause.path
		if _, err = RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
			t.Fatalf("expected interruption at %s", pause.path)
		}
		trialRead(t, o.Out, pause.evidence)
		trialAbsent(t, o.Out, "families.json")
	}
	wire.pause = ""
	if _, err = RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil {
		t.Fatal(err)
	}
	f, _, err := ReadFamilyArtifactForScore(filepath.Join(o.Out, "families.json"), o.Recipe.TaxonomyHash, nodes)
	if err != nil || !reflect.DeepEqual(f.Documents["vn/doc"].Codes, []string{"F"}) || !reflect.DeepEqual(f.Documents["vn/doc"].Dropped, []string{"not-in-menu"}) || f.Selection == nil || f.Selection.LabelsHash != o.LabelsHash {
		t.Fatalf("families artifact = %+v, %v", f, err)
	}
	cost := trialRead(t, o.Out, "cost.json")
	for _, fragment := range []string{`"cost_micro_usd": 151`, `"counted_input_tokens": 100`, `"usage_input_tokens": 101`} {
		if !bytes.Contains(cost, []byte(fragment)) {
			t.Errorf("missing %s in %s", fragment, cost)
		}
	}
	want := []string{"POST /v1/responses/input_tokens", "POST /v1/files", "POST /v1/batches", "GET /v1/batches/batch-one", "GET /v1/batches/batch-one", "GET /v1/files/file-result/content", "GET /v1/files/file-result/content"}
	if _, err = RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err != nil || !reflect.DeepEqual(wire.calls, want) {
		t.Fatalf("resume calls=%v err=%v", wire.calls, err)
	}
}

func TestOpenAITrialDocuments_RefusesSecondCreate(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	wire.pause = "POST /v1/batches"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted create")
	}
	wire.calls, wire.pause = nil, ""
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil || !strings.Contains(err.Error(), "metadata.submission_id") || len(wire.calls) != 0 {
		t.Fatalf("dispatch recovery calls=%v err=%v", wire.calls, err)
	}
}

func TestOpenAITrialDocuments_ReservesAgainstSibling(t *testing.T) {
	o, docs, menu, _, wire := trialFixture(t)
	wire.pause = "POST /v1/batches"
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
		t.Fatal("expected interrupted create")
	}
	o.Out = filepath.Join(filepath.Dir(o.Out), "second-documents")
	o.CapMicroUSD = 40000
	wire.out, wire.calls, wire.pause = o.Out, nil, ""
	if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil || !reflect.DeepEqual(wire.calls, []string{"POST /v1/responses/input_tokens"}) {
		t.Fatalf("cap refusal calls=%v err=%v", wire.calls, err)
	}
}

func TestOpenAITrialDocuments_ReportsUnknownAndPreservesPartial(t *testing.T) {
	for _, scenario := range []string{"unpriced", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			o, docs, menu, _, wire := trialFixture(t)
			if scenario == "unpriced" {
				wire.usage = `null`
			} else {
				wire.status = "cancelled"
				wire.errorResult = `{"custom_id":"0","error":{"code":"batch_cancelled"},"response":null}` + "\n"
			}
			if _, err := RunOpenAIDocumentTrial(t.Context(), o, wire.client(), docs, menu); err == nil {
				t.Fatal("incomplete evidence was accepted")
			}
			trialRead(t, o.Out, "output.jsonl")
			trialAbsent(t, o.Out, "families.json")
			if scenario == "cancelled" {
				if raw := trialRead(t, o.Out, "error.jsonl"); string(raw) != wire.errorResult {
					t.Fatal("error capture changed")
				}
			} else if cost := trialRead(t, o.Out, "cost.json"); !bytes.Contains(cost, []byte(`"unpriced": [`)) || !bytes.Contains(cost, []byte(`"0"`)) {
				t.Fatalf("missing unpriced item: %s", cost)
			}
		})
	}
}
