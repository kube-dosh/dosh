import { describe, expect, it } from "vitest";
import {
  addImagePullSecret,
  asRecord,
  allTemplateContainers,
  cloneTemplateDocument,
  containerImage,
  containerKey,
  containerName,
  findTemplateContainer,
  parseTemplateYAML,
  serializeTemplateYAML,
  templateContainers,
  type PodTemplateDocument,
} from "@/features/workloads/pod-template-document";

const document: PodTemplateDocument = {
  metadata: { labels: { app: "api" }, annotations: { owner: "platform" } },
  spec: {
    initContainers: [{ name: "migrate", image: "registry.test/migrate:1" }, { name: "proxy", image: "registry.test/proxy:1", restartPolicy: "Always" }],
    containers: [{ name: "api", image: "registry.test/api:1" }, { name: "worker", image: "registry.test/worker:1" }],
    unknownField: { retained: true },
  },
};

describe("pod template document helpers", () => {
  it("clones templates without sharing nested state", () => {
    const clone = cloneTemplateDocument(document);
    templateContainers(clone)[0].image = "registry.test/api:2";
    expect(containerImage(templateContainers(document)[0])).toBe("registry.test/api:1");
    expect((asRecord(asRecord(clone.spec).unknownField).retained)).toBe(true);
    expect((asRecord(asRecord(document.spec).unknownField).retained)).toBe(true);
  });

  it("enumerates Init Containers before application containers with stable keys", () => {
    expect(allTemplateContainers(document).map(({ container, init }) => `${containerKey(init, containerName(container))}:${containerImage(container)}`)).toEqual([
      "initContainer:migrate:registry.test/migrate:1",
      "initContainer:proxy:registry.test/proxy:1",
      "container:api:registry.test/api:1",
      "container:worker:registry.test/worker:1",
    ]);
    expect(containerName(findTemplateContainer(document, false, "worker")!)).toBe("worker");
    expect(findTemplateContainer(document, true, "api")).toBeUndefined();
  });

  it("adds image pull secrets once and preserves unknown template fields", () => {
    const clone = cloneTemplateDocument(document);
    addImagePullSecret(clone, "registry-credentials");
    addImagePullSecret(clone, "registry-credentials");
    expect(asRecord(clone.spec).imagePullSecrets).toEqual([{ name: "registry-credentials" }]);
    expect((asRecord(asRecord(clone.spec).unknownField).retained)).toBe(true);
  });

  it("round-trips complete YAML without dropping unknown fields", () => {
    const encoded = serializeTemplateYAML(document);
    const decoded = parseTemplateYAML(encoded);
    expect(decoded).toEqual(document);
    expect((asRecord(asRecord(decoded.spec).unknownField).retained)).toBe(true);
  });

  it("rejects non-object YAML roots", () => {
    expect(() => parseTemplateYAML("- one\n- two\n")).toThrow("YAML 根节点必须是对象");
  });
});
