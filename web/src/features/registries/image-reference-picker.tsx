import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Boxes, Check, ChevronDown, Loader2, Search, Settings2 } from "lucide-react";
import { ApiError, api } from "@/api/client";
import type { ContainerRegistry } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SearchableSelect } from "@/components/ui/searchable-select";
import { cn } from "@/lib/utils";

function useDebouncedValue(value: string, delay = 250) {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [delay, value]);
  return debounced;
}

function registryHost(registry: ContainerRegistry) {
  try {
    return new URL(registry.endpoint).host;
  } catch {
    return registry.endpoint.replace(/^https?:\/\//, "").replace(/\/$/, "");
  }
}

function normalizeRepository(value: string, registry?: ContainerRegistry) {
  let repository = value.trim().replace(/^https?:\/\//, "").replace(/^\/+|\/+$/g, "");
  if (registry) {
    const host = registryHost(registry);
    if (repository.toLowerCase().startsWith(`${host.toLowerCase()}/`)) {
      repository = repository.slice(host.length + 1);
    }
  }
  return repository;
}

function initialRegistry(registries: ContainerRegistry[]) {
  return registries.find((registry) => registry.default)?.id ?? registries[0]?.id ?? "";
}

function registryRequestFailure(label: string, error: unknown, registry?: ContainerRegistry) {
  const details: string[] = [];
  if (error instanceof ApiError) {
    const detail = error.detail && error.detail !== error.message ? error.detail : error.message;
    if (detail) details.push(detail);
    if (error.requestId) details.push(`请求 ID ${error.requestId}`);
  } else if (error instanceof Error && error.message) {
    details.push(error.message);
  }
  if (registry?.lastTest && !registry.lastTest.ok && registry.lastTest.message) {
    details.push(`最近连接测试：${registry.lastTest.message}`);
  }
  const reason = [...new Set(details)].filter(Boolean).join("；");
  return `${label}不可用${reason ? `：${reason}` : ""}。请检查镜像仓地址、HTTPS/CA 配置、统一出站代理 NO_PROXY 和 networkPolicy.containerRegistries 出口策略。`;
}

export function ImageReferencePicker({ id, value, registryId, onChange, label = "容器镜像", placeholder = "registry.example.com/team/app:1.0.0", disabled = false, hint, hideLabel = false }: {
  id: string;
  value: string;
  registryId?: string;
  onChange: (image: string, registryId?: string) => void;
  label?: string;
  placeholder?: string;
  disabled?: boolean;
  hint?: string;
  hideLabel?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [selectedRegistryID, setSelectedRegistryID] = useState("");
  const [restrictedToImageRegistry, setRestrictedToImageRegistry] = useState(false);
  const [repository, setRepository] = useState("");
  const [tag, setTag] = useState("");
  const [repositorySearch, setRepositorySearch] = useState("");
  const [tagSearch, setTagSearch] = useState("");
  const [repositoryListOpen, setRepositoryListOpen] = useState(true);
  const [tagListOpen, setTagListOpen] = useState(false);
  const [manualImage, setManualImage] = useState("");
  const [manualMode, setManualMode] = useState(false);
  const [panelError, setPanelError] = useState("");

  const registries = useQuery({
    queryKey: ["container-registries"],
    queryFn: api.containerRegistries,
    enabled: open,
    staleTime: 30_000,
  });
  const resolution = useQuery({
    queryKey: ["container-registry-resolve", value],
    queryFn: () => api.resolveContainerRegistryImage(value),
    enabled: open && Boolean(value.trim()),
    retry: false,
    staleTime: 30_000,
  });
  const registryItems = registries.data ?? [];
  const selectedRegistry = registryItems.find((registry) => registry.id === selectedRegistryID);
  const visibleRegistries = restrictedToImageRegistry && selectedRegistry ? [selectedRegistry] : registryItems;
  const debouncedRepositorySearch = useDebouncedValue(repositorySearch);
  const debouncedTagSearch = useDebouncedValue(tagSearch);
  const repositories = useQuery({
    queryKey: ["container-registry-repositories", selectedRegistryID, debouncedRepositorySearch],
    queryFn: () => api.containerRegistryRepositories(selectedRegistryID, { q: debouncedRepositorySearch || undefined, pageSize: 200 }),
    enabled: open && Boolean(selectedRegistryID),
    retry: false,
  });
  const tags = useQuery({
    queryKey: ["container-registry-tags", selectedRegistryID, repository, debouncedTagSearch],
    queryFn: () => api.containerRegistryTags(selectedRegistryID, repository, { q: debouncedTagSearch || undefined, pageSize: 200 }),
    enabled: open && Boolean(selectedRegistryID) && Boolean(repository),
    retry: false,
  });

  useEffect(() => {
    if (!open) return;
    setPanelError("");
    setManualImage(value);
    setRepositorySearch("");
    setTagSearch("");
    setRepositoryListOpen(true);
    setTagListOpen(false);
    setTag("");
    setManualMode(false);
    setRestrictedToImageRegistry(false);
    setSelectedRegistryID(registryId ?? "");
    setRepository("");
  }, [open, registryId, value]);

  useEffect(() => {
    if (!open || !resolution.data) return;
    const resolvedRepository = resolution.data.repository ?? "";
    const resolvedTag = resolution.data.tag ?? "";
    setRepository(resolvedRepository);
    setTag(resolvedTag);
    setRepositoryListOpen(!resolvedRepository);
    setTagListOpen(Boolean(resolvedRepository) && !resolvedTag);
    if (resolution.data.matched && resolution.data.registryId) {
      setSelectedRegistryID(resolution.data.registryId);
      setRestrictedToImageRegistry(true);
      setManualMode(false);
      return;
    }
    if (resolution.data.explicitRegistry) {
      setRestrictedToImageRegistry(false);
      setManualMode(true);
      setManualImage(value);
      setRepositoryListOpen(false);
      setTagListOpen(false);
    }
  }, [open, resolution.data, value]);

  useEffect(() => {
    if (!open || selectedRegistryID || manualMode || registryItems.length === 0) return;
    setSelectedRegistryID(initialRegistry(registryItems));
  }, [manualMode, open, registryItems, selectedRegistryID]);

  const registryOptions = useMemo(() => visibleRegistries.map((registry) => ({
    value: registry.id,
    label: `${registry.name} · ${registryHost(registry)}${registry.default ? " · 默认" : ""}`,
  })), [visibleRegistries]);
  const repositoryItems = repositories.data?.items ?? [];
  const tagItems = tags.data?.items ?? [];
  const combinedImage = selectedRegistry && repository.trim() && tag.trim()
    ? `${registryHost(selectedRegistry)}/${normalizeRepository(repository, selectedRegistry)}:${tag.trim()}`
    : "";

  const selectRegistry = (nextID: string) => {
    if (nextID === selectedRegistryID) return;
    setSelectedRegistryID(nextID);
    setRepository("");
    setTag("");
    setRepositorySearch("");
    setTagSearch("");
    setRepositoryListOpen(true);
    setTagListOpen(false);
    setPanelError("");
  };

  const applySelection = () => {
    if (!selectedRegistry) {
      setPanelError("请选择镜像仓");
      return;
    }
    const normalizedRepository = normalizeRepository(repository, selectedRegistry);
    if (!normalizedRepository) {
      setPanelError("请选择或输入仓库路径");
      return;
    }
    if (!tag.trim()) {
      setPanelError("请选择或输入 Tag");
      return;
    }
    const image = `${registryHost(selectedRegistry)}/${normalizedRepository}:${tag.trim()}`;
    onChange(image, selectedRegistry.id);
    setOpen(false);
  };

  const applyManualImage = async () => {
    const image = manualImage.trim();
    if (!image) {
      setPanelError("请输入完整镜像地址");
      return;
    }
    setPanelError("");
    try {
      const resolved = await api.resolveContainerRegistryImage(image);
      onChange(image, resolved.matched ? resolved.registryId : undefined);
      setOpen(false);
    } catch (error) {
      if (error instanceof ApiError && error.status === 400) {
        setPanelError("镜像地址格式不正确");
        return;
      }
      onChange(image, undefined);
      setOpen(false);
    }
  };

  return (
    <>
      <div className="min-w-0 space-y-1.5">
        {hideLabel ? null : <Label htmlFor={id}>{label}</Label>}
        <div className="flex min-w-0 items-stretch gap-2">
          <Input
            id={id}
            value={value}
            onChange={(event) => onChange(event.target.value, undefined)}
            placeholder={placeholder}
            autoComplete="off"
            spellCheck={false}
            className="min-w-0 font-mono"
            disabled={disabled}
          />
          <Button type="button" variant="outline" className="shrink-0" onClick={() => setOpen(true)} disabled={disabled} aria-label="浏览镜像仓">
            <Boxes className="h-4 w-4" />
            <span className="hidden sm:inline">选择镜像</span>
          </Button>
        </div>
        <p className="text-xs text-muted-foreground">{hint ?? (registryId ? "已关联已配置镜像仓，创建或更新时会补充该镜像仓的拉取凭证。" : "可直接输入完整地址，也可从已配置的镜像仓中选择。")}</p>
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="flex w-[calc(100vw-1rem)] max-w-4xl flex-col gap-0 overflow-hidden p-0 sm:w-[calc(100%-2rem)]">
          <DialogHeader className="shrink-0 border-b px-4 py-4 pr-12 sm:px-6">
            <DialogTitle className="flex items-center gap-2"><Boxes className="h-5 w-5 text-primary" />选择容器镜像</DialogTitle>
            <DialogDescription className="break-words">按镜像仓、仓库和 Tag 检索。镜像仓接口不可用时仍可选择仓库并手动输入 Tag。</DialogDescription>
          </DialogHeader>

          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
            <div className="grid min-w-0 gap-5 p-4 sm:p-6 md:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
              <section className="min-w-0 space-y-3" aria-labelledby={`${id}-registry-heading`}>
                <div className="flex min-w-0 items-center justify-between gap-2">
                  <h2 id={`${id}-registry-heading`} className="text-sm font-semibold">1. 镜像仓</h2>
                  {restrictedToImageRegistry ? <Button type="button" variant="link" size="sm" className="h-auto shrink-0 p-0" onClick={() => setRestrictedToImageRegistry(false)}>切换镜像仓</Button> : null}
                </div>
                {registries.isLoading ? (
                  <div className="flex items-center gap-2 rounded-md border bg-muted/30 px-3 py-3 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />正在读取镜像仓…</div>
                ) : registries.isError ? (
                  <div className="rounded-md border border-warning/35 bg-warning/10 px-3 py-3 text-xs text-warning">
                    无法读取镜像仓配置，仍可手动输入完整镜像地址。
                  </div>
                ) : registryItems.length === 0 ? (
                  <div className="space-y-3 rounded-md border bg-muted/30 px-3 py-3 text-xs text-muted-foreground">
                    <p>尚未配置镜像仓。配置后即可搜索仓库与 Tag。</p>
                    <Button asChild type="button" variant="outline" size="sm" className="w-full sm:w-auto"><Link to="/registries" onClick={() => setOpen(false)}><Settings2 className="h-4 w-4" />配置镜像仓</Link></Button>
                  </div>
                ) : (
                  <SearchableSelect
                    id={`${id}-registry`}
                    label="镜像仓"
                    value={selectedRegistryID}
                    options={registryOptions}
                    onChange={selectRegistry}
                    onManualSelect={() => setManualMode(true)}
                    placeholder="选择镜像仓"
                    searchPlaceholder="搜索镜像仓…"
                  />
                )}
                {selectedRegistry ? (
                  <div className="min-w-0 rounded-md border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                    <p className="break-all font-mono">{selectedRegistry.endpoint}</p>
                    <div className="mt-2 flex flex-wrap gap-1.5">
                      <Badge variant={selectedRegistry.default ? "default" : "outline"}>{selectedRegistry.default ? "默认" : "可用"}</Badge>
                      {selectedRegistry.pullSecretName ? <Badge variant="secondary">拉取凭证 {selectedRegistry.pullSecretName}</Badge> : selectedRegistry.authType !== "anonymous" ? <Badge variant="warning">未配置拉取 Secret</Badge> : null}
                      {!selectedRegistry.capabilities.catalog ? <Badge variant="warning">无 Catalog</Badge> : null}
                    </div>
                  </div>
                ) : null}

                {manualMode ? (
                  <div className="min-w-0 space-y-2 rounded-md border p-3">
                    <Label htmlFor={`${id}-manual`}>完整镜像地址</Label>
                    <Input id={`${id}-manual`} value={manualImage} onChange={(event) => setManualImage(event.target.value)} placeholder={placeholder} className="min-w-0 font-mono" autoComplete="off" spellCheck={false} />
                    <p className="break-words text-xs text-muted-foreground">适用于未配置的镜像源、Digest 或一次性调试镜像。</p>
                    <Button type="button" size="sm" className="w-full sm:w-auto" onClick={() => void applyManualImage()}>使用此地址</Button>
                  </div>
                ) : (
                  <Button type="button" variant="link" size="sm" className="h-auto p-0 text-xs" onClick={() => setManualMode(true)}>手动输入完整镜像地址</Button>
                )}
              </section>

              <section className="min-w-0 space-y-4" aria-labelledby={`${id}-repository-heading`}>
                <div className="min-w-0 space-y-3">
                  <h2 id={`${id}-repository-heading`} className="text-sm font-semibold">2. 仓库与 Tag</h2>
                  <div className="min-w-0 space-y-1.5">
                    <div className="flex min-w-0 items-center justify-between gap-2">
                      <Label htmlFor={`${id}-repository`}>仓库路径</Label>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-7 shrink-0 px-2"
                        aria-label={repositoryListOpen ? "收起仓库列表" : "展开仓库列表"}
                        aria-expanded={repositoryListOpen}
                        aria-controls={`${id}-repository-list`}
                        onClick={() => setRepositoryListOpen((current) => !current)}
                        disabled={!selectedRegistry}
                      >
                        <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", repositoryListOpen && "rotate-180")} />
                        {repositoryListOpen ? "收起" : "浏览"}
                      </Button>
                    </div>
                    <Input
                      id={`${id}-repository`}
                      value={repository}
                      onChange={(event) => { setRepository(event.target.value); setTag(""); setRepositoryListOpen(false); setTagListOpen(false); setPanelError(""); }}
                      placeholder="team/application"
                      className="min-w-0 font-mono"
                      disabled={!selectedRegistry}
                      autoComplete="off"
                      spellCheck={false}
                    />
                  </div>
                  {repositoryListOpen ? (
                    <>
                      <div className="relative min-w-0">
                        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                        <Input value={repositorySearch} onChange={(event) => setRepositorySearch(event.target.value)} placeholder="搜索仓库…" aria-label="搜索仓库" className="min-w-0 pl-9" disabled={!selectedRegistry} />
                      </div>
                      <div id={`${id}-repository-list`} className="max-h-48 min-w-0 overflow-y-auto rounded-md border p-1" role="listbox" aria-label="仓库列表">
                        {repositories.isLoading ? <p className="px-3 py-2 text-xs text-muted-foreground">正在读取仓库列表…</p> : null}
                        {repositories.isError ? <p role="alert" className="break-words px-3 py-2 text-xs text-warning">{registryRequestFailure("仓库列表", repositories.error, selectedRegistry)}</p> : null}
                        {!repositories.isLoading && !repositories.isError && repositoryItems.length === 0 ? <p className="px-3 py-2 text-xs text-muted-foreground">没有匹配的仓库，可手动输入。</p> : null}
                        {repositoryItems.map((item) => (
                          <button
                            key={item.name}
                            type="button"
                            role="option"
                            aria-selected={repository === item.name}
                            className={cn("flex w-full min-w-0 items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-accent", repository === item.name && "bg-accent")}
                            onClick={() => { setRepository(item.name); setTag(""); setRepositorySearch(""); setTagSearch(""); setRepositoryListOpen(false); setTagListOpen(true); }}
                          >
                            {repository === item.name ? <Check className="h-3.5 w-3.5 shrink-0" /> : <span className="h-3.5 w-3.5 shrink-0" />}
                            <span className="min-w-0 break-all font-mono">{item.name}</span>
                          </button>
                        ))}
                      </div>
                    </>
                  ) : null}
                </div>

                <div className="min-w-0 space-y-1.5">
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <Label htmlFor={`${id}-tag`}>Tag</Label>
                    <div className="flex shrink-0 items-center gap-1.5">
                      <span className="text-[11px] text-muted-foreground">按版本倒序</span>
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="h-7 shrink-0 px-2"
                        aria-label={tagListOpen ? "收起 Tag 列表" : "展开 Tag 列表"}
                        aria-expanded={tagListOpen}
                        aria-controls={`${id}-tag-list`}
                        onClick={() => setTagListOpen((current) => !current)}
                        disabled={!repository.trim()}
                      >
                        <ChevronDown className={cn("h-3.5 w-3.5 transition-transform", tagListOpen && "rotate-180")} />
                        {tagListOpen ? "收起" : "浏览"}
                      </Button>
                    </div>
                  </div>
                  <Input id={`${id}-tag`} value={tag} onChange={(event) => { setTag(event.target.value); setTagListOpen(false); setPanelError(""); }} placeholder="1.0.0" className="min-w-0 font-mono" disabled={!repository.trim()} autoComplete="off" spellCheck={false} />
                  {tagListOpen ? (
                    <>
                      <div className="relative min-w-0">
                        <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                        <Input value={tagSearch} onChange={(event) => setTagSearch(event.target.value)} placeholder="搜索 Tag…" aria-label="搜索 Tag" className="min-w-0 pl-9" disabled={!repository.trim()} />
                      </div>
                      <div id={`${id}-tag-list`} className="max-h-40 min-w-0 overflow-y-auto rounded-md border p-1" role="listbox" aria-label="Tag 列表">
                        {tags.isLoading ? <p className="px-3 py-2 text-xs text-muted-foreground">正在读取 Tag…</p> : null}
                        {tags.isError ? <p role="alert" className="break-words px-3 py-2 text-xs text-warning">{registryRequestFailure("Tag 列表", tags.error, selectedRegistry)}</p> : null}
                        {repository.trim() && !tags.isLoading && !tags.isError && tagItems.length === 0 ? <p className="px-3 py-2 text-xs text-muted-foreground">没有匹配的 Tag，可手动输入。</p> : null}
                        {tagItems.map((item) => (
                          <button
                            key={item.name}
                            type="button"
                            role="option"
                            aria-selected={tag === item.name}
                            className={cn("flex w-full min-w-0 items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-accent", tag === item.name && "bg-accent")}
                            onClick={() => { setTag(item.name); setTagSearch(""); setTagListOpen(false); setPanelError(""); }}
                          >
                            {tag === item.name ? <Check className="h-3.5 w-3.5 shrink-0" /> : <span className="h-3.5 w-3.5 shrink-0" />}
                            <span className="min-w-0 break-all font-mono">{item.name}</span>
                          </button>
                        ))}
                      </div>
                    </>
                  ) : null}
                </div>
              </section>
            </div>
          </div>

          <div className="shrink-0 border-t bg-card px-4 py-3 sm:px-6">
            {panelError ? <p className="mb-2 break-words text-xs text-destructive-strong">{panelError}</p> : null}
            <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <p className="min-w-0 break-all font-mono text-xs text-muted-foreground">{combinedImage || value || "尚未选择镜像"}</p>
              <div className="flex shrink-0 justify-end gap-2">
                <Button type="button" variant="outline" onClick={() => setOpen(false)}>取消</Button>
                <Button type="button" onClick={applySelection} disabled={!combinedImage}>使用此镜像</Button>
              </div>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

