package query

import (
	"testing"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

func frag(id string) model.SourceFragment {
	return model.SourceFragment{ID: id}
}

func TestMergeRRF_Empty(t *testing.T) {
	result := mergeRRF(nil, 10)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}

func TestMergeRRF_SingleList(t *testing.T) {
	list := rankedList{frag("a"), frag("b"), frag("c")}
	result := mergeRRF([]rankedList{list}, 10)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0].ID != "a" {
		t.Fatalf("expected a first, got %s", result[0].ID)
	}
}

func TestMergeRRF_SingleListWithLimit(t *testing.T) {
	list := rankedList{frag("a"), frag("b"), frag("c")}
	result := mergeRRF([]rankedList{list}, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
}

func TestMergeRRF_Deduplication(t *testing.T) {
	list1 := rankedList{frag("a"), frag("b")}
	list2 := rankedList{frag("b"), frag("a")}
	result := mergeRRF([]rankedList{list1, list2}, 10)
	if len(result) != 2 {
		t.Fatalf("expected 2 unique fragments, got %d", len(result))
	}
}

func TestMergeRRF_OverlappingBoosted(t *testing.T) {
	// Fragment "a" appears in both lists at rank 0 — it should score highest.
	list1 := rankedList{frag("a"), frag("b")}
	list2 := rankedList{frag("a"), frag("c")}
	result := mergeRRF([]rankedList{list1, list2}, 10)
	if result[0].ID != "a" {
		t.Fatalf("expected 'a' (appears in both) to rank first, got %s", result[0].ID)
	}
}

func TestMergeRRF_DisjointLists(t *testing.T) {
	list1 := rankedList{frag("a"), frag("b")}
	list2 := rankedList{frag("c"), frag("d")}
	result := mergeRRF([]rankedList{list1, list2}, 10)
	if len(result) != 4 {
		t.Fatalf("expected 4, got %d", len(result))
	}
}

func TestMergeRRF_LimitApplied(t *testing.T) {
	list1 := rankedList{frag("a"), frag("b"), frag("c")}
	list2 := rankedList{frag("d"), frag("e"), frag("f")}
	result := mergeRRF([]rankedList{list1, list2}, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
}

func TestMergeRRF_EmptyLists(t *testing.T) {
	result := mergeRRF([]rankedList{{}, {}}, 10)
	if len(result) != 0 {
		t.Fatalf("expected empty, got %d", len(result))
	}
}
