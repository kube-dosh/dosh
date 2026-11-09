import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useCursorPagination } from "@/hooks/use-cursor-pagination";

describe("useCursorPagination", () => {
  it("keeps a cursor stack for next and previous navigation", () => {
    const { result } = renderHook(() => useCursorPagination("pods|default|", 20));

    expect(result.current.page).toBe(1);
    expect(result.current.cursor).toBeUndefined();
    expect(result.current.canPrevious).toBe(false);

    act(() => result.current.next("cursor-2"));
    expect(result.current.page).toBe(2);
    expect(result.current.cursor).toBe("cursor-2");
    expect(result.current.canPrevious).toBe(true);

    act(() => result.current.next("cursor-3"));
    expect(result.current.page).toBe(3);
    expect(result.current.cursor).toBe("cursor-3");

    act(() => result.current.previous());
    expect(result.current.page).toBe(2);
    expect(result.current.cursor).toBe("cursor-2");
  });

  it("supports page-number-only pagination and resets when filters change", () => {
    const { result, rerender } = renderHook(({ key }) => useCursorPagination(key, 20), {
      initialProps: { key: "events|default|" },
    });

    act(() => result.current.next());
    expect(result.current.page).toBe(2);
    expect(result.current.cursor).toBeUndefined();

    rerender({ key: "events|prod|" });
    expect(result.current.page).toBe(1);
    expect(result.current.cursor).toBeUndefined();
    expect(result.current.canPrevious).toBe(false);
  });
});
