import type { ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ThemeProvider, THEME_STORAGE_KEY, useTheme } from "@/lib/theme";

function wrapper({ children }: { children: ReactNode }) {
  return <ThemeProvider>{children}</ThemeProvider>;
}

function installMatchMedia(matches: boolean) {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }));
}

describe("theme", () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.className = "";
    delete document.documentElement.dataset.theme;
    document.documentElement.style.colorScheme = "";
    installMatchMedia(false);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("follows the system preference until the user makes a choice", () => {
    installMatchMedia(true);
    const { result } = renderHook(() => useTheme(), { wrapper });

    expect(result.current.preference).toBe("system");
    expect(result.current.theme).toBe("dark");
    expect(document.documentElement).toHaveClass("dark");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
  });

  it("persists an explicit theme and applies semantic color-scheme metadata", () => {
    const { result } = renderHook(() => useTheme(), { wrapper });

    act(() => result.current.setPreference("dark"));

    expect(result.current.preference).toBe("dark");
    expect(result.current.theme).toBe("dark");
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(document.documentElement.style.colorScheme).toBe("dark");
  });

  it("falls back to system for an invalid stored value and toggles the resolved theme", () => {
    localStorage.setItem(THEME_STORAGE_KEY, "sepia");
    const { result } = renderHook(() => useTheme(), { wrapper });

    expect(result.current.preference).toBe("system");
    expect(result.current.theme).toBe("light");

    act(() => result.current.toggleTheme());

    expect(result.current.preference).toBe("dark");
    expect(result.current.theme).toBe("dark");
  });
});
