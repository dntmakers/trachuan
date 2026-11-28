package knowledge

import (
	"encoding/json"
	"fmt"
	"slices"

	"danny.vn/trachuan/internal/frameworks/businesskey"
	"danny.vn/trachuan/internal/lawkey"
	"danny.vn/trachuan/internal/releasechanges"
	"gopkg.in/yaml.v3"
)

type ControlKey struct {
	Code     string `json:"code"`
	Version  string `json:"version"`
	Citation string `json:"citation"`
}

type ControlRecord struct {
	ID          ControlKey  `json:"id"`
	ControlKind string      `json:"control_kind"`
	Parent      *ControlKey `json:"parent"`
	Path        []string    `json:"path"`
	Ordinal     int         `json:"ordinal"`
	Title       string      `json:"title"`
	Status      string      `json:"status"`
	Supersedes  *ControlKey `json:"supersedes"`
	Amends      *ControlKey `json:"amends"`
}

func ControlSHA256(record ControlRecord) (string, error) {
	if record.Path == nil {
		record.Path = []string{}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("encode control: %w", err)
	}
	canonical, err := releasechanges.CanonicalPayload(raw)
	if err != nil {
		return "", fmt.Errorf("canonicalize control: %w", err)
	}
	return Hash(canonical), nil
}

type ControlLawLink struct {
	ID                                           string
	Position                                     int
	Control                                      ControlKey
	TargetKey, TargetAnchor, RelationKind, Quote string
	Contract                                     int
	TargetDepends                                ProvisionDepends
	TargetOwnSHA256, ControlSHA256               string
	Reviewed                                     Reviewed
}

func ParseRelease(country string, data []byte) (File, error) {
	file, err := Parse(country, data)
	if err != nil {
		return File{}, err
	}
	if err = file.ValidateForRelease(); err != nil {
		return File{}, err
	}
	return file, nil
}

func (f File) ValidateForRelease() error {
	if len(f.ControlLawLinks) > 0 {
		return fmt.Errorf("control_law_link entries cannot be validated against a law release yet")
	}
	return nil
}

type controlLawIdentity struct {
	control                               ControlKey
	targetKey, targetAnchor, relationKind string
}

var controlLawTextSources = []string{"source_tree", "html_transcription", "pdf_text", "ocr", "none"}
var controlKeyValidator = businesskey.New()

func (p *parser) controlLaw(at string, m map[string]*yaml.Node) ControlLawLink {
	target := p.fields(at+".target", m["target"], "key", "anchor")
	claim := p.fields(at+".claim", m["claim"], "control", "relation_kind")
	control := p.fields(at+".claim.control", claim["control"], "code", "version", "citation")
	evidence := p.fields(at+".evidence", m["evidence"], "quote")
	depends := p.fields(at+".depends", m["depends"], "contract", "target", "target_own_sha256", "control_sha256")
	link := ControlLawLink{
		Control:   ControlKey{Code: p.text(at+".claim.control.code", control["code"]), Version: p.text(at+".claim.control.version", control["version"]), Citation: p.text(at+".claim.control.citation", control["citation"])},
		TargetKey: p.key(at+".target.key", target["key"]), TargetAnchor: p.anchor(at+".target.anchor", target["anchor"]),
		RelationKind: p.oneOf(at+".claim.relation_kind", claim["relation_kind"], []string{"related"}),
		Quote:        p.length(at+".evidence.quote", evidence["quote"], 10, 200), Contract: p.positive(at+".depends.contract", depends["contract"]),
		TargetDepends:   p.provision(at+".depends.target", depends["target"]),
		TargetOwnSHA256: p.hash(at+".depends.target_own_sha256", depends["target_own_sha256"]), ControlSHA256: p.hash(at+".depends.control_sha256", depends["control_sha256"]),
	}
	if !controlKeyValidator.Valid(link.Control.Code, link.Control.Version, link.Control.Citation) {
		p.add("%s.claim.control: invalid control identity", at)
	}
	if !lawkey.DocumentKey(link.TargetKey) {
		p.add("%s.target.key: invalid law document key", at)
	}
	if !slices.Contains(controlLawTextSources, link.TargetDepends.TextSource) {
		p.add("%s.depends.target.text_source: invalid text source", at)
	}
	return link
}

func (s *structure) controlLaw(at string, link ControlLawLink) {
	s.batch(at, link.Reviewed)
	identity := controlLawIdentity{link.Control, link.TargetKey, link.TargetAnchor, link.RelationKind}
	if s.controlLaws[identity] {
		s.p.add("%s: same identity as an earlier entry", at)
	}
	s.controlLaws[identity] = true
}
