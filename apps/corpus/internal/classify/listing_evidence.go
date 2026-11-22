package classify

import (
	"fmt"
	"math"
	"time"
)

type listingCounts struct {
	Processing *int64 `json:"processing"`
	Succeeded  *int64 `json:"succeeded"`
	Errored    *int64 `json:"errored"`
	Canceled   *int64 `json:"canceled"`
	Expired    *int64 `json:"expired"`
}

type listingBatch struct {
	ID        string         `json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	Status    string         `json:"processing_status"`
	Counts    *listingCounts `json:"request_counts"`
}

type listingPage struct {
	Data    []listingBatch `json:"data"`
	HasMore *bool          `json:"has_more"`
	FirstID *string        `json:"first_id"`
	LastID  *string        `json:"last_id"`
}

type listingPager struct {
	pages   int
	more    bool
	cursors map[string]bool
	ids     map[string]bool
}

func (p *listingPager) accept(raw string) (listingPage, error) {
	page, err := decodeListingPage(raw)
	if err != nil {
		return page, err
	}
	if p.pages > 0 && !p.more {
		return page, fmt.Errorf("provider listing continues after final page")
	}
	if p.ids == nil {
		p.ids = map[string]bool{}
	}
	for _, item := range page.Data {
		if item.ID == "" || p.ids[item.ID] {
			return page, fmt.Errorf("duplicate or empty listed batch identity")
		}
		p.ids[item.ID] = true
	}
	if len(page.Data) == 0 {
		if *page.HasMore || (page.FirstID != nil && *page.FirstID != "") || (page.LastID != nil && *page.LastID != "") {
			return page, fmt.Errorf("empty listing page has invalid cursor")
		}
	} else {
		if page.FirstID == nil || page.LastID == nil || *page.FirstID != page.Data[0].ID || *page.LastID != page.Data[len(page.Data)-1].ID || *page.LastID == "" {
			return page, fmt.Errorf("invalid provider listing cursor")
		}
		if p.cursors == nil {
			p.cursors = map[string]bool{}
		}
		if p.cursors[*page.LastID] {
			return page, fmt.Errorf("provider listing cursor repeats")
		}
		p.cursors[*page.LastID] = true
	}
	p.pages++
	p.more = *page.HasMore
	return page, nil
}

func (p listingPager) complete() bool { return p.pages > 0 && !p.more }

func (c *listingCounts) total() (int64, error) {
	if c == nil || c.Processing == nil || c.Succeeded == nil || c.Errored == nil || c.Canceled == nil || c.Expired == nil || *c.Processing != 0 {
		return 0, fmt.Errorf("missing or unfinished batch request counts")
	}
	var n int64
	for _, v := range []*int64{c.Succeeded, c.Errored, c.Canceled, c.Expired} {
		if *v < 0 || *v > math.MaxInt64-n {
			return 0, fmt.Errorf("invalid batch request count")
		}
		n += *v
	}
	return n, nil
}

func validProviderCustomID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
