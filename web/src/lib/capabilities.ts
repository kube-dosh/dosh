import type { Capabilities, ResourceCapabilities } from "@/api/types";

const workloadResources: Record<string, string> = {
  deployment: "deployments",
  statefulset: "statefulsets",
  daemonset: "daemonsets",
  job: "jobs",
  cronjob: "cronjobs",
};

export function resourceCapabilities(capabilities: Capabilities | undefined, resource: string): ResourceCapabilities {
  return capabilities?.resourceActions?.[resource] ?? { create: false, update: false, delete: false, scale: false };
}

export function workloadCapabilities(capabilities: Capabilities | undefined, kind: string): ResourceCapabilities {
  return resourceCapabilities(capabilities, workloadResources[kind.toLowerCase()] ?? "deployments");
}

export function yamlResourceCapabilities(capabilities: Capabilities | undefined, type: string): ResourceCapabilities {
  switch (type.toLowerCase()) {
    case "deployment":
    case "statefulset":
    case "daemonset":
    case "job":
    case "cronjob":
      return workloadCapabilities(capabilities, type);
    case "service": return resourceCapabilities(capabilities, "services");
    case "ingress": return resourceCapabilities(capabilities, "ingresses");
    case "configmap": return resourceCapabilities(capabilities, "configmaps");
    case "secret": return resourceCapabilities(capabilities, "secrets");
    case "pod": return resourceCapabilities(capabilities, "pods");
    case "persistentvolumeclaim": return resourceCapabilities(capabilities, "persistentvolumeclaims");
    case "persistentvolume": return resourceCapabilities(capabilities, "persistentvolumes");
    case "storageclass": return resourceCapabilities(capabilities, "storageclasses");
    case "node": return resourceCapabilities(capabilities, "nodes");
    case "namespace": return resourceCapabilities(capabilities, "namespaces");
  default: return { create: false, update: false, delete: false, scale: false };
  }
}
