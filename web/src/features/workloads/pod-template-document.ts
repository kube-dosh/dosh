import { parse, stringify } from "yaml";

export type PodTemplateDocument = Record<string, unknown>;
export type PodContainerDocument = Record<string, unknown>;

export function cloneTemplateDocument(document: PodTemplateDocument): PodTemplateDocument {
  return JSON.parse(JSON.stringify(document)) as PodTemplateDocument;
}

export function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

export function templateMetadata(document: PodTemplateDocument): Record<string, unknown> {
  const metadata = asRecord(document.metadata);
  document.metadata = metadata;
  return metadata;
}

export function templateSpec(document: PodTemplateDocument): Record<string, unknown> {
  const spec = asRecord(document.spec);
  document.spec = spec;
  return spec;
}

export function templateContainers(document: PodTemplateDocument, init = false): PodContainerDocument[] {
  const spec = templateSpec(document);
  const key = init ? "initContainers" : "containers";
  const current = spec[key];
  if (!Array.isArray(current)) {
    spec[key] = [];
  }
  return spec[key] as PodContainerDocument[];
}

export function allTemplateContainers(document: PodTemplateDocument): Array<{ container: PodContainerDocument; init: boolean }> {
  return [
    ...templateContainers(document, true).map((container) => ({ container, init: true })),
    ...templateContainers(document, false).map((container) => ({ container, init: false })),
  ];
}

export function containerName(container: PodContainerDocument): string {
  return typeof container.name === "string" ? container.name : "";
}

export function containerImage(container: PodContainerDocument): string {
  return typeof container.image === "string" ? container.image : "";
}

export function containerKey(init: boolean, name: string): string {
  return `${init ? "initContainer" : "container"}:${name}`;
}

export function findTemplateContainer(document: PodTemplateDocument, init: boolean, name: string): PodContainerDocument | undefined {
  return templateContainers(document, init).find((container) => containerName(container) === name);
}

export function parseTemplateYAML(value: string): PodTemplateDocument {
  const parsed = parse(value) as unknown;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("YAML 根节点必须是对象");
  }
  return parsed as PodTemplateDocument;
}

export function serializeTemplateYAML(document: PodTemplateDocument): string {
  return stringify(document, { lineWidth: 0 });
}

export function stringMap(value: unknown): Record<string, string> {
  const source = asRecord(value);
  const result: Record<string, string> = {};
  for (const [key, current] of Object.entries(source)) {
    if (typeof current === "string") result[key] = current;
  }
  return result;
}

export function stringValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

export function numberValue(value: unknown): number | undefined {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim() !== "") {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return undefined;
}

export function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

export function setStringMap(target: Record<string, unknown>, key: string, values: Record<string, string>) {
  const clean = Object.fromEntries(Object.entries(values).filter(([entryKey, entryValue]) => entryKey.trim() && entryValue !== undefined));
  if (Object.keys(clean).length) target[key] = clean;
  else delete target[key];
}

export function parseLineValues(value: string): string[] {
  return value.split("\n").map((line) => line.trim()).filter(Boolean);
}

export function formatLineValues(value: unknown): string {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string").join("\n") : "";
}

export function containerRegistrySecret(registries: Array<{ id: string; pullSecretName?: string }>, registryId: string | undefined): string | undefined {
  if (!registryId) return undefined;
  return registries.find((registry) => registry.id === registryId)?.pullSecretName || undefined;
}

export function addImagePullSecret(document: PodTemplateDocument, secretName: string) {
  if (!secretName.trim()) return;
  const spec = templateSpec(document);
  const secrets = arrayValue(spec.imagePullSecrets).map((item) => asRecord(item)).filter((item) => typeof item.name === "string");
  if (secrets.some((item) => item.name === secretName)) return;
  spec.imagePullSecrets = [...secrets, { name: secretName }];
}

