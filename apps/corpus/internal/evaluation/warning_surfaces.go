package evaluation

import (
	"context"
	"fmt"

	"danny.vn/trachuan/internal/retrieval"
)

type warningReader interface {
	Read(context.Context, retrieval.ReadRequest) (retrieval.ReadResult, error)
}

func readWarning(ctx context.Context, reader warningReader, release string, c WarningCase) (*WarningMatch, error) {
	key, anchor, err := retrieval.ParseHandle(c.Target)
	if err != nil || anchor == "" {
		return nil, fmt.Errorf("invalid warning target")
	}
	item := retrieval.ReadItem{Handle: c.Target}
	seen := map[string]bool{}
	for {
		response, readErr := reader.Read(ctx, retrieval.ReadRequest{Release: release, Items: []retrieval.ReadItem{item}})
		if readErr != nil {
			return nil, fmt.Errorf("read warning %s: %w", c.EntryID, readErr)
		}
		if response.Release != release || len(response.Items) != 1 || response.Items[0].Error != "" || response.Items[0].Doc != key {
			return nil, nil
		}
		page := response.Items[0]
		for _, unit := range page.Units {
			if unit.Kind == "provision" && unit.Anchor == anchor {
				return warningMatch(c, c.Target, unit.Warnings), nil
			}
		}
		if page.Next == "" {
			return nil, nil
		}
		if seen[page.Next] {
			return nil, fmt.Errorf("warning read cursor repeated")
		}
		seen[page.Next] = true
		item = retrieval.ReadItem{Next: page.Next}
	}
}

func warningMatch(c WarningCase, carrier string, warnings []retrieval.Warning) *WarningMatch {
	key, _, err := retrieval.ParseHandle(c.Acting)
	if err != nil {
		return nil
	}
	for _, w := range warnings {
		if w.Dir == "in" && w.Kind == c.RelationKind && w.Key == key && w.InRelease && w.Source == c.Acting && w.Target == c.Target && w.Exact {
			return &WarningMatch{Carrier: carrier, Dir: w.Dir, Kind: w.Kind, Key: w.Key, InRelease: w.InRelease, Source: w.Source, Target: w.Target, Exact: w.Exact}
		}
	}
	return nil
}

func matchWarningSearch(ctx context.Context, c WarningCase, response retrieval.SearchResult, scope func(context.Context, string, string) (bool, error)) (*WarningMatch, error) {
	for _, hits := range [][]retrieval.PresentedHit{response.Hits, response.NonCurrent} {
		for _, hit := range hits {
			match := warningMatch(c, hit.Handle, hit.Warnings)
			if match == nil {
				continue
			}
			ok, err := scope(ctx, c.Target, hit.Handle)
			if err != nil {
				return nil, fmt.Errorf("resolve warning scope: %w", err)
			}
			if ok {
				return match, nil
			}
		}
	}
	return nil, nil
}
