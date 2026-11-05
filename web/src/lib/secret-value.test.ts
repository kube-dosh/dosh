import { describe, expect, it } from "vitest";
import { decodeSecretValue } from "@/lib/secret-value";

function encode(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString("base64");
}

describe("decodeSecretValue", () => {
  it("decodes UTF-8 text and preserves line breaks", () => {
    const decoded = decodeSecretValue(encode(new TextEncoder().encode("token=abc\n用户名=运维\n")));
    expect(decoded).toEqual({ kind: "text", display: "token=abc\n用户名=运维\n", copyValue: "token=abc\n用户名=运维\n" });
  });

  it("keeps non-text bytes as base64", () => {
    const encoded = encode(new Uint8Array([0, 255, 10]));
    expect(decodeSecretValue(encoded)).toEqual({ kind: "binary", display: encoded, copyValue: encoded });
  });

  it("returns the original value when base64 decoding fails", () => {
    expect(decodeSecretValue("not-base64!")).toEqual({ kind: "invalid", display: "not-base64!", copyValue: "not-base64!" });
  });

  it("accepts missing padding and URL-safe alphabet", () => {
    expect(decodeSecretValue("5L2g5aW9").display).toBe("你好");
  });

  it("handles an empty value", () => {
    expect(decodeSecretValue("")).toEqual({ kind: "text", display: "", copyValue: "" });
  });
});
