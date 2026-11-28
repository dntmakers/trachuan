package embed

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"danny.vn/trachuan/apps/corpus/internal/artifact"
	"danny.vn/trachuan/apps/corpus/internal/stage/chunk"
	"danny.vn/trachuan/internal/embedding"
)

type Index struct {
	Version     int              `json:"version"`
	DocumentKey string           `json:"document_key"`
	ChunkSHA256 string           `json:"chunk_sha256"`
	RecipeHash  string           `json:"recipe_hash"`
	Recipe      embedding.Recipe `json:"recipe"`
	Dimensions  int              `json:"dimensions"`
	Rows        []Row            `json:"rows"`
	Rejected    []Rejected       `json:"rejected"`
}

type Row struct {
	Row          int    `json:"row"`
	ChunkOrdinal int    `json:"chunk_ordinal"`
	TokenCount   int    `json:"token_count"`
	InputSHA256  string `json:"input_sha256"`
}

type Rejected struct {
	ChunkOrdinal int    `json:"chunk_ordinal"`
	TokenCount   *int   `json:"token_count"`
	Kind         string `json:"kind"`
}
type reportGap struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}
type report struct {
	Version  int         `json:"version"`
	Rows     int         `json:"rows"`
	Rejected int         `json:"rejected"`
	Gaps     []reportGap `json:"gaps"`
}

func makeReport(idx Index, coverage chunk.Coverage) report {
	r := report{Version: 1, Rows: len(idx.Rows), Rejected: len(idx.Rejected), Gaps: []reportGap{}}
	for _, g := range coverage.Gaps {
		r.Gaps = append(r.Gaps, reportGap{Kind: g.Kind, Detail: g.Detail})
	}
	for _, v := range idx.Rejected {
		detail := fmt.Sprintf("chunk ordinal %d", v.ChunkOrdinal)
		if v.TokenCount != nil {
			detail += fmt.Sprintf(", token count %d", *v.TokenCount)
		}
		r.Gaps = append(r.Gaps, reportGap{Kind: v.Kind, Detail: detail})
	}
	return r
}
func decodeFile(dir, name string, dst any) error {
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("open %s: %w", name, err)
	}
	defer func() { _ = f.Close() }()
	return decodeArtifact(name, f, dst)
}

func decodeArtifact(name string, r io.Reader, dst any) error {
	d := json.NewDecoder(r)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	var extra json.RawMessage
	err := d.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	var syntax *json.SyntaxError
	if err == nil || err == io.ErrUnexpectedEOF || errors.As(err, &syntax) {
		return fmt.Errorf("trailing JSON in %s", name)
	}
	return fmt.Errorf("decode trailing JSON in %s: %w", name, err)
}
func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32 && s == fmt.Sprintf("%x", b)
}

func ReadArtifact(dir string) (Index, error) {
	var idx Index
	if _, err := artifact.Verify(dir); err != nil {
		return idx, fmt.Errorf("verify embed artifact: %w", err)
	}
	return readVerifiedIndex(dir)
}

func readVerifiedIndex(dir string) (Index, error) {
	file, err := os.Open(filepath.Join(dir, "index.json"))
	if err != nil {
		return Index{}, fmt.Errorf("open index.json: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(file)
	if err != nil {
		return Index{}, fmt.Errorf("decode index.json: %w", err)
	}
	return DecodeIndex(data)
}

func DecodeIndex(data []byte) (Index, error) {
	var idx Index
	if err := decodeArtifact("index.json", bytes.NewReader(data), &idx); err != nil {
		return idx, err
	}
	if idx.Version != 1 || idx.DocumentKey == "" || !validHash(idx.ChunkSHA256) || idx.RecipeHash != idx.Recipe.Hash() || idx.Dimensions <= 0 || idx.Dimensions != idx.Recipe.Dimensions || idx.Rows == nil || idx.Rejected == nil {
		return idx, fmt.Errorf("invalid embedding index")
	}
	seen := map[int]bool{}
	prev := -1
	for i, r := range idx.Rows {
		if r.Row != i || r.ChunkOrdinal <= prev || r.TokenCount <= 0 || r.TokenCount > idx.Recipe.TokenCap || !validHash(r.InputSHA256) {
			return idx, fmt.Errorf("invalid embedding row")
		}
		seen[r.ChunkOrdinal] = true
		prev = r.ChunkOrdinal
	}
	prev = -1
	for _, r := range idx.Rejected {
		if r.ChunkOrdinal <= prev || seen[r.ChunkOrdinal] || (r.Kind == "embedding_input_too_long" && (r.TokenCount == nil || *r.TokenCount <= idx.Recipe.TokenCap)) || (r.Kind == "embedding_input_bytes_exceeded" && r.TokenCount != nil) || (r.Kind != "embedding_input_too_long" && r.Kind != "embedding_input_bytes_exceeded") {
			return idx, fmt.Errorf("invalid rejected row")
		}
		seen[r.ChunkOrdinal] = true
		prev = r.ChunkOrdinal
	}
	return idx, nil
}
func readReport(dir string) (report, error) {
	var r report
	err := decodeFile(dir, "report.json", &r)
	if err != nil {
		return r, err
	}
	if r.Version != 1 || r.Rows < 0 || r.Rejected < 0 || r.Gaps == nil {
		return r, fmt.Errorf("invalid embedding report")
	}
	return r, nil
}
func validateVectors(data []byte, rows, dimensions int) error {
	if rows < 0 || dimensions <= 0 || int64(len(data)) != int64(rows)*int64(dimensions)*4 {
		return fmt.Errorf("invalid vector size")
	}
	for i := 0; i < rows; i++ {
		var norm float64
		for j := 0; j < dimensions; j++ {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[(i*dimensions+j)*4:])))
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("non-finite vector")
			}
			norm += v * v
		}
		if math.Abs(math.Sqrt(norm)-1) > 0.001 {
			return fmt.Errorf("invalid vector norm")
		}
	}
	return nil
}

func ValidateVectorBytes(data []byte, index Index) error {
	return validateVectors(data, len(index.Rows), index.Dimensions)
}

func ReadVectorBytes(data []byte, index Index, row int) ([]float32, error) {
	if row < 0 || row >= len(index.Rows) {
		return nil, fmt.Errorf("vector row out of bounds")
	}
	if index.Dimensions <= 0 || int64(len(data)) != int64(len(index.Rows))*int64(index.Dimensions)*4 {
		return nil, fmt.Errorf("invalid vector size")
	}
	width := index.Dimensions * 4
	raw := data[row*width : (row+1)*width]
	if err := validateVectors(raw, 1, index.Dimensions); err != nil {
		return nil, err
	}
	values := make([]float32, index.Dimensions)
	for i := range values {
		values[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return values, nil
}

type VectorReader struct {
	file             *os.File
	dimensions, rows int
}

func OpenVectors(dir string, index Index) (*VectorReader, error) {
	actual, err := ReadArtifact(dir)
	if err != nil {
		return nil, err
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(index)
	if string(a) != string(b) {
		return nil, fmt.Errorf("embedding index mismatch")
	}
	f, err := os.Open(filepath.Join(dir, "vectors.f32"))
	if err != nil {
		return nil, fmt.Errorf("open vectors: %w", err)
	}
	fail := func(e error) (*VectorReader, error) { _ = f.Close(); return nil, e }
	info, err := f.Stat()
	if err != nil {
		return fail(fmt.Errorf("stat vectors: %w", err))
	}
	if info.Size() != int64(len(index.Rows))*int64(index.Dimensions)*4 {
		return fail(fmt.Errorf("invalid vector size"))
	}
	buf := make([]byte, index.Dimensions*4)
	for range index.Rows {
		if _, err = io.ReadFull(f, buf); err != nil {
			return fail(fmt.Errorf("read vector: %w", err))
		}
		if err = validateVectors(buf, 1, index.Dimensions); err != nil {
			return fail(err)
		}
	}
	return &VectorReader{f, index.Dimensions, len(index.Rows)}, nil
}

func (r *VectorReader) Row(i int) ([]float32, error) {
	if i < 0 || i >= r.rows {
		return nil, fmt.Errorf("vector row out of bounds")
	}
	buf := make([]byte, r.dimensions*4)
	if _, err := r.file.ReadAt(buf, int64(i)*int64(len(buf))); err != nil {
		return nil, fmt.Errorf("read vector row: %w", err)
	}
	if err := validateVectors(buf, 1, r.dimensions); err != nil {
		return nil, err
	}
	values := make([]float32, r.dimensions)
	for j := range values {
		values[j] = math.Float32frombits(binary.LittleEndian.Uint32(buf[j*4:]))
	}
	return values, nil
}

func (r *VectorReader) Close() error { return r.file.Close() }
