package domain

import (
	"errors"
	"testing"
)

func TestPaginateSliceWithCursorNavigatesBoundedCollections(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	first, err := PaginateSliceWithCursor(items, PageRequest{Number: 1, Size: 2})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Items) != 2 || first.Items[0] != 1 || !first.HasMore || first.NextCursor == "" || first.Total != 5 || !first.TotalKnown {
		t.Fatalf("first page = %#v", first)
	}

	second, err := PaginateSliceWithCursor(items, PageRequest{Number: 2, Size: 2, Cursor: first.NextCursor})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 2 || second.Items[0] != 3 || !second.HasMore || second.NextCursor == "" {
		t.Fatalf("second page = %#v", second)
	}

	third, err := PaginateSliceWithCursor(items, PageRequest{Number: 3, Size: 2, Cursor: second.NextCursor})
	if err != nil {
		t.Fatalf("third page: %v", err)
	}
	if len(third.Items) != 1 || third.Items[0] != 5 || third.HasMore || third.NextCursor != "" {
		t.Fatalf("third page = %#v", third)
	}
}

func TestPaginateSliceWithCursorRejectsMalformedCursor(t *testing.T) {
	_, err := PaginateSliceWithCursor([]int{1}, PageRequest{Number: 1, Size: 20, Cursor: "not-a-cursor"})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
}
