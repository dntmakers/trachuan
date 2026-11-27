package releasechanges

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const Version = 2

var kinds = [...]string{"document_added", "document_removed", "document_validity", "provision_text", "document_key"}

const KindCount = len(kinds)

func Kinds() []string { return slices.Clone(kinds[:]) }

func CountColumns() string { return strings.Join(kinds[:], ",") }

func SetDestinations[T int | int64](header *Header, country, release, digest **string, setCount *int, ok *bool, counts []T) []any {
	dest := []any{&header.Version, country, release, &header.Predecessor, &header.PredecessorIdentityHash, &header.Baseline}
	for i := range counts {
		dest = append(dest, &counts[i])
	}
	return append(dest, digest, setCount, ok)
}

type Header struct {
	Version                 int
	Country                 string
	Release                 string
	Predecessor             *string
	PredecessorIdentityHash *string
	Baseline                bool
}

type Row struct {
	Ordinal int64
	Kind    string
	Key     *string
	Key2    *string
	Payload string
}

type Hasher struct {
	hash     hash.Hash
	output   io.Writer
	baseline bool
	writer   bool
	ordinal  int64
	counts   map[string]int
}

func field(value string) error {
	if !utf8.ValidString(value) || strings.ContainsAny(value, "\t\n\x00") {
		return fmt.Errorf("invalid field")
	}
	return nil
}

func optional(value *string) (string, error) {
	if value == nil {
		return "", nil
	}
	if *value == "" {
		return "", fmt.Errorf("empty optional field")
	}
	return *value, field(*value)
}

func NewHasher(header Header) (*Hasher, error) { return newHasher(header, io.Discard, false) }

func NewWriter(header Header, output io.Writer) (*Hasher, error) {
	return newHasher(header, output, true)
}

func newHasher(header Header, output io.Writer, writer bool) (*Hasher, error) {
	if header.Version != Version {
		return nil, fmt.Errorf("unknown projection version %d", header.Version)
	}
	if header.Country == "" || header.Release == "" {
		return nil, fmt.Errorf("empty header field")
	}
	if err := field(header.Country); err != nil {
		return nil, err
	}
	if err := field(header.Release); err != nil {
		return nil, err
	}
	predecessor, err := optional(header.Predecessor)
	if err != nil {
		return nil, err
	}
	identity, err := optional(header.PredecessorIdentityHash)
	if err != nil {
		return nil, err
	}
	if identity != "" {
		decoded, e := hex.DecodeString(identity)
		if e != nil || len(decoded) != 32 || identity != strings.ToLower(identity) {
			return nil, fmt.Errorf("invalid predecessor identity hash")
		}
	}
	baseline := "0"
	if header.Baseline {
		baseline = "1"
	}
	h := &Hasher{hash: sha256.New(), output: output, baseline: header.Baseline, writer: writer, counts: make(map[string]int, len(kinds))}
	for _, kind := range kinds {
		h.counts[kind] = 0
	}
	if err := h.write(strings.Join([]string{"v" + strconv.Itoa(Version), header.Country, header.Release, predecessor, identity, baseline}, "\t") + "\n"); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *Hasher) write(line string) error {
	if _, err := io.WriteString(h.output, line); err != nil {
		return fmt.Errorf("write projection: %w", err)
	}
	_, _ = io.WriteString(h.hash, line)
	return nil
}

func (h *Hasher) Add(row Row) error {
	if h.baseline || row.Ordinal != h.ordinal+1 {
		return fmt.Errorf("ordinal %d: invalid sequence or baseline", row.Ordinal)
	}
	if _, ok := h.counts[row.Kind]; !ok {
		return fmt.Errorf("ordinal %d: unknown kind", row.Ordinal)
	}
	key, err := optional(row.Key)
	if err != nil {
		return fmt.Errorf("ordinal %d: %w", row.Ordinal, err)
	}
	key2, err := optional(row.Key2)
	if err != nil {
		return fmt.Errorf("ordinal %d: %w", row.Ordinal, err)
	}
	if row.Payload == "" {
		return fmt.Errorf("ordinal %d: empty payload", row.Ordinal)
	}
	if err = field(row.Payload); err != nil {
		return fmt.Errorf("ordinal %d: %w", row.Ordinal, err)
	}
	if h.writer {
		canonical, canonicalErr := CanonicalPayload([]byte(row.Payload))
		if canonicalErr != nil {
			return fmt.Errorf("ordinal %d: invalid payload: %w", row.Ordinal, canonicalErr)
		}
		if canonical != row.Payload {
			return fmt.Errorf("ordinal %d: noncanonical payload", row.Ordinal)
		}
	} else if _, err = parsePayload([]byte(row.Payload)); err != nil {
		return fmt.Errorf("ordinal %d: invalid payload: %w", row.Ordinal, err)
	}
	line := strings.Join([]string{strconv.FormatInt(row.Ordinal, 10), row.Kind, key, key2, row.Payload}, "\t") + "\n"
	if err := h.write(line); err != nil {
		return err
	}
	h.ordinal++
	h.counts[row.Kind]++
	return nil
}

func (h *Hasher) Sum() string { return hex.EncodeToString(h.hash.Sum(nil)) }
func (h *Hasher) Counts() map[string]int {
	out := make(map[string]int, len(h.counts))
	for k, v := range h.counts {
		out[k] = v
	}
	return out
}

func Digest(header Header, rows []Row) (string, error) {
	h, err := NewHasher(header)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if err := h.Add(row); err != nil {
			return "", err
		}
	}
	return h.Sum(), nil
}
