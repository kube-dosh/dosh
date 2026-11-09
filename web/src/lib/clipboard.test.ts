import { afterEach, describe, expect, it, vi } from "vitest";
import { copyText } from "@/lib/clipboard";

describe("copyText", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: undefined });
    Object.defineProperty(globalThis, "isSecureContext", { configurable: true, value: false });
  });

  it("uses the asynchronous clipboard API in a secure context", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    Object.defineProperty(globalThis, "isSecureContext", { configurable: true, value: true });

    await copyText("secret-value");

    expect(writeText).toHaveBeenCalledWith("secret-value");
  });

  it("falls back to a temporary textarea when the clipboard API is unavailable", async () => {
    const execCommand = vi.fn().mockReturnValue(true);
    Object.defineProperty(document, "execCommand", { configurable: true, value: execCommand });

    await copyText("http-value");

    expect(execCommand).toHaveBeenCalledWith("copy");
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("reports a clear failure when neither clipboard path is available", async () => {
    Object.defineProperty(document, "execCommand", { configurable: true, value: undefined });

    await expect(copyText("value")).rejects.toThrow("clipboard is unavailable");
  });
});
