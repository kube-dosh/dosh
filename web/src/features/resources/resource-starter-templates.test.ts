import { describe, expect, it } from "vitest";
import { parse } from "yaml";
import type { ResourceDescriptor } from "@/api/types";
import { hasStarterTemplate, starterTemplate } from "@/features/resources/resource-starter-templates";

const descriptors: ResourceDescriptor[] = [
  { group: "apps", version: "v1", resource: "statefulsets", kind: "StatefulSet", namespaced: true, verbs: [] },
  { group: "apps", version: "v1", resource: "daemonsets", kind: "DaemonSet", namespaced: true, verbs: [] },
  { group: "batch", version: "v1", resource: "jobs", kind: "Job", namespaced: true, verbs: [] },
  { group: "batch", version: "v1", resource: "cronjobs", kind: "CronJob", namespaced: true, verbs: [] },
  { group: "", version: "v1", resource: "persistentvolumes", kind: "PersistentVolume", namespaced: false, verbs: [] },
  { group: "storage.k8s.io", version: "v1", resource: "storageclasses", kind: "StorageClass", namespaced: false, verbs: [] },
];

describe("resource starter templates", () => {
  it.each(descriptors)("creates editable $kind YAML", (descriptor) => {
    const template = starterTemplate(descriptor, "default");
    const parsed = parse(template) as Record<string, unknown>;
    expect(parsed.kind).toBe(descriptor.kind);
    expect(parsed.metadata).toMatchObject({ name: expect.any(String) });
    if (descriptor.kind === "StorageClass") expect(parsed.provisioner).toEqual(expect.any(String));
    else expect(parsed.spec).toEqual(expect.any(Object));
    expect(hasStarterTemplate(descriptor)).toBe(true);
  });

  it("falls back to a minimal identity for other resources", () => {
    const descriptor: ResourceDescriptor = { group: "example.io", version: "v1", resource: "widgets", kind: "Widget", namespaced: true, verbs: [] };
    const parsed = parse(starterTemplate(descriptor, "default")) as Record<string, unknown>;
    expect(parsed).toMatchObject({ apiVersion: "example.io/v1", kind: "Widget", metadata: { name: "example", namespace: "default" }, spec: {} });
    expect(hasStarterTemplate(descriptor)).toBe(false);
  });
});
