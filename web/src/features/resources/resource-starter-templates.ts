import type { ResourceDescriptor } from "@/api/types";

function identity(apiVersion: string, kind: string, name: string, namespace: string): string {
  return `apiVersion: ${apiVersion}\nkind: ${kind}\nmetadata:\n  name: ${name}${namespace ? `\n  namespace: ${namespace}` : ""}\n`;
}

function workloadIdentity(kind: string, name: string, namespace: string, replicas?: number): string {
  return `${identity("apps/v1", kind, name, namespace)}spec:\n${kind === "StatefulSet" ? `  serviceName: ${name}\n` : ""}${replicas === undefined ? "" : `  replicas: ${replicas}\n`}  selector:\n    matchLabels:\n      app: ${name}\n  template:\n    metadata:\n      labels:\n        app: ${name}\n    spec:\n      containers:\n        - name: app\n          image: nginx:alpine\n          ports:\n            - name: http\n              containerPort: 80\n`;
}

function jobTemplate(name: string, namespace: string): string {
  return `${identity("batch/v1", "Job", name, namespace)}spec:\n  backoffLimit: 3\n  template:\n    metadata:\n      labels:\n        app: ${name}\n    spec:\n      restartPolicy: Never\n      containers:\n        - name: task\n          image: busybox:1.36\n          command: ["sh", "-c", "echo dosh-job && sleep 10"]\n`;
}

function cronJobTemplate(name: string, namespace: string): string {
  return `${identity("batch/v1", "CronJob", name, namespace)}spec:\n  schedule: "*/5 * * * *"\n  concurrencyPolicy: Forbid\n  successfulJobsHistoryLimit: 3\n  failedJobsHistoryLimit: 3\n  jobTemplate:\n    spec:\n      backoffLimit: 1\n      template:\n        metadata:\n          labels:\n            app: ${name}\n        spec:\n          restartPolicy: Never\n          containers:\n            - name: task\n              image: busybox:1.36\n              command: ["sh", "-c", "date -u"]\n`;
}

function persistentVolumeTemplate(name: string): string {
  return `${identity("v1", "PersistentVolume", name, "")}spec:\n  capacity:\n    storage: 1Gi\n  accessModes:\n    - ReadWriteOnce\n  persistentVolumeReclaimPolicy: Retain\n  storageClassName: standard\n  hostPath:\n    path: /tmp/dosh-${name}\n    type: DirectoryOrCreate\n`;
}

function storageClassTemplate(name: string): string {
  return `${identity("storage.k8s.io/v1", "StorageClass", name, "")}provisioner: kubernetes.io/no-provisioner\nvolumeBindingMode: WaitForFirstConsumer\nreclaimPolicy: Retain\nallowVolumeExpansion: true\n`;
}

// starterTemplate returns an editable manifest that makes the Schema form
// immediately useful for resource types whose empty spec is not valid or not
// representative. Unsupported types continue to use the minimal identity YAML.
export function starterTemplate(descriptor: ResourceDescriptor, namespace: string): string {
  const group = descriptor.group || "";
  if (group === "apps" && descriptor.resource === "statefulsets") {
    return workloadIdentity("StatefulSet", "statefulset-example", namespace, 1);
  }
  if (group === "apps" && descriptor.resource === "daemonsets") {
    return workloadIdentity("DaemonSet", "daemonset-example", namespace);
  }
  if (group === "batch" && descriptor.resource === "jobs") {
    return jobTemplate("job-example", namespace);
  }
  if (group === "batch" && descriptor.resource === "cronjobs") {
    return cronJobTemplate("cronjob-example", namespace);
  }
  if (!group && descriptor.resource === "persistentvolumes") {
    return persistentVolumeTemplate("pv-example");
  }
  if (group === "storage.k8s.io" && descriptor.resource === "storageclasses") {
    return storageClassTemplate("storageclass-example");
  }
  return `${identity(descriptor.group ? `${descriptor.group}/${descriptor.version}` : descriptor.version, descriptor.kind, "example", descriptor.namespaced ? namespace || "default" : "")}spec: {}\n`;
}

export function hasStarterTemplate(descriptor: ResourceDescriptor): boolean {
  const group = descriptor.group || "";
  return (group === "apps" && ["statefulsets", "daemonsets"].includes(descriptor.resource))
    || (group === "batch" && ["jobs", "cronjobs"].includes(descriptor.resource))
    || (!group && descriptor.resource === "persistentvolumes")
    || (group === "storage.k8s.io" && descriptor.resource === "storageclasses");
}
