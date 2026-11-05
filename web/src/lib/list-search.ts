export interface ListSearch {
  q?: string;
  namespace?: string;
  kind?: string;
  status?: string;
  action?: string;
  result?: string;
  tab?: string;
}

export function parseListSearch(raw: Record<string, unknown>): ListSearch {
  const read = (key: string) => {
    const value = raw[key];
    if (typeof value !== "string") return undefined;
    const trimmed = value.trim();
    return trimmed || undefined;
  };
  return { q: read("q"), namespace: read("namespace"), kind: read("kind"), status: read("status"), action: read("action"), result: read("result"), tab: read("tab") };
}
