import { useSyncExternalStore } from "react";

export interface RecentResource {
  path: string;
  title: string;
  subtitle: string;
  kind?: string;
  namespace?: string;
  visitedAt: number;
}

export interface SavedResourceView {
  id: string;
  name: string;
  q?: string;
  namespace?: string;
  hiddenColumns: string[];
  createdAt: number;
}

const FAVORITES_KEY = "dosh.resource-favorites.v1";
const RECENT_KEY = "dosh.recent-resources.v1";
const VIEWS_KEY = "dosh.resource-saved-views.v1";
const COLUMNS_KEY = "dosh.resource-hidden-columns.v1";
const EVENT = "dosh:resource-preferences";
let revision = 0;

function notify() {
  revision += 1;
  if (typeof window !== "undefined") window.dispatchEvent(new Event(EVENT));
}

function subscribe(listener: () => void) {
  if (typeof window === "undefined") return () => undefined;
  window.addEventListener(EVENT, listener);
  return () => window.removeEventListener(EVENT, listener);
}

export function useResourcePreferencesRevision(): number {
  return useSyncExternalStore(subscribe, () => revision, () => 0);
}

function readJSON<T>(key: string, fallback: T): T {
  if (typeof window === "undefined") return fallback;
  try {
    const raw = window.localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}

function writeJSON(key: string, value: unknown) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
    notify();
  } catch {
    // Browser storage can be disabled; the current page remains usable.
  }
}

export function resourceTypeKey(group: string, resource: string): string {
  return `${group || "_core"}/${resource}`;
}

export function favoriteResourceTypes(): string[] {
  const values = readJSON<unknown>(FAVORITES_KEY, []);
  return Array.isArray(values) ? values.filter((item): item is string => typeof item === "string") : [];
}

export function toggleFavoriteResourceType(key: string): boolean {
  const current = new Set(favoriteResourceTypes());
  const next = !current.has(key);
  if (next) current.add(key);
  else current.delete(key);
  writeJSON(FAVORITES_KEY, [...current].sort());
  return next;
}

export function recentResources(limit = 8): RecentResource[] {
  const values = readJSON<unknown>(RECENT_KEY, []);
  if (!Array.isArray(values)) return [];
  return values.filter((item): item is RecentResource => Boolean(item && typeof item === "object" && typeof (item as RecentResource).path === "string" && typeof (item as RecentResource).title === "string"))
    .slice(0, limit);
}

export function rememberResource(entry: Omit<RecentResource, "visitedAt">) {
  const current = recentResources(50).filter((item) => item.path !== entry.path);
  writeJSON(RECENT_KEY, [{ ...entry, visitedAt: Date.now() }, ...current].slice(0, 50));
}

export function clearRecentResources() {
  writeJSON(RECENT_KEY, []);
}

function viewsByResource(): Record<string, SavedResourceView[]> {
  const values = readJSON<unknown>(VIEWS_KEY, {});
  return values && typeof values === "object" && !Array.isArray(values) ? values as Record<string, SavedResourceView[]> : {};
}

export function savedResourceViews(key: string): SavedResourceView[] {
  const values = viewsByResource()[key];
  return Array.isArray(values) ? values.filter((item) => item && typeof item.id === "string" && typeof item.name === "string") : [];
}

export function saveResourceView(key: string, view: Omit<SavedResourceView, "id" | "createdAt">): SavedResourceView {
  const values = viewsByResource();
  const saved: SavedResourceView = { ...view, id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`, createdAt: Date.now() };
  values[key] = [saved, ...(values[key] ?? [])].slice(0, 12);
  writeJSON(VIEWS_KEY, values);
  return saved;
}

export function deleteResourceView(key: string, id: string) {
  const values = viewsByResource();
  values[key] = (values[key] ?? []).filter((item) => item.id !== id);
  if (!values[key].length) delete values[key];
  writeJSON(VIEWS_KEY, values);
}

function hiddenColumnsByResource(): Record<string, string[]> {
  const values = readJSON<unknown>(COLUMNS_KEY, {});
  return values && typeof values === "object" && !Array.isArray(values) ? values as Record<string, string[]> : {};
}

export function hiddenResourceColumns(key: string): string[] {
  const values = hiddenColumnsByResource()[key];
  return Array.isArray(values) ? values.filter((item): item is string => typeof item === "string") : [];
}

export function setHiddenResourceColumns(key: string, columns: string[]) {
  const values = hiddenColumnsByResource();
  if (columns.length) values[key] = [...new Set(columns)].sort();
  else delete values[key];
  writeJSON(COLUMNS_KEY, values);
}
