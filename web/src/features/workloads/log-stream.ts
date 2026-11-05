export type LogStreamFrame =
  | { type: "log"; data: string }
  | { type: "end" }
  | { type: "error"; code: string; message: string; requestId?: string };

function frameError(message: string): LogStreamFrame {
  return { type: "error", code: "invalid_stream_frame", message };
}

export function parseLogStreamFrame(line: string): LogStreamFrame {
  let value: unknown;
  try {
    value = JSON.parse(line);
  } catch {
    return frameError("收到无法解析的日志流数据");
  }
  if (!value || typeof value !== "object") return frameError("日志流帧格式无效");
  const frame = value as Record<string, unknown>;
  if (frame.type === "log" && typeof frame.data === "string") {
    return { type: "log", data: frame.data };
  }
  if (frame.type === "end") return { type: "end" };
  if (frame.type === "error" && typeof frame.message === "string") {
    return {
      type: "error",
      code: typeof frame.code === "string" ? frame.code : "stream_error",
      message: frame.message,
      ...(typeof frame.requestId === "string" && frame.requestId ? { requestId: frame.requestId } : {}),
    };
  }
  return frameError("日志流帧格式无效");
}

export interface LogStreamParser {
  push(chunk: string): LogStreamFrame[];
  flush(): LogStreamFrame[];
}

export function createLogStreamParser(): LogStreamParser {
  let buffered = "";
  const consume = (line: string): LogStreamFrame[] => line ? [parseLogStreamFrame(line)] : [];

  return {
    push(chunk: string): LogStreamFrame[] {
      buffered += chunk;
      const lines = buffered.split("\n");
      buffered = lines.pop() ?? "";
      return lines.flatMap(consume);
    },
    flush(): LogStreamFrame[] {
      const frames = consume(buffered.trimEnd());
      buffered = "";
      return frames;
    },
  };
}
