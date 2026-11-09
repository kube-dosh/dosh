import { useCallback, useState } from "react";
import { ApiError } from "@/api/client";
import type { ListMeta } from "@/api/types";
import type { ServerPagination } from "@/components/data/data-table";

interface PagerState {
  key: string;
  cursors: string[];
  index: number;
}

export interface CursorPagination {
  cursor?: string;
  page: number;
  pageSize: number;
  canPrevious: boolean;
  next: (nextCursor?: string) => void;
  previous: () => void;
  reset: () => void;
}

// useCursorPagination keeps cursor history on the client. The cursor stack is
// derived from the current filter key so URL back/forward and filter changes
// never reuse a token from a different query.
export function useCursorPagination(resetKey: string, pageSize = 20): CursorPagination {
  const [state, setState] = useState<PagerState>({ key: resetKey, cursors: [""], index: 0 });
  const current = state.key === resetKey ? state : { key: resetKey, cursors: [""], index: 0 };
  const cursor = current.index > 0 ? current.cursors[current.index] || undefined : undefined;

  const next = useCallback((nextCursor?: string) => {
    setState((previous) => {
      const base = previous.key === resetKey ? previous : { key: resetKey, cursors: [""], index: 0 };
      const cursors = [...base.cursors.slice(0, base.index + 1), nextCursor ?? ""];
      return { key: resetKey, cursors, index: base.index + 1 };
    });
  }, [resetKey]);

  const previous = useCallback(() => {
    setState((previousState) => {
      const base = previousState.key === resetKey ? previousState : { key: resetKey, cursors: [""], index: 0 };
      return { ...base, index: Math.max(0, base.index - 1) };
    });
  }, [resetKey]);

  const reset = useCallback(() => {
    setState({ key: resetKey, cursors: [""], index: 0 });
  }, [resetKey]);

  return {
    cursor,
    page: current.index + 1,
    pageSize,
    canPrevious: current.index > 0,
    next,
    previous,
    reset,
  };
}

export function cursorPaginationProps(pager: CursorPagination, meta: ListMeta | undefined, loaded: number): ServerPagination {
  return {
    page: pager.page,
    pageSize: pager.pageSize,
    total: meta?.total ?? loaded,
    totalKnown: meta?.totalKnown ?? false,
    hasMore: meta?.hasMore ?? false,
    canPrevious: pager.canPrevious,
    onPrevious: pager.previous,
    onNext: () => pager.next(meta?.nextCursor),
    warnings: meta?.warnings,
  };
}

export function retryListQuery(pager: CursorPagination, error: unknown, refetch: () => unknown): void {
  if (error instanceof ApiError && error.code === "stale_cursor") {
    pager.reset();
    return;
  }
  void refetch();
}
