import { beforeEach, describe, expect, it } from "vitest";
import {
  clearGenericDraft,
  deleteGenericTemplate,
  genericDraft,
  genericTemplateKey,
  listGenericTemplates,
  saveGenericDraft,
  saveGenericTemplate,
} from "@/lib/generic-templates";

describe("generic resource templates", () => {
  const key = genericTemplateKey("example.io", "v1", "widgets");
  beforeEach(() => localStorage.clear());

  it("scopes templates by group, version and resource", () => {
    expect(key).toBe("example.io/v1/widgets");
    expect(genericTemplateKey("", "v1", "configmaps")).toBe("_core/v1/configmaps");
  });

  it("saves, lists and deletes local templates", () => {
    const first = saveGenericTemplate(key, "small", "kind: Widget\nspec:\n  size: small\n");
    const second = saveGenericTemplate(key, "large", "kind: Widget\nspec:\n  size: large\n");
    expect(listGenericTemplates(key).map((item) => item.name)).toEqual(["large", "small"]);
    deleteGenericTemplate(key, first.id);
    expect(listGenericTemplates(key).map((item) => item.id)).toEqual([second.id]);
  });

  it("stores and clears one recent draft per resource", () => {
    saveGenericDraft(key, "kind: Widget\n");
    expect(genericDraft(key)?.yaml).toContain("kind: Widget");
    clearGenericDraft(key);
    expect(genericDraft(key)).toBeUndefined();
  });

  it("filters malformed entries", () => {
    localStorage.setItem("dosh.generic-resource-templates.v1", JSON.stringify({ [key]: [{ id: 1 }, { id: "valid", name: "ok", yaml: "kind: Widget\n" }] }));
    localStorage.setItem("dosh.generic-resource-drafts.v1", JSON.stringify({ [key]: { yaml: 42 } }));
    expect(listGenericTemplates(key).map((item) => item.id)).toEqual(["valid"]);
    expect(genericDraft(key)).toBeUndefined();
  });
});
