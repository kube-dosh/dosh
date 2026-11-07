import { isMap, parseAllDocuments } from "yaml";

const maxYAMLBytes = 1024 * 1024;

const supportedYAMLKinds = new Map<string, "namespaced" | "cluster">([
  ["v1/Pod", "namespaced"],
  ["apps/v1/Deployment", "namespaced"],
  ["apps/v1/StatefulSet", "namespaced"],
  ["apps/v1/DaemonSet", "namespaced"],
  ["batch/v1/Job", "namespaced"],
  ["batch/v1/CronJob", "namespaced"],
  ["v1/Service", "namespaced"],
  ["networking.k8s.io/v1/Ingress", "namespaced"],
  ["v1/ConfigMap", "namespaced"],
  ["v1/Secret", "namespaced"],
  ["v1/PersistentVolumeClaim", "namespaced"],
  ["storage.k8s.io/v1/StorageClass", "cluster"],
  ["v1/PersistentVolume", "cluster"],
]);

const clusterScopedKinds = new Set([
  "APIService",
  "CSIDriver",
  "CSINode",
  "ClusterRole",
  "ClusterRoleBinding",
  "CustomResourceDefinition",
  "IngressClass",
  "MutatingWebhookConfiguration",
  "Namespace",
  "Node",
  "PersistentVolume",
  "PodSecurityPolicy",
  "PriorityClass",
  "RuntimeClass",
  "StorageClass",
  "ValidatingAdmissionPolicy",
  "ValidatingAdmissionPolicyBinding",
  "ValidatingWebhookConfiguration",
  "VolumeAttachment",
]);

export type YAMLTarget = {
  index: number;
  apiVersion: string;
  kind: string;
  name: string;
  namespace: string;
  clusterScoped: boolean;
};

export type ParsedYAMLDocument = YAMLTarget & { source: string };

export type YAMLInspection = {
  documents: ParsedYAMLDocument[];
  errors: string[];
};

function renderDocument(document: ReturnType<typeof parseAllDocuments>[number]): string {
  const text = document.toString({ lineWidth: 0 }).replace(/^---\s*\n/, "").trimEnd();
  return `${text}\n`;
}

function renderDocuments(documents: ReturnType<typeof parseAllDocuments>): string {
  return documents
    .filter((document) => document.contents !== null)
    .map(renderDocument)
    .join("---\n");
}

function targetFromDocument(document: ReturnType<typeof parseAllDocuments>[number]): Omit<YAMLTarget, "index"> | undefined {
  const value = document.toJS() as unknown;
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const record = value as Record<string, unknown>;
  const metadata = record.metadata && typeof record.metadata === "object" && !Array.isArray(record.metadata)
    ? record.metadata as Record<string, unknown>
    : {};
  const apiVersion = typeof record.apiVersion === "string" ? record.apiVersion : "";
  const kind = typeof record.kind === "string" ? record.kind : "";
  const name = typeof metadata.name === "string" ? metadata.name : "";
  const namespace = typeof metadata.namespace === "string" ? metadata.namespace : "";
  return { apiVersion, kind, name, namespace, clusterScoped: clusterScopedKinds.has(kind) };
}

export function inspectYAMLDocuments(source: string): YAMLInspection {
  const sourceBytes = new TextEncoder().encode(source).length;
  if (sourceBytes > maxYAMLBytes) {
    return { documents: [], errors: [`YAML 清单不能超过 1 MiB（当前 ${(sourceBytes / 1024 / 1024).toFixed(2)} MiB）`] };
  }
  const parsed = parseAllDocuments(source, { prettyErrors: true, uniqueKeys: true });
  const documents: ParsedYAMLDocument[] = [];
  const errors: string[] = [];
  const seenTargets = new Map<string, number>();

  parsed.forEach((document, parsedIndex) => {
    if (document.errors.length > 0) {
      document.errors.forEach((error) => errors.push(`文档 ${parsedIndex + 1}：${error.message}`));
      return;
    }
    if (document.contents === null) return;
    const target = targetFromDocument(document);
    if (!target) {
      errors.push(`文档 ${parsedIndex + 1}：根节点必须是 YAML 对象`);
      return;
    }
    if (!target.apiVersion) errors.push(`文档 ${parsedIndex + 1}：apiVersion 不能为空`);
    if (!target.kind) errors.push(`文档 ${parsedIndex + 1}：kind 不能为空`);
    if (!target.name) errors.push(`文档 ${parsedIndex + 1}：metadata.name 不能为空`);
    if (!target.apiVersion || !target.kind || !target.name) {
      if (!target.clusterScoped && !target.namespace) errors.push(`文档 ${parsedIndex + 1}：metadata.namespace 不能为空`);
      return;
    }
    const scope = supportedYAMLKinds.get(`${target.apiVersion}/${target.kind}`);
    if (!scope) {
      errors.push(`文档 ${parsedIndex + 1}：暂不支持通过 YAML 管理 ${target.apiVersion}/${target.kind}`);
      return;
    }
    target.clusterScoped = scope === "cluster";
    if (scope === "namespaced" && !target.namespace) errors.push(`文档 ${parsedIndex + 1}：metadata.namespace 不能为空`);
    if (scope === "namespaced" && !target.namespace) return;

    const index = documents.length + 1;
    const key = `${target.apiVersion}/${target.kind}/${target.namespace}/${target.name}`;
    const duplicateIndex = seenTargets.get(key);
    if (duplicateIndex !== undefined) {
      errors.push(`文档 ${parsedIndex + 1} 与文档 ${duplicateIndex} 指向同一资源 ${target.kind} ${target.namespace ? `${target.namespace}/` : ""}${target.name}`);
      return;
    }
    seenTargets.set(key, parsedIndex + 1);
    documents.push({ ...target, index, source: renderDocument(document) });
  });

  if (parsed.length === 0 || documents.length === 0) errors.push("请至少输入一个非空 YAML 文档");
  return { documents, errors };
}

export function formatYAMLDocuments(source: string): { yaml: string; errors: string[] } {
  const inspection = inspectYAMLDocuments(source);
  if (inspection.errors.length > 0) return { yaml: source, errors: inspection.errors };
  const parsed = parseAllDocuments(source, { prettyErrors: true, uniqueKeys: true });
  return { yaml: renderDocuments(parsed), errors: [] };
}

export function setNamespaceOnYAMLDocuments(source: string, namespace: string): { yaml: string; updated: number; skipped: number; errors: string[] } {
  const inspection = inspectYAMLDocuments(source);
  if (inspection.errors.length > 0) return { yaml: source, updated: 0, skipped: 0, errors: inspection.errors };
  if (!namespace.trim()) return { yaml: source, updated: 0, skipped: 0, errors: ["命名空间不能为空"] };

  const parsed = parseAllDocuments(source, { prettyErrors: true, uniqueKeys: true });
  let updated = 0;
  let skipped = 0;
  const errors: string[] = [];
  parsed.forEach((document, index) => {
    if (document.contents === null) return;
    const target = targetFromDocument(document);
    if (!target) return;
    if (target.clusterScoped) {
      skipped += 1;
      return;
    }
    if (!isMap(document.contents)) {
      errors.push(`文档 ${index + 1}：根节点必须是 YAML 对象`);
      return;
    }
    try {
      document.setIn(["metadata", "namespace"], namespace);
      updated += 1;
    } catch (error) {
      errors.push(`文档 ${index + 1}：${error instanceof Error ? error.message : "无法写入命名空间"}`);
    }
  });
  if (errors.length > 0) return { yaml: source, updated: 0, skipped, errors };
  const rewritten = renderDocuments(parsed);
  const revalidation = inspectYAMLDocuments(rewritten);
  if (revalidation.errors.length > 0) return { yaml: source, updated: 0, skipped, errors: revalidation.errors };
  return { yaml: rewritten, updated, skipped, errors: [] };
}

export function isClusterScopedKind(kind: string): boolean {
  return clusterScopedKinds.has(kind);
}
