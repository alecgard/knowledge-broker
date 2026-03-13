package query

import (
	"sort"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

const rrfK = 60 // standard RRF constant

// rankedList is a list of fragments in rank order (index 0 = best).
type rankedList []model.SourceFragment

// mergeRRF combines multiple ranked lists using Reciprocal Rank Fusion.
// Returns fragments sorted by descending RRF score, deduplicated by fragment ID.
func mergeRRF(lists []rankedList, limit int) []model.SourceFragment {
	if len(lists) == 0 {
		return nil
	}
	if len(lists) == 1 {
		if len(lists[0]) > limit {
			return lists[0][:limit]
		}
		return lists[0]
	}

	scores := make(map[string]float64)
	fragByID := make(map[string]model.SourceFragment)

	for _, list := range lists {
		for rank, frag := range list {
			scores[frag.ID] += 1.0 / float64(rrfK+rank+1)
			fragByID[frag.ID] = frag
		}
	}

	type scored struct {
		id    string
		score float64
	}
	entries := make([]scored, 0, len(scores))
	for id, s := range scores {
		entries = append(entries, scored{id, s})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].score > entries[j].score
	})

	if len(entries) > limit {
		entries = entries[:limit]
	}

	result := make([]model.SourceFragment, len(entries))
	for i, e := range entries {
		result[i] = fragByID[e.id]
	}
	return result
}
