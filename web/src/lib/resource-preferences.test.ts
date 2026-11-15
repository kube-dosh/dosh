import { beforeEach, describe, expect, it } from "vitest";
import {
  clearRecentResources,
  deleteResourceView,
  favoriteResourceTypes,
  hiddenResourceColumns,
  recentResources,
  rememberResource,
  resourceTypeKey,
  saveResourceView,
  savedResourceViews,
  setHiddenResourceColumns,
  toggleFavoriteResourceType,
} from "@/lib/resource-preferences";

describe("resource preferences", () => {
  beforeEach(() => localStorage.clear());

  it("favorites resource types idempotently and restores them", () => {
    const key = resourceTypeKey("example.io", "widgets");
    expect(favoriteResourceTypes()).toEqual([]);
    expect(toggleFavoriteResourceType(key)).toBe(true);
    expect(toggleFavoriteResourceType(resourceTypeKey("", "pods"))).toBe(true);
    expect(favoriteResourceTypes()).toEqual(["_core/pods", key]);
    expect(toggleFavoriteResourceType(key)).toBe(false);
    expect(favoriteResourceTypes()).toEqual(["_core/pods"]);
  });

  it("saves and deletes per-resource views", () => {
    const key = resourceTypeKey("example.io", "widgets");
    const view = saveResourceView(key, { name: "production", namespace: "production", q: "api", hiddenColumns: ["labels"] });
    expect(savedResourceViews(key)).toMatchObject([{ id: view.id, name: "production", namespace: "production", q: "api", hiddenColumns: ["labels"] }]);
    deleteResourceView(key, view.id);
    expect(savedResourceViews(key)).toEqual([]);
  });

  it("deduplicates hidden columns and persists recent objects", () => {
    const key = resourceTypeKey("example.io", "widgets");
    setHiddenResourceColumns(key, ["labels", "labels", "createdAt"]);
    expect(hiddenResourceColumns(key)).toEqual(["createdAt", "labels"]);
    rememberResource({ path: "/resources/example.io/v1/widgets/a", title: "a", subtitle: "Widget" });
    rememberResource({ path: "/resources/example.io/v1/widgets/b", title: "b", subtitle: "Widget" });
    rememberResource({ path: "/resources/example.io/v1/widgets/a", title: "a", subtitle: "Widget" });
    expect(recentResources().map((item) => item.title)).toEqual(["a", "b"]);
    clearRecentResources();
    expect(recentResources()).toEqual([]);
  });

  it("ignores corrupted storage instead of breaking the page", () => {
    localStorage.setItem("dosh.resource-favorites.v1", "{bad json");
    localStorage.setItem("dosh.resource-hidden-columns.v1", JSON.stringify(["unexpected"]));
    expect(favoriteResourceTypes()).toEqual([]);
    expect(hiddenResourceColumns("example.io/widgets")).toEqual([]);
    expect(recentResources()).toEqual([]);
  });
});
