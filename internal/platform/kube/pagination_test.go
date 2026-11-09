package kube

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type paginationChunk struct {
	items     []int
	next      string
	remaining *int64
}

func remaining(value int64) *int64 { return &value }

func paginationFetcher(chunks map[string]paginationChunk, seen *[]string) func(metav1.ListOptions) ([]int, metav1.ListMeta, error) {
	return func(options metav1.ListOptions) ([]int, metav1.ListMeta, error) {
		if seen != nil {
			*seen = append(*seen, options.Continue)
		}
		chunk := chunks[options.Continue]
		return chunk.items, metav1.ListMeta{Continue: chunk.next, RemainingItemCount: chunk.remaining}, nil
	}
}

func identity(value int) int { return value }

func TestListPageUsesOpaqueCursorAndExactTotals(t *testing.T) {
	chunks := map[string]paginationChunk{
		"":   {items: []int{1, 2}, next: "p2", remaining: remaining(3)},
		"p2": {items: []int{3, 4}, next: "p3", remaining: remaining(1)},
		"p3": {items: []int{5}},
	}
	fetch := paginationFetcher(chunks, nil)

	first, err := listPage(context.Background(), domain.PageRequest{Number: 1, Size: 2}, false, fetch, identity, nil)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Items) != 2 || first.Items[0] != 1 || first.Items[1] != 2 {
		t.Fatalf("first items = %#v", first.Items)
	}
	if first.Total != 5 || !first.TotalKnown || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page metadata = %#v", first)
	}

	second, err := listPage(context.Background(), domain.PageRequest{Number: 2, Size: 2, Cursor: first.NextCursor}, false, fetch, identity, nil)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 2 || second.Items[0] != 3 || second.Items[1] != 4 || second.Total != 5 || !second.TotalKnown {
		t.Fatalf("second page = %#v", second)
	}

	third, err := listPage(context.Background(), domain.PageRequest{Number: 3, Size: 2, Cursor: second.NextCursor}, false, fetch, identity, nil)
	if err != nil {
		t.Fatalf("third page: %v", err)
	}
	if len(third.Items) != 1 || third.Items[0] != 5 || third.HasMore || third.Total != 5 || !third.TotalKnown {
		t.Fatalf("third page = %#v", third)
	}
}

func TestListPageFiltersAcrossBoundedChunks(t *testing.T) {
	chunks := map[string]paginationChunk{
		"":   {items: []int{1, 2}, next: "c3"},
		"c3": {items: []int{3, 4}, next: "c5"},
		"c5": {items: []int{5, 6}, next: "c7"},
		"c7": {items: []int{7, 8}, next: "c9"},
		"c9": {items: []int{9, 10}},
	}
	fetch := paginationFetcher(chunks, nil)
	odd := func(value int) bool { return value%2 == 1 }

	first, err := listPage(context.Background(), domain.PageRequest{Number: 1, Size: 2}, true, fetch, identity, odd)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Items) != 2 || first.Items[0] != 1 || first.Items[1] != 3 {
		t.Fatalf("first filtered items = %#v", first.Items)
	}
	if first.TotalKnown || first.Total != 2 || !first.HasMore {
		t.Fatalf("first filtered metadata = %#v", first)
	}

	seen := make([]string, 0)
	second, err := listPage(context.Background(), domain.PageRequest{Number: 2, Size: 2, Cursor: first.NextCursor}, true, paginationFetcher(chunks, &seen), identity, odd)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 2 || second.Items[0] != 5 || second.Items[1] != 7 || !second.HasMore || second.TotalKnown {
		t.Fatalf("second filtered page = %#v", second)
	}
	if len(seen) == 0 || seen[0] != "c5" {
		t.Fatalf("second page did not resume from cursor: %#v", seen)
	}

	third, err := listPage(context.Background(), domain.PageRequest{Number: 3, Size: 2, Cursor: second.NextCursor}, true, fetch, identity, odd)
	if err != nil {
		t.Fatalf("third page: %v", err)
	}
	if len(third.Items) != 1 || third.Items[0] != 9 || third.HasMore || !third.TotalKnown || third.Total != 5 {
		t.Fatalf("third filtered page = %#v", third)
	}
}

func TestListPageRejectsMalformedCursor(t *testing.T) {
	_, err := listPage(context.Background(), domain.PageRequest{Number: 1, Size: 20, Cursor: "not-a-cursor"}, false,
		paginationFetcher(nil, nil), identity, nil)
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}
