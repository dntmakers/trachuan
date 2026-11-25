package evidence

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"danny.vn/trachuan/internal/releasechanges"
)

type changingProjectionReader struct {
	*strings.Reader
	next    string
	rewinds int
}

func (r *changingProjectionReader) Seek(offset int64, whence int) (int64, error) {
	if whence == io.SeekStart {
		r.rewinds++
		if r.rewinds == 2 {
			r.Reader = strings.NewReader(r.next)
		}
	}
	return r.Reader.Seek(offset, whence)
}

func TestProjectStream_ChangingInput(t *testing.T) {
	original := "{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n{\"type\":\"document\",\"key\":\"a\",\"change\":\"added\",\"cause\":\"membership\"}\n"
	for _, changed := range []string{strings.Replace(original, `"key":"a"`, `"key":"b"`, 1), strings.Replace(original, `"schema":1`, `"schema":1,"extra":true`, 1)} {
		input := &changingProjectionReader{Reader: strings.NewReader(original), next: changed}
		emitted := 0
		err := ProjectStream(input, func(releasechanges.Row) error { emitted++; return nil })
		if err == nil || emitted != 0 {
			t.Fatalf("changed input accepted or emitted: %v %d", err, emitted)
		}
	}
}

func TestProjectStream_LineLimit(t *testing.T) {
	input := "{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n" + strings.Repeat(" ", 1024*1024+1) + "\n"
	err := ProjectStream(strings.NewReader(input), func(releasechanges.Row) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatal(err)
	}
}

func TestProjection_ItemBudget(t *testing.T) {
	key := strings.Repeat("𐀀", 512)
	input := fmt.Sprintf("{\"schema\":1,\"bindings\":{\"rel\":\"r\",\"dump_sha256\":\"d\",\"build_artifact_sha256\":\"b\",\"report_sha256\":\"p\",\"predecessor_rel\":\"x\",\"predecessor_identity_sha256\":\"i\"},\"baseline\":false}\n{\"type\":\"document\",\"key\":%q,\"change\":\"added\",\"cause\":\"membership\"}\n", key)
	err := ProjectStream(strings.NewReader(input), func(releasechanges.Row) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "ordinal 1") {
		t.Fatal(err)
	}
}

func TestProjection_RowBudget(t *testing.T) {
	source := func(yield func(projectionRecord) error) error {
		for _, key := range []string{"a", "b"} {
			r := projectionRecord{"type": []byte(`"document"`), "key": []byte(fmt.Sprintf(`%q`, key)), "change": []byte(`"added"`), "cause": []byte(`"membership"`)}
			if err := yield(r); err != nil {
				return err
			}
		}
		return nil
	}
	var emitted int
	err := projectValidatedLimit(false, source, func(releasechanges.Row) error { emitted++; return nil }, 1)
	if err == nil || !strings.Contains(err.Error(), "ordinal 1") || emitted != 0 {
		t.Fatalf("%v %d", err, emitted)
	}
}
