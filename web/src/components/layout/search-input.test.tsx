import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SearchInput } from "@/components/layout/search-input";

describe("SearchInput", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("exposes an accessible name and can clear the current query", () => {
    const onChange = vi.fn();
    render(<SearchInput value="web" onChange={onChange} placeholder="搜索工作负载" />);

    expect(screen.getByRole("textbox", { name: "搜索工作负载" })).toHaveValue("web");
    fireEvent.click(screen.getByRole("button", { name: "清空搜索" }));

    expect(onChange).toHaveBeenCalledWith("");
  });

  it("debounces rapid typing so each keystroke does not trigger a request", () => {
    vi.useFakeTimers();
    const onChange = vi.fn();
    render(<SearchInput value="" onChange={onChange} />);

    fireEvent.change(screen.getByRole("textbox"), { target: { value: "nginx" } });
    expect(onChange).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(250));
    expect(onChange).toHaveBeenCalledWith("nginx");
  });
});
