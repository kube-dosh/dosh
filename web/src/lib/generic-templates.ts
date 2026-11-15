export interface GenericResourceTemplate {
  id: string;
  name: string;
  yaml: string;
  createdAt: number;
}

const TEMPLATES_KEY = "dosh.generic-resource-templates.v1";
const DRAFTS_KEY = "dosh.generic-resource-drafts.v1";

function read(key: string): Record<string, unknown> {
  if (typeof window === "undefined") return {};
  try {
    const value = JSON.parse(window.localStorage.getItem(key) ?? "{}") as unknown;
    return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
  } catch {
    return {};
  }
}

function write(key: string, value: Record<string, unknown>) {
  if (typeof window === "undefined") return;
  try { window.localStorage.setItem(key, JSON.stringify(value)); } catch { /* storage can be disabled */ }
}

export function genericTemplateKey(group: string, version: string, resource: string): string {
  return `${group || "_core"}/${version}/${resource}`;
}

export function listGenericTemplates(key: string): GenericResourceTemplate[] {
  const value = read(TEMPLATES_KEY)[key];
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is GenericResourceTemplate => Boolean(item && typeof item === "object" && typeof (item as GenericResourceTemplate).id === "string" && typeof (item as GenericResourceTemplate).yaml === "string"));
}

export function saveGenericTemplate(key: string, name: string, yaml: string): GenericResourceTemplate {
  const values = read(TEMPLATES_KEY);
  const item: GenericResourceTemplate = { id: `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`, name, yaml, createdAt: Date.now() };
  values[key] = [item, ...listGenericTemplates(key)].slice(0, 20);
  write(TEMPLATES_KEY, values);
  return item;
}

export function deleteGenericTemplate(key: string, id: string) {
  const values = read(TEMPLATES_KEY);
  values[key] = listGenericTemplates(key).filter((item) => item.id !== id);
  if (!(values[key] as unknown[]).length) delete values[key];
  write(TEMPLATES_KEY, values);
}

export function genericDraft(key: string): { yaml: string; savedAt: number } | undefined {
  const value = read(DRAFTS_KEY)[key];
  if (!value || typeof value !== "object") return undefined;
  const draft = value as { yaml?: unknown; savedAt?: unknown };
  return typeof draft.yaml === "string" ? { yaml: draft.yaml, savedAt: typeof draft.savedAt === "number" ? draft.savedAt : 0 } : undefined;
}

export function saveGenericDraft(key: string, yaml: string) {
  const values = read(DRAFTS_KEY);
  values[key] = { yaml, savedAt: Date.now() };
  write(DRAFTS_KEY, values);
}

export function clearGenericDraft(key: string) {
  const values = read(DRAFTS_KEY);
  delete values[key];
  write(DRAFTS_KEY, values);
}
