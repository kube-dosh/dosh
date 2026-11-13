import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { streamLogs } from "@/api/client";
import { LogViewer } from "@/features/workloads/log-viewer";

vi.mock("@/api/client", () => ({ streamLogs: vi.fn() }));

function readerFor(chunks: string[]): ReadableStreamDefaultReader<Uint8Array> {
  const encoded = chunks.map((chunk) => new TextEncoder().encode(chunk));
  let index = 0;
  return {
    read: vi.fn(async () => index < encoded.length
      ? { done: false, value: encoded[index++] }
      : { done: true, value: undefined }),
    cancel: vi.fn(async () => undefined),
    releaseLock: vi.fn(),
    closed: Promise.resolve(),
  } as unknown as ReadableStreamDefaultReader<Uint8Array>;
}

describe("LogViewer security", () => {
  afterEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    delete (window as Window & { __doshXss?: boolean }).__doshXss;
  });

  it("renders HTML as inert text and does not persist log content", async () => {
    const marker = "DOSH_SENSITIVE_MARKER";
    const html = `<img src=x onerror="window.__doshXss=true"> ${marker}`;
    vi.mocked(streamLogs).mockResolvedValue(readerFor([
      `${JSON.stringify({ type: "log", data: `${html}\n` })}\n`,
      JSON.stringify({ type: "log", data: "\u001b[31mred\u001b[0m\n" }) + "\n",
      JSON.stringify({ type: "log", data: "\u001b]8;;javascript:alert(1)\u0007link\u001b]8;;\u0007\n" }) + "\n",
      '{"type":"end"}\n',
    ]));

    const { container } = render(<LogViewer namespace="default" name="web" containers={[{ name: "web", ready: true }]} />);
    const output = container.querySelector("pre");

    await waitFor(() => expect(output?.textContent).toContain("link"));
    expect(output?.textContent).toContain(html);
    expect(output?.textContent).toContain("red");
    expect(output?.querySelector("img")).toBeNull();
    expect(output?.querySelector("script")).toBeNull();
    expect((window as Window & { __doshXss?: boolean }).__doshXss).toBeUndefined();
    expect(localStorage).toHaveLength(0);
    expect(sessionStorage).toHaveLength(0);

    const attributeValues = Array.from(document.querySelectorAll("*"))
      .flatMap((element) => Array.from(element.attributes, (attribute) => attribute.value))
      .join("\n");
    expect(attributeValues).not.toContain(marker);
  });

  it("normalizes ANSI sequences that arrive in separate stream chunks", async () => {
    vi.mocked(streamLogs).mockResolvedValue(readerFor([
      '{"type":"log","data":"safe \\u001b[3"}\n',
      '{"type":"log","data":"1mred\\u001b[0m\\n"}\n',
      '{"type":"end"}\n',
    ]));

    const { container } = render(<LogViewer namespace="default" name="web" containers={[{ name: "web", ready: true }]} />);
    const output = container.querySelector("pre");

    await waitFor(() => expect(output?.textContent).toContain("safe red"));
    expect(output?.textContent).not.toContain("\u001b");
  });

  it("shows a structured terminal error and can reconnect manually", async () => {
    vi.mocked(streamLogs).mockResolvedValueOnce(readerFor([
      '{"type":"log","data":"before failure\\n"}\n',
      '{"type":"error","code":"dependency_unavailable","message":"cluster API is unavailable","requestId":"req-42"}\n',
    ])).mockResolvedValueOnce(readerFor([
      '{"type":"log","data":"after reconnect\\n"}\n',
      '{"type":"end"}\n',
    ]));

    render(<LogViewer namespace="default" name="web" containers={[{ name: "web", ready: true }]} />);
    expect(await screen.findByText("cluster API is unavailable（请求 ID：req-42）")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));
    expect(await screen.findByText(/after reconnect/)).toBeInTheDocument();
    expect(streamLogs).toHaveBeenCalledTimes(2);
  });

  it("reports a stream that closes without an end frame as interrupted", async () => {
    vi.mocked(streamLogs).mockResolvedValue(readerFor([
      '{"type":"log","data":"partial output\\n"}\n',
    ]));

    render(<LogViewer namespace="default" name="web" containers={[{ name: "web", ready: true }]} />);

    expect(await screen.findByText("日志流意外中断（未收到结束帧）")).toBeInTheDocument();
  });

  it("requests previous logs and filters the current log window", async () => {
    vi.mocked(streamLogs).mockResolvedValue(readerFor([
      '{"type":"log","data":"ready line\\npanic: previous instance failed\\n"}\n',
      '{"type":"end"}\n',
    ]));

    const { container } = render(<LogViewer namespace="default" name="web" containers={[{ name: "web", ready: false }]} />);
    await waitFor(() => expect(container.querySelector("pre")?.textContent).toContain("previous instance failed"));

    fireEvent.change(screen.getByRole("textbox", { name: "筛选当前日志窗口" }), { target: { value: "panic" } });
    expect(container.querySelector("pre")?.textContent).toBe("panic: previous instance failed");

    fireEvent.click(screen.getByRole("button", { name: "上一次实例" }));
    await waitFor(() => expect(streamLogs).toHaveBeenLastCalledWith("default", "web", expect.objectContaining({ previous: true, follow: false })));
  });
});
