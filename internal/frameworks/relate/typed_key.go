package relate

import (
	"strings"

	"danny.vn/trachuan/internal/lawkey"
)

type LawKey struct {
	Country string `json:"country"`
	Key     string `json:"key"`
	Anchor  string `json:"anchor"`
}

type PublicKey struct {
	From     Key     `json:"from"`
	Control  *Key    `json:"control,omitempty"`
	Law      *LawKey `json:"law_provision,omitempty"`
	Relation string  `json:"relation"`
	Source   string  `json:"source"`
}

func (k PublicKey) Kind() string {
	if k.Law != nil && k.Control == nil {
		return "control_law"
	}
	if k.Control == nil || k.Law != nil {
		return ""
	}
	if k.From.Code == k.Control.Code && k.From.Version == k.Control.Version {
		return "within_version"
	}
	return "cross_version"
}

func (k PublicKey) Less(other PublicKey) bool {
	a := []string{k.From.Code, k.From.Version, k.From.Citation}
	b := []string{other.From.Code, other.From.Version, other.From.Citation}
	target := func(k PublicKey) []string {
		if k.Control != nil {
			return []string{"control", k.Control.Code, k.Control.Version, k.Control.Citation, k.Relation, k.Source}
		}
		if k.Law != nil {
			return []string{"law_provision", k.Law.Country, k.Law.Key, k.Law.Anchor, k.Relation, k.Source}
		}
		return []string{"", "", "", "", k.Relation, k.Source}
	}
	a = append(a, target(k)...)
	b = append(b, target(other)...)
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func validLawKey(k LawKey) bool {
	return k.Country == "vn" && lawkey.DocumentKey(k.Key) && !strings.Contains(k.Anchor, "~") && lawkey.PlainAnchor(k.Anchor)
}
