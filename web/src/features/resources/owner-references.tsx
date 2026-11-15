import { Link } from "@tanstack/react-router";
import { ArrowUpRight, GitBranch } from "lucide-react";
import type { ResourceDescriptor } from "@/api/types";

interface OwnerReference {
  apiVersion?: string;
  kind?: string;
  name?: string;
  uid?: string;
  controller?: boolean;
  blockOwnerDeletion?: boolean;
}

export function OwnerReferences({ object, descriptors, namespace }: { object: Record<string, unknown>; descriptors: ResourceDescriptor[]; namespace: string }) {
  const metadata = object.metadata && typeof object.metadata === "object" && !Array.isArray(object.metadata) ? object.metadata as Record<string, unknown> : {};
  const owners = Array.isArray(metadata.ownerReferences) ? metadata.ownerReferences.filter((item): item is OwnerReference => Boolean(item && typeof item === "object")) : [];
  return (
    <div className="min-w-0">
      <p className="mb-2 flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-muted-foreground"><GitBranch className="h-3.5 w-3.5" />Owner References</p>
      {owners.length ? <div className="space-y-2">{owners.map((owner, index) => {
        const apiVersion = owner.apiVersion ?? "";
        const slash = apiVersion.indexOf("/");
        const group = slash >= 0 ? apiVersion.slice(0, slash) : "";
        const version = slash >= 0 ? apiVersion.slice(slash + 1) : apiVersion;
        const descriptor = descriptors.find((item) => item.group === group && item.version === version && item.kind === owner.kind);
        const label = `${owner.kind || "Unknown"}/${owner.name || "unknown"}`;
        return (
          <div key={`${owner.uid || label}-${index}`} className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm">
            <div className="min-w-0"><p className="break-all font-mono text-xs font-medium">{label}</p><p className="mt-0.5 break-all text-[11px] text-muted-foreground">{apiVersion || "apiVersion 未知"}{owner.controller ? " · Controller" : ""}{owner.blockOwnerDeletion ? " · BlockOwnerDeletion" : ""}</p></div>
            {descriptor && owner.name ? <Link to="/resources/$group/$version/$resource/$name" params={{ group: descriptor.group || "_core", version: descriptor.version, resource: descriptor.resource, name: owner.name }} search={{ namespace: namespace || undefined }} className="inline-flex shrink-0 items-center gap-1 text-xs font-medium text-primary hover:underline">打开所有者<ArrowUpRight className="h-3 w-3" /></Link> : <span className="text-[11px] text-muted-foreground">Discovery 中未找到可跳转资源</span>}
          </div>
        );
      })}</div> : <p className="text-sm text-muted-foreground">该对象没有 OwnerReference，可能是顶层资源或由集群外部系统管理。</p>}
    </div>
  );
}
