import { useDeferredValue, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Boxes, CircleDot, Layers3, Search, Server } from "lucide-react";
import { api } from "@/api/client";
import { NamespaceSelect } from "@/components/layout/namespace-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { StatusBadge } from "@/components/feedback/status-badge";

export function GlobalSearch({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [term, setTerm] = useState("");
  const [namespace, setNamespace] = useState("");
  const deferred = useDeferredValue(term.trim());
  const navigate = useNavigate();
  const enabled = open && deferred.length >= 2;
  const nodes = useQuery({ queryKey: ["search", "nodes", deferred], queryFn: () => api.nodes({ q: deferred, pageSize: 50 }), enabled, staleTime: 10_000 });
  const workloads = useQuery({ queryKey: ["search", "workloads", deferred], queryFn: () => api.workloads({ q: deferred, pageSize: 50 }), enabled, staleTime: 10_000 });
  const pods = useQuery({ queryKey: ["search", "pods", deferred], queryFn: () => api.pods({ q: deferred, pageSize: 50 }), enabled, staleTime: 10_000 });
  const resources = useQuery({ queryKey: ["search", "resources", deferred, namespace], queryFn: () => api.resourceSearch(deferred, namespace || undefined, 20), enabled, staleTime: 10_000 });
  const loading = enabled && (nodes.isLoading || workloads.isLoading || pods.isLoading || resources.isLoading);
  const failedGroups = [
    nodes.isError ? "节点" : "",
    workloads.isError ? "工作负载" : "",
    pods.isError ? "Pod" : "",
    resources.isError ? "通用资源" : "",
  ].filter(Boolean);
  const empty = enabled && !loading && failedGroups.length === 0 && !nodes.data?.items.length && !workloads.data?.items.length && !pods.data?.items.length && !resources.data?.items.length;

  const closeAndNavigate = (action: () => void) => {
    onOpenChange(false);
    setTerm("");
    action();
  };

  const handleOpenChange = (next: boolean) => {
    onOpenChange(next);
    if (!next) setTerm("");
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-2xl gap-3 p-0">
        <DialogHeader className="border-b px-5 py-4">
          <DialogTitle className="text-base">搜索集群资源</DialogTitle>
          <DialogDescription>搜索节点、工作负载、Pod 和已授权通用资源，至少输入两个字符。</DialogDescription>
        </DialogHeader>
        <div className="px-5">
          <div className="relative">
            <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input autoFocus value={term} onChange={(event) => setTerm(event.target.value)} placeholder="输入资源名称…" aria-label="搜索集群资源" className="h-11 pl-9 text-base" />
          </div>
          <div className="mt-2 flex items-center gap-2">
            <NamespaceSelect value={namespace} onChange={setNamespace} className="h-8 w-48" />
            <span className="text-[11px] text-muted-foreground">通用资源搜索限定在所选命名空间；未选择时仅搜索集群级资源。</span>
          </div>
        </div>
        <div className="max-h-[60vh] overflow-y-auto px-3 pb-4 scrollbar-thin">
          {!enabled ? <p className="px-2 py-8 text-center text-sm text-muted-foreground">输入资源名称开始搜索</p> : null}
          {enabled && failedGroups.length ? <div role="alert" className="mx-2 my-2 flex items-center justify-between gap-3 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning"><span>{failedGroups.join("、")}资源加载失败</span><Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => { void nodes.refetch(); void workloads.refetch(); void pods.refetch(); void resources.refetch(); }}>重试</Button></div> : null}
          {enabled && resources.data?.warnings?.length ? <div role="status" className="mx-2 my-2 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">{resources.data.warnings.join("；")}</div> : null}
          {loading ? <div className="space-y-2 p-2">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="h-10 w-full" />)}</div> : null}
          {empty ? <p className="px-2 py-8 text-center text-sm text-muted-foreground">没有找到匹配资源</p> : null}
          {nodes.data?.items.length ? <SearchGroup label="节点" icon={Server}>{nodes.data.items.map((item) => (
            <SearchRow key={item.name} title={item.name} subtitle={item.internalIP} status={item.status} onClick={() => closeAndNavigate(() => navigate({ to: `/nodes/${encodeURIComponent(item.name)}` }))} />
          ))}</SearchGroup> : null}
          {workloads.data?.items.length ? <SearchGroup label="工作负载" icon={Boxes}>{workloads.data.items.map((item) => (
            <SearchRow key={`${item.kind}/${item.namespace}/${item.name}`} title={item.name} subtitle={`${item.kind} · ${item.namespace}`} status={item.status} onClick={() => closeAndNavigate(() => navigate({ to: `/workloads/${item.kind.toLowerCase()}/${encodeURIComponent(item.namespace)}/${encodeURIComponent(item.name)}` }))} />
          ))}</SearchGroup> : null}
          {pods.data?.items.length ? <SearchGroup label="Pod" icon={CircleDot}>{pods.data.items.slice(0, 8).map((item) => (
            <SearchRow key={`${item.namespace}/${item.name}`} title={item.name} subtitle={`${item.namespace} · ${item.node}`} status={item.status} onClick={() => closeAndNavigate(() => navigate({ to: `/pods/${encodeURIComponent(item.namespace)}/${encodeURIComponent(item.name)}` }))} />
          ))}</SearchGroup> : null}
          {resources.data?.items.length ? <SearchGroup label="通用资源" icon={Layers3}>{resources.data.items.slice(0, 10).map((item) => (
            <SearchRow
              key={`${item.apiVersion}/${item.resource}/${item.namespace}/${item.name}`}
              title={item.name}
              subtitle={`${item.kind} · ${item.namespace || "集群级"}`}
              onClick={() => closeAndNavigate(() => {
                const slash = item.apiVersion.indexOf("/");
                const group = slash >= 0 ? item.apiVersion.slice(0, slash) : "_core";
                const version = slash >= 0 ? item.apiVersion.slice(slash + 1) : item.apiVersion;
                void navigate({
                  to: "/resources/$group/$version/$resource/$name",
                  params: { group, version, resource: item.resource || item.kind.toLowerCase() + "s", name: item.name },
                  search: { namespace: item.namespace },
                });
              })}
            />
          ))}</SearchGroup> : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}

function SearchGroup({ label, icon: Icon, children }: { label: string; icon: typeof Server; children: React.ReactNode }) {
  return (
    <div className="py-2">
      <div className="flex items-center gap-2 px-2 pb-1 text-xs font-medium uppercase tracking-wide text-muted-foreground"><Icon className="h-3.5 w-3.5" />{label}</div>
      <div className="space-y-0.5">{children}</div>
    </div>
  );
}

function SearchRow({ title, subtitle, status, onClick }: { title: string; subtitle: string; status?: string; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} className="flex w-full items-center justify-between gap-4 rounded-md px-2 py-2 text-left hover:bg-accent">
      <div className="min-w-0"><p className="truncate text-sm font-medium">{title}</p><p className="truncate text-xs text-muted-foreground">{subtitle}</p></div>
      {status ? <StatusBadge status={status} /> : null}
    </button>
  );
}

