package relate

import (
	"fmt"
	"sort"

	"danny.vn/trachuan/internal/frameworks/businesskey"
	"danny.vn/trachuan/internal/frameworks/parse"
	"danny.vn/trachuan/internal/frameworks/registry"
)

type SourceReader interface {
	ReadSource(version registry.Framework, file registry.File) ([]byte, error)
}

type DirectSource struct {
	Version VersionKey
	File    registry.File
	Catalog *parse.Result
	Data    []byte
}
type LawLink struct {
	From Key
	To   LawKey
}
type EvidenceCandidate struct {
	Key   PublicKey
	Value string
}
type AdmissionResult struct {
	Keys                 []AdmittedKey
	MappingDropped       map[VersionKey]int
	PrivateParserRejects map[VersionKey]int
	Evidence             []EvidenceCandidate
}
type AdmittedKey struct {
	Key   PublicKey
	Basis string
}

func Admit(reg *registry.Registry, catalogs map[string]*parse.Result, reader SourceReader, reviewed []byte, laws []LawLink) (*AdmissionResult, error) {
	sources, err := readDirectSources(reg, reader)
	if err != nil {
		return nil, err
	}
	verifiedCatalogs := make(map[string]*parse.Result, len(catalogs))
	for key, catalog := range catalogs {
		verifiedCatalogs[key] = catalog
	}
	for _, source := range sources {
		if source.File.Role == "catalog" {
			verifiedCatalogs[source.Version.Code+":"+source.Version.Version] = source.Catalog
		}
	}
	catalogs = verifiedCatalogs
	reviewedKeys, err := ReadReviewedKeys(reg, catalogs, reviewed)
	if err != nil {
		return nil, err
	}
	result := &AdmissionResult{MappingDropped: map[VersionKey]int{}, PrivateParserRejects: map[VersionKey]int{}}
	seen := map[string]string{}
	add := func(k PublicKey, basis string) error {
		id := fmt.Sprintf("%q", []string{k.From.Code, k.From.Version, k.From.Citation, k.Kind(), targetID(k), k.Relation, k.Source})
		if prior, ok := seen[id]; ok {
			if prior != basis {
				return fmt.Errorf("conflicting admission basis")
			}
			return nil
		}
		seen[id] = basis
		result.Keys = append(result.Keys, AdmittedKey{k, basis})
		return nil
	}
	type sourceOwner struct {
		source  string
		version VersionKey
	}
	approved := map[sourceOwner]bool{}
	directPayloads := map[ReviewedKey]bool{}
	evidencePayloads := map[ReviewedKey]string{}
	for _, source := range sources {
		v, err := sourceVersion(reg, source)
		if err != nil {
			return nil, err
		}
		name := ""
		switch {
		case source.File.Role == "catalog" && v.Parser == "nist-oscal-800-53" && source.File.Format == "oscal-json":
			name = "nist-oscal"
		case source.File.Role == "catalog" && v.Parser == "nist-csf-2-xlsx" && source.File.Format == "xlsx":
			name = "nist-csf"
		case source.File.Role == "mapping workbook" && source.File.Parser == "nist-olir" && source.File.Format == "xlsx":
			name = "nist-olir"
		default:
			return nil, fmt.Errorf("unknown direct source")
		}

		owner := VersionKey{v.Code, v.Version}
		if name == "nist-csf" {
			approved[sourceOwner{"nist-csf-withdrawals", owner}] = true
			approved[sourceOwner{"nist-csf-references", owner}] = true
		} else {
			approved[sourceOwner{name, owner}] = true
		}
		var evidence []string
		if name == "nist-olir" {
			if v.Code != "nist80053" || v.Version != "r5" {
				return nil, fmt.Errorf("invalid OLIR profile")
			}
			var dropped int
			source.Catalog, evidence, dropped, err = admissionOLIR(source.Data)
			result.PrivateParserRejects[VersionKey{v.Code, v.Version}] += dropped
			if err != nil {
				return nil, err
			}
		}
		if source.Catalog == nil || len(source.Catalog.Nodes) == 0 {
			return nil, fmt.Errorf("empty registered source")
		}
		for nodeIndex, node := range source.Catalog.Nodes {
			result.PrivateParserRejects[VersionKey{v.Code, v.Version}] += node.DroppedMappings
			for _, mapping := range node.Mappings {
				kind := name
				if name == "nist-csf" {
					if mapping.Relation == "related" {
						kind = "nist-csf-references"
					} else {
						kind = "nist-csf-withdrawals"
					}
				}
				if !directRelation(kind, mapping.Relation) {
					result.MappingDropped[VersionKey{v.Code, v.Version}]++
					continue
				}
				from := Key{v.Code, v.Version, node.Citation}
				code, version := v.Code, v.Version
				if kind == "nist-oscal" || kind == "nist-csf-withdrawals" {
					if mapping.TargetFramework != v.Code || mapping.TargetVersion != v.Version {
						result.MappingDropped[VersionKey{v.Code, v.Version}]++
						continue
					}
				} else if kind == "nist-olir" {
					code, version = "iso27001", "2022"
				} else {
					code, version, _ = resolveVersion(reg, mapping.TargetFramework, mapping.TargetVersion)
				}
				to := Key{code, version, lookupCitation(reg, Key{code, version, mapping.TargetCitation})}
				canonicalize := func(k *Key) {
					if result := catalogs[k.Code+":"+k.Version]; result != nil {
						for _, n := range result.Nodes {
							if norm(lookupCitation(reg, Key{k.Code, k.Version, n.Citation})) == norm(lookupCitation(reg, *k)) {
								k.Citation = n.Citation
								return
							}
						}
					}
				}
				canonicalize(&from)
				canonicalize(&to)
				valid := businesskey.New()
				known := false
				for _, f := range reg.Frameworks {
					if f.Code == to.Code {
						known = true
						break
					}
				}
				if !known || !valid.Valid(from.Code, from.Version, from.Citation) || !valid.Valid(to.Code, to.Version, to.Citation) || from == to {
					result.MappingDropped[VersionKey{v.Code, v.Version}]++
					continue
				}
				k := PublicKey{From: from, Control: &to, Relation: mapping.Relation, Source: kind}
				if k.Kind() == "cross_version" && (kind == "nist-oscal" || mapping.Relation == "moved-to" || mapping.Relation == "incorporated-into") {
					result.MappingDropped[VersionKey{v.Code, v.Version}]++
					continue
				}
				payloadKey := ReviewedKey{From: from, To: to, Relation: mapping.Relation, Source: kind}
				if prior, ok := directPayloads[payloadKey]; ok && prior != mapping.Unresolved {
					return nil, fmt.Errorf("conflicting direct mapping")
				}
				directPayloads[payloadKey] = mapping.Unresolved
				if name == "nist-olir" {
					value := evidence[nodeIndex]
					if prior, ok := evidencePayloads[payloadKey]; ok && prior != value {
						return nil, fmt.Errorf("conflicting OLIR evidence")
					}
					if _, ok := evidencePayloads[payloadKey]; !ok && value != "" {
						result.Evidence = append(result.Evidence, EvidenceCandidate{Key: k, Value: value})
					}
					evidencePayloads[payloadKey] = value
				}
				if err := add(k, "direct"); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, row := range reviewedKeys {
		if approved[sourceOwner{row.Source, VersionKey{row.From.Code, row.From.Version}}] {
			continue
		}
		to := row.To
		if err := add(PublicKey{From: row.From, Control: &to, Relation: row.Relation, Source: row.Source}, "reviewed_key"); err != nil {
			return nil, err
		}
	}
	lawClaims := map[LawLink]bool{}
	for _, law := range laws {
		if lawClaims[law] {
			return nil, fmt.Errorf("duplicate law claim")
		}
		lawClaims[law] = true
		if !canonicalReviewed(reg, catalogs, law.From) || !validLawKey(law.To) {
			return nil, fmt.Errorf("invalid law link")
		}
		to := law.To
		if err := add(PublicKey{From: law.From, Law: &to, Relation: "related", Source: "reviewed-knowledge"}, "reviewed_law"); err != nil {
			return nil, err
		}
	}
	sort.Slice(result.Keys, func(i, j int) bool { return result.Keys[i].Key.Less(result.Keys[j].Key) })
	sort.Slice(result.Evidence, func(i, j int) bool { return result.Evidence[i].Key.Less(result.Evidence[j].Key) })
	return result, nil
}

func targetID(k PublicKey) string {
	if k.Control != nil {
		return fmt.Sprintf("%q", []string{k.Control.Code, k.Control.Version, k.Control.Citation})
	}
	return fmt.Sprintf("%q", []string{k.Law.Country, k.Law.Key, k.Law.Anchor})
}
func directRelation(source, relation string) bool {
	switch source {
	case "nist-oscal":
		return relation == "related" || relation == "moved-to" || relation == "incorporated-into"
	case "nist-csf-references", "nist-olir":
		return relation == "related"
	case "nist-csf-withdrawals":
		return relation == "moved-to" || relation == "incorporated-into"
	}
	return false
}
func sourceVersion(reg *registry.Registry, source DirectSource) (registry.Framework, error) {
	for _, v := range reg.Frameworks {
		if v.Code == source.Version.Code && v.Version == source.Version.Version {
			for _, file := range v.Files {
				if file == source.File {
					return v, nil
				}
			}
			break
		}
	}
	return registry.Framework{}, fmt.Errorf("unregistered source file")
}

func sourceApproved(version registry.Framework) bool { return version.Access == "free" }

func registeredDirectSources(reg *registry.Registry) ([]DirectSource, error) {
	var sources []DirectSource
	for _, version := range reg.Frameworks {
		if sourceApproved(version) && (version.Parser == "nist-csf-2-xlsx" || version.Parser == "nist-oscal-800-53") {
			selected, err := version.SelectedSource()
			if err != nil || (version.Parser == "nist-csf-2-xlsx" && selected.Format != "xlsx") || (version.Parser == "nist-oscal-800-53" && selected.Format != "oscal-json") {
				return nil, fmt.Errorf("missing registered catalog")
			}
		}
		for _, file := range version.Files {
			if file.Role == "mapping workbook" {
				if file.Format != "xlsx" {
					return nil, fmt.Errorf("invalid mapping workbook format")
				}
				switch file.Parser {
				case "nist-olir", "cis-iso27001", "cis-nistcsf", "cis-nist80053":
				default:
					return nil, fmt.Errorf("unknown registered source")
				}
			}
			if file.Role == "catalog" && (version.Parser == "nist-csf-2-xlsx" && file.Format == "xlsx" || version.Parser == "nist-oscal-800-53" && file.Format == "oscal-json") || file.Role == "mapping workbook" && file.Parser == "nist-olir" && file.Format == "xlsx" {
				sources = append(sources, DirectSource{Version: VersionKey{version.Code, version.Version}, File: file})
			}
		}
	}
	return sources, nil
}
