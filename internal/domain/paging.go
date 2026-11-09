package domain

// PageRequest is transport-neutral pagination input. Cursor is an opaque
// continuation value owned by the repository; application layers must not
// inspect or construct it. Number is one-based and is used for display and for
// compatibility with clients that still navigate by page number.
type PageRequest struct {
	Number int
	Size   int
	Cursor string
}

// PageResult is a bounded slice of a collection. Total is exact only when
// TotalKnown is true. This distinction is necessary for filtered scans, where
// computing an exact count would require reading the entire Kubernetes
// collection and would recreate the unbounded-memory problem pagination is
// meant to avoid.
type PageResult[T any] struct {
	Items      []T
	Total      int
	TotalKnown bool
	HasMore    bool
	NextCursor string
	Truncated  bool
	Warnings   []string
}

// PaginateSlice applies page navigation to an already materialized collection.
// It is intended for deliberately bounded sources such as the in-memory audit
// history and Kubernetes event windows; repository adapters should prefer
// cursor pagination for unbounded collections.
func PaginateSlice[T any](items []T, page PageRequest) PageResult[T] {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < 1 {
		page.Size = 20
	}
	total := len(items)
	start := (page.Number - 1) * page.Size
	if start > total {
		start = total
	}
	end := start + page.Size
	if end > total {
		end = total
	}
	return PageResult[T]{
		Items:      items[start:end],
		Total:      total,
		TotalKnown: true,
		HasMore:    end < total,
	}
}
