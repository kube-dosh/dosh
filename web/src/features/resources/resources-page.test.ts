import { describe, expect, it } from "vitest";
import { collapseResourceVersions } from "@/features/resources/resources-page";

describe("collapseResourceVersions", () => {
  it("groups served versions and selects the preferred descriptor", () => {
    const rows = collapseResourceVersions([
      { group: "example.io", version: "v1beta1", resource: "widgets", kind: "Widget", namespaced: true, verbs: ["get", "list"] },
      { group: "example.io", version: "v1", resource: "widgets", kind: "Widget", namespaced: true, preferred: true, verbs: ["get", "list"] },
      { group: "", version: "v1", resource: "pods", kind: "Pod", namespaced: true, preferred: true, verbs: ["get", "list"] },
    ]);
    expect(rows).toHaveLength(2);
    const widgets = rows.find((row) => row.resource === "widgets");
    expect(widgets?.version).toBe("v1");
    expect(widgets?.versions.map((item) => item.version)).toEqual(["v1", "v1beta1"]);
  });
});
