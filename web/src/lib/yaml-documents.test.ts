import { describe, expect, it } from "vitest";
import { formatYAMLDocuments, inspectYAMLDocuments, setNamespaceOnYAMLDocuments } from "@/lib/yaml-documents";

describe("yaml-documents", () => {
  it("inspects multiple Kubernetes objects and preserves document order", () => {
    const source = `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: default
data:
  mode: debug
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
`;
    const result = inspectYAMLDocuments(source);
    expect(result.errors).toEqual([]);
    expect(result.documents.map((document) => `${document.kind}/${document.namespace}/${document.name}`)).toEqual([
      "ConfigMap/default/app-config",
      "Deployment/default/web",
    ]);
    expect(result.documents[0].source).toContain("kind: ConfigMap");
    expect(result.documents[1].source).not.toContain("kind: ConfigMap");
  });

  it("reports syntax, namespace, and duplicate-target errors", () => {
    const invalid = inspectYAMLDocuments("kind: ConfigMap\nmetadata:\n  name: app\n");
    expect(invalid.errors).toContain("文档 1：apiVersion 不能为空");
    expect(invalid.errors).toContain("文档 1：metadata.namespace 不能为空");

    const duplicate = inspectYAMLDocuments(`apiVersion: v1
kind: ConfigMap
metadata: { name: app, namespace: default }
---
apiVersion: v1
kind: ConfigMap
metadata: { name: app, namespace: default }
`);
    expect(duplicate.errors.some((error) => error.includes("指向同一资源"))).toBe(true);
  });

  it("rejects unsupported Kubernetes kinds and oversized sources", () => {
    const namespace = inspectYAMLDocuments(`apiVersion: v1
kind: Namespace
metadata:
  name: team-a
`);
    expect(namespace.errors).toContain("文档 1：暂不支持通过 YAML 管理 v1/Namespace");

    const oldDeployment = inspectYAMLDocuments(`apiVersion: extensions/v1beta1
kind: Deployment
metadata: { name: web, namespace: default }
`);
    expect(oldDeployment.errors).toContain("文档 1：暂不支持通过 YAML 管理 extensions/v1beta1/Deployment");

    const oversized = inspectYAMLDocuments("a".repeat(1024 * 1024 + 1));
    expect(oversized.documents).toEqual([]);
    expect(oversized.errors[0]).toContain("YAML 清单不能超过 1 MiB");
  });

  it("formats documents without losing comments", () => {
    const source = `# application settings
apiVersion: v1
kind: ConfigMap
metadata: { name: app, namespace: default }
data: { mode: debug }
`;
    const result = formatYAMLDocuments(source);
    expect(result.errors).toEqual([]);
    expect(result.yaml).toContain("# application settings");
    expect(result.yaml).toContain("data: { mode: debug }");
    expect(result.yaml.endsWith("\n")).toBe(true);
  });

  it("rewrites namespaced documents and skips cluster-scoped resources", () => {
    const source = `apiVersion: v1
kind: ConfigMap
metadata:
  name: app
  namespace: default
---
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: fast
`;
    const result = setNamespaceOnYAMLDocuments(source, "production");
    expect(result.errors).toEqual([]);
    expect(result.updated).toBe(1);
    expect(result.skipped).toBe(1);
    const inspected = inspectYAMLDocuments(result.yaml);
    expect(inspected.documents[0].namespace).toBe("production");
    expect(inspected.documents[1].namespace).toBe("");
  });

  it("does not merge distinct documents into duplicate targets when rewriting namespaces", () => {
    const source = `apiVersion: v1
kind: ConfigMap
metadata: { name: app, namespace: one }
---
apiVersion: v1
kind: ConfigMap
metadata: { name: app, namespace: two }
`;
    const result = setNamespaceOnYAMLDocuments(source, "production");
    expect(result.updated).toBe(0);
    expect(result.yaml).toBe(source);
    expect(result.errors.some((error) => error.includes("指向同一资源"))).toBe(true);
  });
});
