import { describe, expect, it } from "vitest";
import { diffStats, diffYAML } from "@/features/resources/yaml-diff";

describe("diffYAML", () => {
  it("emits context, removed and added lines with old/new line numbers", () => {
    const diff = diffYAML("kind: Widget\nspec:\n  size: small\n", "kind: Widget\nspec:\n  size: large\n  replicas: 2\n");
    expect(diff.filter((line) => line.kind === "removed").map((line) => line.text)).toEqual(["  size: small"]);
    expect(diff.filter((line) => line.kind === "added").map((line) => line.text)).toEqual(["  size: large", "  replicas: 2"]);
    expect(diff[0]).toMatchObject({ kind: "context", text: "kind: Widget", oldLine: 1, newLine: 1 });
    expect(diffStats(diff)).toEqual({ added: 2, removed: 1 });
  });

  it("handles empty sides and large bounded changes", () => {
    expect(diffStats(diffYAML("", "a: 1\n"))).toEqual({ added: 1, removed: 0 });
    const before = Array.from({ length: 700 }, (_, index) => `old-${index}: true`).join("\n");
    const after = Array.from({ length: 700 }, (_, index) => `new-${index}: true`).join("\n");
    const diff = diffYAML(before, after);
    expect(diff.some((line) => line.kind === "meta")).toBe(true);
    expect(diffStats(diff)).toMatchObject({ added: 700, removed: 700 });
  });
});
