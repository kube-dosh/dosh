import { describe, expect, it } from "vitest";
import { createLogStreamParser, parseLogStreamFrame } from "@/features/workloads/log-stream";

describe("log stream protocol", () => {
  it("parses chunked NDJSON frames without corrupting embedded newlines", () => {
    const parser = createLogStreamParser();
    const frames = [
      ...parser.push('{"type":"log","data":"line one\\n'),
      ...parser.push('line two\\n"}\n{"type":"end"}\n'),
      ...parser.flush(),
    ];
    expect(frames).toEqual([
      { type: "log", data: "line one\nline two\n" },
      { type: "end" },
    ]);
  });

  it("preserves structured terminal errors and request IDs", () => {
    expect(parseLogStreamFrame('{"type":"error","code":"dependency_unavailable","message":"cluster API is unavailable","requestId":"req-42"}')).toEqual({
      type: "error",
      code: "dependency_unavailable",
      message: "cluster API is unavailable",
      requestId: "req-42",
    });
  });

  it("turns malformed frames into a terminal client error", () => {
    expect(parseLogStreamFrame('{"type":"log"')).toEqual({
      type: "error",
      code: "invalid_stream_frame",
      message: "收到无法解析的日志流数据",
    });
  });
});
