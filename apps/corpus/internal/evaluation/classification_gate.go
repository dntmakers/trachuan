package evaluation

type ClassificationGate struct {
	Status                  string                `json:"status"`
	ClassificationSetSHA256 string                `json:"classification_set_sha256"`
	PolicySHA256            string                `json:"policy_sha256"`
	TaxonomyVersion         string                `json:"taxonomy_version"`
	LLMRows                 int64                 `json:"llm_rows"`
	Slices                  []ClassificationSlice `json:"slices,omitempty"`
	Reasons                 []string              `json:"reasons,omitempty"`
}

type ClassificationSlice struct {
	Slice             string                   `json:"slice"`
	Status            string                   `json:"status"`
	LabelsSHA256      string                   `json:"labels_sha256"`
	Entries           int                      `json:"entries"`
	InvalidEntries    int                      `json:"invalid_entries"`
	ExcludedFragments []ClassificationFragment `json:"excluded_fragments"`
	Level2            *ClassificationLevel2    `json:"level_2,omitempty"`
	Reasons           []string                 `json:"reasons,omitempty"`
}

type ClassificationFragment struct {
	DocumentKey string `json:"document_key"`
	Anchor      string `json:"anchor"`
}

type ClassificationLevel2 struct {
	All         map[string]ClassificationCount `json:"all"`
	NonFragment map[string]ClassificationCount `json:"non_fragment"`
}

type ClassificationCount struct {
	Hits  int `json:"hits"`
	Total int `json:"total"`
}
