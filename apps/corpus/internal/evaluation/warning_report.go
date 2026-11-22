package evaluation

import "danny.vn/trachuan/internal/retrieval"

type WarningMatch struct {
	Carrier   string `json:"carrier"`
	Dir       string `json:"dir"`
	Kind      string `json:"kind"`
	Key       string `json:"key"`
	InRelease bool   `json:"in_release"`
	Source    string `json:"source"`
	Target    string `json:"target"`
	Exact     bool   `json:"exact"`
}

type WarningSearch struct {
	Mode   retrieval.Mode   `json:"mode"`
	Limit  int              `json:"limit"`
	Detail retrieval.Detail `json:"detail"`
	Match  *WarningMatch    `json:"match"`
}

type WarningResult struct {
	EntryID string          `json:"entry_id"`
	Outcome string          `json:"outcome"`
	Reason  string          `json:"reason,omitempty"`
	Search  []WarningSearch `json:"search"`
	Read    *WarningMatch   `json:"read"`
}

func finishWarnings(r *Report, cases []WarningResult, modes []retrieval.Mode) {
	r.WarningCases = cases
	if len(cases) == 0 {
		r.WarningCases = []WarningResult{}
		r.NotRun["warning_cases"] = "not_run"
		return
	}
	delete(r.NotRun, "warning_cases")
	for _, c := range cases {
		if c.Outcome != "applied" && c.Outcome != "redundant" || c.Read == nil || len(c.Search) != len(modes)*4 {
			r.QualityPass = false
			continue
		}
		i := 0
		for _, mode := range modes {
			for _, limit := range []int{8, 20} {
				for _, detail := range []retrieval.Detail{retrieval.DetailCompact, retrieval.DetailFull} {
					s := c.Search[i]
					if s.Mode != mode || s.Limit != limit || s.Detail != detail || s.Match == nil {
						r.QualityPass = false
					}
					i++
				}
			}
		}
	}
}
