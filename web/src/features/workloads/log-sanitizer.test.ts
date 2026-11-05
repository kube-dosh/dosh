import { describe, expect, it } from "vitest";
import { createLogSanitizer } from "@/features/workloads/log-sanitizer";

function sanitizeChunks(chunks: string[]): string {
  const sanitizer = createLogSanitizer();
  return chunks.map((chunk) => sanitizer.push(chunk)).join("") + sanitizer.flush();
}

describe("log sanitizer", () => {
  it("removes ANSI CSI sequences while preserving line structure", () => {
    expect(sanitizeChunks(["ok\t\u001b[31mred\u001b[0m\nnext\r\n"])).toBe("ok\tred\nnext\n");
  });

  it("removes a CSI sequence split across network chunks", () => {
    expect(sanitizeChunks(["one \u001b[3", "1mred\u001b[0", "m two"])).toBe("one red two");
  });

  it("removes OSC titles and hyperlinks split across chunks", () => {
    expect(sanitizeChunks(["\u001b]0;window title\u0007visible\n\u001b]8;;https://example.test/\u001b", "\\click\u001b]8;;\u0007done"]))
      .toBe("visible\nclickdone");
  });

  it("removes DCS payloads without leaking their contents", () => {
    expect(sanitizeChunks(["before\u001bP1;2|secret\u001b", "\\after"])).toBe("beforeafter");
  });

  it("removes C0, C1, DEL and bidirectional display controls", () => {
    expect(sanitizeChunks(["a\u0000b\u0008c\u007fd\u009be\u202ef\u2067g"])).toBe("abcdefg");
  });

  it("drops an incomplete escape sequence at stream end without swallowing later input", () => {
    const sanitizer = createLogSanitizer();
    expect(sanitizer.push("safe\u001b[31")).toBe("safe");
    expect(sanitizer.flush()).toBe("");
    expect(sanitizer.push("plain")).toBe("plain");
  });

  it("keeps HTML markup as literal text", () => {
    const payload = '<img src=x onerror="alert(1)"><script>alert(2)</script>';
    expect(sanitizeChunks([payload])).toBe(payload);
  });
});
