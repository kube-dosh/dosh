import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Boxes, Loader2, Pencil, RotateCcw, Search } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { ImageReferencePicker } from "@/features/registries/image-reference-picker";
import {
  addImagePullSecret,
  allTemplateContainers,
  cloneTemplateDocument,
  containerImage,
  containerKey,
  containerName,
  containerRegistrySecret,
  templateContainers,
  type PodContainerDocument,
} from "@/features/workloads/pod-template-document";
import { cn } from "@/lib/utils";

type ImageDraft = { image: string; registryId?: string };

export function WorkloadImageUpdateDialog({ open, onOpenChange, kind, namespace, name, focusContainer, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  kind: string;
  namespace: string;
  name: string;
  focusContainer?: string;
  onSaved: () => Promise<unknown> | unknown;
}) {
  const queryClient = useQueryClient();
  const [drafts, setDrafts] = useState<Record<string, ImageDraft>>({});
  const [search, setSearch] = useState("");
  const [changeCause, setChangeCause] = useState("");
  const [conflict, setConflict] = useState("");
  const [serverResourceVersion, setServerResourceVersion] = useState("");
  const initializedRef = useRef(false);
  const query = useQuery({
    queryKey: ["workload-pod-template", kind, namespace, name],
    queryFn: () => api.workloadPodTemplate(kind, namespace, name),
    enabled: open,
    staleTime: 0,
  });
  const registries = useQuery({
    queryKey: ["container-registries"],
    queryFn: api.containerRegistries,
    enabled: open,
    staleTime: 30_000,
  });
  const rows = useMemo(() => query.data ? allTemplateContainers(query.data.template).map(({ container, init }) => ({
    container, init, key: containerKey(init, containerName(container)),
  })) : [], [query.data]);
  const visibleRows = useMemo(() => {
    const needle = search.trim().toLowerCase();
    if (!needle) return rows;
    return rows.filter(({ container }) => `${containerName(container)} ${containerImage(container)}`.toLowerCase().includes(needle));
  }, [rows, search]);
  const changedRows = rows.filter(({ key, container }) => {
    const draft = drafts[key];
    return Boolean(draft && draft.image.trim() && draft.image.trim() !== containerImage(container));
  });

  useEffect(() => {
    if (!open) {
      initializedRef.current = false;
      return;
    }
    if (!query.data || initializedRef.current) return;
    const next: Record<string, ImageDraft> = {};
    for (const { container, init } of allTemplateContainers(query.data.template)) {
      next[containerKey(init, containerName(container))] = { image: containerImage(container) };
    }
    setDrafts(next);
    setServerResourceVersion(query.data.resourceVersion ?? "");
    setSearch("");
    setChangeCause("");
    setConflict("");
    initializedRef.current = true;
  }, [open, query.data]);

  useEffect(() => {
    if (!open || !focusContainer) return;
    window.setTimeout(() => {
      document.getElementById(`workload-image-row-${CSS.escape(focusContainer)}`)?.scrollIntoView({ block: "center", behavior: "smooth" });
    }, 80);
  }, [focusContainer, open, visibleRows.length]);

  const submit = useMutation({
    mutationFn: async () => {
      if (!query.data) throw new Error("模板尚未加载");
      const document = cloneTemplateDocument(query.data.template);
      const registryItems = registries.data ?? [];
      for (const { container, init, key } of rows) {
        const draft = drafts[key];
        if (!draft || !draft.image.trim() || draft.image.trim() === containerImage(container)) continue;
        const target = templateContainers(document, init).find((item) => containerName(item) === containerName(container));
        if (!target) throw new Error(`容器 ${containerName(container)} 已不存在，请刷新后重试`);
        target.image = draft.image.trim();
        const secret = containerRegistrySecret(registryItems, draft.registryId);
        if (secret) addImagePullSecret(document, secret);
      }
      return api.updateWorkloadPodTemplate(kind, namespace, name, {
        resourceVersion: serverResourceVersion,
        template: document,
        changeCause: changeCause.trim() || `Dosh: 更新 ${changedRows.length} 个容器镜像`,
        source: "form",
        acknowledgeRisks: false,
      });
    },
    onSuccess: async (result) => {
      if (!result.changed) {
        toast.info("没有检测到镜像变化");
        return;
      }
      toast.success(`已更新 ${changedRows.length} 个容器镜像，滚动发布已触发`);
      onOpenChange(false);
      await queryClient.invalidateQueries({ queryKey: ["workload-container-inventory", kind, namespace, name] });
      await queryClient.invalidateQueries({ queryKey: ["workloads"] });
      await onSaved();
    },
    onError: async (error) => {
      if (error instanceof ApiError && error.status === 409) {
        setConflict("工作负载已被其他操作更新。你的镜像草稿仍然保留，已刷新服务端版本；请核对当前镜像后再次提交。");
        try {
          const latest = await queryClient.fetchQuery({
            queryKey: ["workload-pod-template", kind, namespace, name],
            queryFn: () => api.workloadPodTemplate(kind, namespace, name),
            staleTime: 0,
          });
          setServerResourceVersion(latest.resourceVersion ?? "");
        } catch {
          setConflict("工作负载已被其他操作更新，且最新版本读取失败。你的草稿仍保留，请关闭后重新打开核对。");
        }
        return;
      }
      toast.error(error instanceof Error ? error.message : "更新容器镜像失败");
    },
  });

  const updateDraft = (key: string, next: ImageDraft) => setDrafts((current) => ({ ...current, [key]: next }));
  const resetDraft = (key: string, container: PodContainerDocument) => updateDraft(key, { image: containerImage(container) });

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!submit.isPending) onOpenChange(next); }}>
      <DialogContent className="flex h-[min(92dvh,880px)] w-[calc(100vw-1rem)] max-w-5xl flex-col gap-0 overflow-hidden p-0 sm:w-[calc(100%-2rem)]">
        <DialogHeader className="shrink-0 border-b px-4 py-4 pr-12 sm:px-6">
          <DialogTitle className="flex items-center gap-2"><Pencil className="h-5 w-5 text-primary" />更新容器镜像</DialogTitle>
          <DialogDescription className="break-words">{namespace}/{name} · 业务容器与 Init Container 可一次性原子更新，只触发一次模板变更。</DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          <div className="space-y-4 p-4 sm:p-6">
            {query.isLoading ? <div className="flex items-center justify-center gap-2 py-16 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />正在读取 Pod 模板</div> : null}
            {query.isError ? <div className="rounded-md border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm text-destructive-strong">{query.error.message}</div> : null}
            {query.data ? <>
              <div className="flex min-w-0 flex-col gap-3 rounded-lg border bg-muted/20 p-3 sm:flex-row sm:items-center">
                <div className="relative min-w-0 flex-1">
                  <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                  <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder="搜索容器名称或镜像" className="pl-8" />
                </div>
                <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                  <Badge variant="outline">{query.data.kind}</Badge>
                  <span>共 {rows.length} 个容器</span>
                  <Badge variant={changedRows.length ? "default" : "secondary"}>已修改 {changedRows.length}</Badge>
                </div>
              </div>

              {conflict ? <div role="alert" className="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2.5 text-sm text-warning"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" /><span className="min-w-0 break-words">{conflict}</span></div> : null}
              {query.data.restrictions?.length ? <div className="rounded-md border px-3 py-2 text-sm text-muted-foreground">{query.data.restrictions.join("；")}</div> : null}

              <div className="space-y-3">
                {visibleRows.length ? visibleRows.map(({ container, init, key }) => {
                  const containerNameValue = containerName(container);
                  const currentImage = containerImage(container);
                  const draft = drafts[key] ?? { image: currentImage };
                  const changed = draft.image.trim() !== currentImage;
                  return (
                    <div id={`workload-image-row-${CSS.escape(containerNameValue)}`} key={key} className={cn("min-w-0 rounded-lg border bg-card p-3 sm:p-4", changed && "border-primary/40 bg-primary/[0.03]")}>
                      <div className="mb-3 flex min-w-0 flex-wrap items-center gap-2">
                        <span className="min-w-0 break-all font-mono text-sm font-medium">{containerNameValue}</span>
                        <Badge variant={init ? "outline" : "secondary"}>{init ? "Init Container" : "业务容器"}</Badge>
                        {changed ? <Badge>已修改</Badge> : null}
                        <Button type="button" variant="ghost" size="sm" className="ml-auto h-7 px-2" disabled={!changed} onClick={() => resetDraft(key, container)}><RotateCcw className="h-3.5 w-3.5" />撤销</Button>
                      </div>
                      <div className="mb-3 min-w-0 rounded-md border border-dashed bg-muted/20 px-3 py-2">
                        <p className="text-[11px] text-muted-foreground">当前镜像</p>
                        <p className="mt-1 break-all font-mono text-xs">{currentImage || "—"}</p>
                      </div>
                      <ImageReferencePicker
                        id={`image-${init ? "init" : "container"}-${containerNameValue}`}
                        value={draft.image}
                        registryId={draft.registryId}
                        onChange={(image, registryId) => updateDraft(key, { image, registryId })}
                        label="镜像地址"
                        hint="支持镜像仓、仓库、Tag 搜索，也可以手动输入完整镜像地址。"
                      />
                    </div>
                  );
                }) : <div className="rounded-lg border border-dashed px-4 py-10 text-center text-sm text-muted-foreground"><Boxes className="mx-auto mb-2 h-6 w-6" />没有匹配的容器</div>}
              </div>

              <div className="space-y-2">
                <label htmlFor="workload-image-change-cause" className="text-sm font-medium">变更说明（可选）</label>
                <Input id="workload-image-change-cause" value={changeCause} onChange={(event) => setChangeCause(event.target.value)} placeholder={`Dosh: 更新 ${changedRows.length || 0} 个容器镜像`} maxLength={256} />
                <p className="text-xs text-muted-foreground">会写入 kubernetes.io/change-cause，并显示在工作负载发布历史中。</p>
              </div>

              {query.data.warnings?.length ? <div className="space-y-2 rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm text-warning">
                <p className="flex items-center gap-2 font-medium"><AlertTriangle className="h-4 w-4" />模板已有高风险配置</p>
                <ul className="list-disc space-y-1 pl-5 text-xs leading-5">{query.data.warnings.map((warning) => <li key={warning} className="break-words">{warning}</li>)}</ul>
                <p className="text-xs leading-5 opacity-90">仅修改镜像不会新增这些配置，因此本次操作不要求重复确认；如将镜像与高风险配置一起修改，请使用 Pod 模板编辑器。</p>
              </div> : null}
            </> : null}
          </div>
        </div>

        <DialogFooter className="shrink-0 border-t bg-background px-4 py-3 sm:px-6">
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={submit.isPending}>取消</Button>
          <Button type="button" onClick={() => submit.mutate()} disabled={submit.isPending || !query.data || !changedRows.length}>
            {submit.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Pencil className="h-4 w-4" />}
            确认更新
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

