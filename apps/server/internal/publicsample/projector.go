package publicsample

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"danny.vn/trachuan/internal/retrieval"
)

const (
	Query          = "Ngân hàng cần xin sự đồng ý của khách hàng khi xử lý dữ liệu cá nhân trong những trường hợp nào?"
	DocumentKey    = "vn:vbpl:179252"
	DocumentNumber = "91/2025/QH15"
	CanonicalURL   = "https://vbpl.vn/van-ban/chi-tiet/179252"
	BodyCap        = 16 * 1024
)

type Reason string

func (r Reason) Error() string { return string(r) }

const (
	ReasonSuccess       Reason = "success"
	ReasonSearchError   Reason = "search_error"
	ReasonTimeout       Reason = "timeout"
	ReasonCanceled      Reason = "canceled"
	ReasonAbstain       Reason = "abstain"
	ReasonNoHit         Reason = "no_hit"
	ReasonWrongDocument Reason = "wrong_document"
	ReasonWrongNumber   Reason = "wrong_number"
	ReasonNoProvenCut   Reason = "no_proven_cut"
	ReasonMismatch      Reason = "raw_presented_mismatch"
	ReasonSchema        Reason = "schema"
	ReasonBodyCap       Reason = "body_cap"
)

type PublicSampleResult struct {
	Release   string    `json:"release"`
	Abstain   bool      `json:"abstain"`
	Hits      []Hit     `json:"hits"`
	Documents Documents `json:"documents"`
}
type Documents struct {
	Document Document `json:"vn:vbpl:179252"`
}
type Hit struct {
	Handle   string `json:"handle"`
	Doc      string `json:"doc"`
	Citation string `json:"citation"`
	Snippet  string `json:"snippet"`
	Complete bool   `json:"complete"`
}
type Status struct {
	Current     bool   `json:"current"`
	Class       string `json:"class,omitempty"`
	Basis       string `json:"basis,omitempty"`
	StatusBasis string `json:"status_basis,omitempty"`
}
type Document struct {
	Number        string          `json:"number"`
	Status        Status          `json:"status"`
	TextSource    string          `json:"text_source"`
	TextComplete  bool            `json:"text_complete"`
	Title         string          `json:"title,omitempty"`
	Type          string          `json:"type,omitempty"`
	Issuer        string          `json:"issuer,omitempty"`
	Issued        *retrieval.Date `json:"issued,omitempty"`
	EffectiveFrom *retrieval.Date `json:"effective_from,omitempty"`
	EffectiveTo   *retrieval.Date `json:"effective_to,omitempty"`
	SourceURL     string          `json:"source_url,omitempty"`
}

func Project(raw retrieval.Response, presented retrieval.SearchResult, release string) (PublicSampleResult, Reason) {
	var zero PublicSampleResult
	if raw.Abstain || presented.Abstain {
		return zero, ReasonAbstain
	}
	if len(raw.Hits) == 0 || len(presented.Hits) == 0 {
		return zero, ReasonNoHit
	}
	if release == "" {
		return zero, ReasonSchema
	}
	if raw.Release != release || presented.Release != release {
		return zero, ReasonMismatch
	}
	rh, ph := raw.Hits[0], presented.Hits[0]
	if ph.Doc != DocumentKey || rh.Identity.DocumentKey != DocumentKey {
		return zero, ReasonWrongDocument
	}
	doc, ok := presented.Documents[DocumentKey]
	if !ok {
		return zero, ReasonSchema
	}
	if doc.Number != DocumentNumber {
		return zero, ReasonWrongNumber
	}
	snippet, complete, cut := retrieval.SnippetWithBodyCut(rh, retrieval.SnippetCap)
	url := ""
	if rh.SourceURL != nil {
		url = *rh.SourceURL
	}
	if ph.Handle != retrieval.Handle(rh.Identity) || ph.Citation != rh.Citation || ph.Snippet != snippet || ph.Complete != complete || strings.Join(strings.Fields(rh.Number), " ") != doc.Number || strings.Join(strings.Fields(rh.Title), " ") != doc.Title || url != doc.SourceURL || rh.Stamp.Current != doc.Status.Current || rh.Stamp.StatusClass != doc.Status.Class || rh.Stamp.Basis != doc.Status.Basis || rh.Stamp.StatusBasis != doc.Status.StatusBasis {
		return zero, ReasonMismatch
	}
	if !cut || doc.TextSource != "source_tree" {
		return zero, ReasonNoProvenCut
	}
	key, anchor, err := retrieval.ParseHandle(ph.Handle)
	if err != nil || key != DocumentKey || anchor == "" || strings.Contains(anchor, "\n") || release == "" || ph.Citation == "" || ph.Snippet == "" || complete || !utf8.ValidString(ph.Snippet) || len([]rune(ph.Snippet)) > retrieval.SnippetCap || !validDate(doc.Issued) || !validDate(doc.EffectiveFrom) || !validDate(doc.EffectiveTo) {
		return zero, ReasonSchema
	}
	if !validUTF8(release, ph.Handle, ph.Doc, ph.Citation, ph.Snippet, doc.Number, doc.Status.Class, doc.Status.Basis, doc.Status.StatusBasis, doc.TextSource, doc.Title, doc.Type, doc.Issuer, doc.SourceURL) {
		return zero, ReasonSchema
	}
	output := PublicSampleResult{Release: release, Hits: []Hit{{Handle: ph.Handle, Doc: DocumentKey, Citation: ph.Citation, Snippet: ph.Snippet}}, Documents: Documents{Document: Document{Number: doc.Number, Status: Status{Current: doc.Status.Current, Class: doc.Status.Class, Basis: doc.Status.Basis, StatusBasis: doc.Status.StatusBasis}, TextSource: doc.TextSource, TextComplete: doc.TextComplete, Title: doc.Title, Type: doc.Type, Issuer: doc.Issuer, Issued: doc.Issued, EffectiveFrom: doc.EffectiveFrom, EffectiveTo: doc.EffectiveTo}}}
	if doc.SourceURL == CanonicalURL {
		output.Documents.Document.SourceURL = CanonicalURL
	}
	b, err := json.Marshal(output)
	if err != nil {
		return zero, ReasonSchema
	}
	if len(b) > BodyCap {
		return zero, ReasonBodyCap
	}
	return output, ""
}
func validUTF8(values ...string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) {
			return false
		}
	}
	return true
}

func validDate(d *retrieval.Date) bool {
	if d == nil {
		return true
	}
	_, err := time.Parse(time.DateOnly, d.String())
	return err == nil && d.Year >= 0 && d.Year <= 9999 && time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Year() == d.Year && time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Month() == d.Month && time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Day() == d.Day
}
