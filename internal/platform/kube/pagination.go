package kube

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"dosh/internal/domain"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	minimumPageSize = 1
	maximumPageSize = 200
	maxScanChunks   = 50
)

type cursorPayload struct {
	Continue   string `json:"c,omitempty"`
	Total      int    `json:"t,omitempty"`
	TotalKnown bool   `json:"k,omitempty"`
}

func normalizePage(page domain.PageRequest) domain.PageRequest {
	if page.Number < 1 {
		page.Number = 1
	}
	if page.Size < minimumPageSize {
		page.Size = 20
	}
	if page.Size > maximumPageSize {
		page.Size = maximumPageSize
	}
	return page
}

func encodeCursor(payload cursorPayload) (string, error) {
	if payload.Continue == "" {
		return "", nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeCursor(value string) (cursorPayload, error) {
	if value == "" {
		return cursorPayload{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursorPayload{}, fmt.Errorf("%w: cursor is malformed", domain.ErrInvalid)
	}
	var payload cursorPayload
	if err := json.Unmarshal(decoded, &payload); err != nil || payload.Continue == "" {
		return cursorPayload{}, fmt.Errorf("%w: cursor is malformed", domain.ErrInvalid)
	}
	return payload, nil
}

func slicePage[T any](items []T, page domain.PageRequest) domain.PageResult[T] {
	return domain.PaginateSlice(items, normalizePage(page))
}

// listPage reads Kubernetes collections in bounded chunks. The Kubernetes
// continue token never crosses the infrastructure boundary because it is
// wrapped in an opaque cursor. Filtering happens while chunks are streamed, so
// memory usage is proportional to the requested page rather than cluster size.
func listPage[TIn any, TOut any](
	ctx context.Context,
	page domain.PageRequest,
	filtered bool,
	fetch func(metav1.ListOptions) ([]TIn, metav1.ListMeta, error),
	convert func(TIn) TOut,
	match func(TOut) bool,
) (domain.PageResult[TOut], error) {
	return listPageWithFinalize(ctx, page, filtered, fetch, convert, match, nil)
}

func listPageWithFinalize[TIn any, TOut any](
	ctx context.Context,
	page domain.PageRequest,
	filtered bool,
	fetch func(metav1.ListOptions) ([]TIn, metav1.ListMeta, error),
	convert func(TIn) TOut,
	match func(TOut) bool,
	finalize func(TOut) TOut,
) (domain.PageResult[TOut], error) {
	page = normalizePage(page)
	state, err := decodeCursor(page.Cursor)
	if err != nil {
		return domain.PageResult[TOut]{}, err
	}

	token := state.Continue
	total := state.Total
	totalKnown := state.TotalKnown
	baseMatched := total
	skip := 0
	if page.Cursor == "" && page.Number > 1 {
		skip = (page.Number - 1) * page.Size
	}

	items := make([]TOut, 0, page.Size)
	seenMatches := 0
	firstChunk := true
	chunkCount := 0
	truncated := false
	warnings := make([]string, 0, 1)

	for len(items) < page.Size {
		if token == "" && !firstChunk {
			break
		}
		if chunkCount >= maxScanChunks {
			truncated = true
			warnings = append(warnings, "已达到单次分页扫描上限，请使用更精确的搜索条件")
			break
		}
		chunkCount++
		raw, meta, fetchErr := fetch(metav1.ListOptions{Limit: int64(page.Size), Continue: token})
		if fetchErr != nil {
			return domain.PageResult[TOut]{}, fetchErr
		}
		if firstChunk && page.Cursor == "" && !filtered {
			if meta.RemainingItemCount != nil {
				total = len(raw) + int(*meta.RemainingItemCount)
				totalKnown = true
			} else if meta.Continue == "" {
				total = len(raw)
				totalKnown = true
			}
		}
		for _, rawItem := range raw {
			item := convert(rawItem)
			if match != nil && !match(item) {
				continue
			}
			seenMatches++
			if skip > 0 {
				skip--
				continue
			}
			if len(items) < page.Size {
				if finalize != nil {
					item = finalize(item)
				}
				items = append(items, item)
			}
		}
		firstChunk = false
		token = meta.Continue
		if token == "" {
			if filtered || !totalKnown {
				total = baseMatched + seenMatches
				totalKnown = true
			}
			break
		}
	}

	if filtered && !totalKnown {
		total = baseMatched + seenMatches
	}
	nextCursor, cursorErr := encodeCursor(cursorPayload{Continue: token, Total: total, TotalKnown: totalKnown})
	if cursorErr != nil {
		return domain.PageResult[TOut]{}, cursorErr
	}
	return domain.PageResult[TOut]{
		Items:      items,
		Total:      total,
		TotalKnown: totalKnown,
		HasMore:    token != "",
		NextCursor: nextCursor,
		Truncated:  truncated,
		Warnings:   warnings,
	}, nil
}
