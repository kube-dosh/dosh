import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { IngressClassSummary, IngressDetail, IngressPathInput, IngressTLS } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

type PathDraft = { host: string; path: string; pathType: string; service: string; port: string };
type TLSDraft = { hosts: string; secretName: string };
type FormState = { name: string; class: string; paths: PathDraft[]; tls: TLSDraft[] };

const PATH_TYPES = ["Prefix", "Exact", "ImplementationSpecific"] as const;

function initialForm(ingress?: IngressDetail, defaultClass = ""): FormState {
  return {
    name: ingress?.name || "",
    class: ingress?.class || defaultClass,
    paths: ingress?.paths.length ? ingress.paths.map((path) => ({
      host: path.host || "", path: path.path || "/", pathType: path.pathType || "Prefix", service: path.service || "", port: path.port || "",
    })) : [{ host: "", path: "/", pathType: "Prefix", service: "", port: "" }],
    tls: ingress?.tls?.length ? ingress.tls.map((entry) => ({ hosts: entry.hosts.join(", "), secretName: entry.secretName || "" })) : [],
  };
}

function validate(form: FormState, creating: boolean): { paths: IngressPathInput[]; tls: IngressTLS[] } | string {
  if (creating && !/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(form.name)) return "Ingress 名称必须是小写 DNS-1123 标签";
  if (!form.paths.length) return "至少配置一条路由";
  const paths: IngressPathInput[] = [];
  for (const [index, path] of form.paths.entries()) {
    if (!path.service.trim()) return `第 ${index + 1} 条路由必须填写后端 Service`;
    if (!path.port.trim()) return `第 ${index + 1} 条路由必须填写后端端口`;
    if (!path.path.startsWith("/")) return `第 ${index + 1} 条路由路径必须以 / 开头`;
    paths.push({ host: path.host.trim(), path: path.path.trim(), pathType: path.pathType, service: path.service.trim(), port: path.port.trim() });
  }
  const tls: IngressTLS[] = [];
  for (const [index, entry] of form.tls.entries()) {
    const hosts = entry.hosts.split(",").map((host) => host.trim()).filter(Boolean);
    if (!entry.secretName.trim()) return `第 ${index + 1} 个 TLS 条目必须填写 Secret 名称`;
    if (!hosts.length) return `第 ${index + 1} 个 TLS 条目至少需要一个 Host`;
    tls.push({ hosts, secretName: entry.secretName.trim() });
  }
  return { paths, tls };
}

export function IngressFormDialog({ open, onOpenChange, namespace, ingress, ingressClasses, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  namespace: string;
  ingress?: IngressDetail;
  ingressClasses: IngressClassSummary[];
  onSaved: () => Promise<void> | void;
}) {
  const creating = !ingress;
  const defaultClass = ingressClasses.find((item) => item.default)?.name || ingressClasses[0]?.name || "";
  const [form, setForm] = useState<FormState>(() => initialForm(ingress, defaultClass));
  const [formError, setFormError] = useState("");
  useEffect(() => {
    if (!open) return;
    setForm(initialForm(ingress, defaultClass));
    setFormError("");
  }, [open, ingress, defaultClass]);
  const save = useMutation({
    mutationFn: async () => {
      const parsed = validate(form, creating);
      if (typeof parsed === "string") throw new Error(parsed);
      if (creating) {
        return api.createIngress({ name: form.name, namespace, class: form.class || undefined, paths: parsed.paths, tls: parsed.tls });
      }
      return api.updateIngress(namespace, ingress.name, {
        class: form.class || undefined,
        paths: parsed.paths,
        tls: parsed.tls,
        annotations: ingress.annotations,
        resourceVersion: ingress.resourceVersion,
      });
    },
    onSuccess: async (saved) => {
      toast.success(creating ? `Ingress ${saved.name} 已创建` : `Ingress ${saved.name} 已更新`);
      await onSaved();
      onOpenChange(false);
    },
    onError: (error) => {
      setFormError(error instanceof ApiError && error.status === 409 ? "Ingress 已被其他操作更新，请关闭后刷新再试" : error.message);
    },
  });
  const update = (patch: Partial<FormState>) => { setForm((current) => ({ ...current, ...patch })); setFormError(""); };
  const updatePath = (index: number, patch: Partial<PathDraft>) => update({ paths: form.paths.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item) });
  const updateTLS = (index: number, patch: Partial<TLSDraft>) => update({ tls: form.tls.map((item, itemIndex) => itemIndex === index ? { ...item, ...patch } : item) });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-4xl">
        <DialogHeader>
          <DialogTitle>{creating ? "创建 Ingress" : "编辑 Ingress"}</DialogTitle>
          <DialogDescription>{namespace} · 配置 Host、路径、后端 Service 与 TLS。Annotations 可通过 YAML 维护。</DialogDescription>
        </DialogHeader>
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2"><Label htmlFor="ingress-name">Ingress 名称</Label><Input id="ingress-name" value={form.name} onChange={(event) => update({ name: event.target.value })} disabled={!creating} autoComplete="off" spellCheck={false} /></div>
            <div className="space-y-2"><Label htmlFor="ingress-class">IngressClass</Label>
              <Select value={form.class || "__none__"} onValueChange={(value) => update({ class: value === "__none__" ? "" : value })}>
                <SelectTrigger id="ingress-class" aria-label="IngressClass"><SelectValue placeholder="选择 IngressClass" /></SelectTrigger>
                <SelectContent><SelectItem value="__none__">使用集群默认</SelectItem>{ingressClasses.map((item) => <SelectItem key={item.name} value={item.name}>{item.name}{item.default ? " · Default" : ""}</SelectItem>)}</SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3"><div><Label>路由规则</Label><p className="text-xs text-muted-foreground">Host 留空表示匹配所有 Host。</p></div><Button type="button" variant="outline" size="sm" onClick={() => update({ paths: [...form.paths, { host: "", path: "/", pathType: "Prefix", service: "", port: "" }] })}><Plus className="h-4 w-4" />添加路由</Button></div>
            {form.paths.map((path, index) => (
              <div key={index} className="grid min-w-0 gap-2 rounded-md border p-3 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_150px_minmax(0,1fr)_100px_auto] lg:items-end">
                <div className="space-y-1.5"><Label htmlFor={`ingress-host-${index}`} className="text-xs">Host</Label><Input id={`ingress-host-${index}`} value={path.host} onChange={(event) => updatePath(index, { host: event.target.value })} placeholder="app.example.com" /></div>
                <div className="space-y-1.5"><Label htmlFor={`ingress-path-${index}`} className="text-xs">Path</Label><Input id={`ingress-path-${index}`} value={path.path} onChange={(event) => updatePath(index, { path: event.target.value })} placeholder="/" /></div>
                <div className="space-y-1.5"><Label htmlFor={`ingress-path-type-${index}`} className="text-xs">匹配类型</Label><Select value={path.pathType} onValueChange={(value) => updatePath(index, { pathType: value })}><SelectTrigger id={`ingress-path-type-${index}`} aria-label={`路由 ${index + 1} 匹配类型`}><SelectValue /></SelectTrigger><SelectContent>{PATH_TYPES.map((type) => <SelectItem key={type} value={type}>{type}</SelectItem>)}</SelectContent></Select></div>
                <div className="space-y-1.5"><Label htmlFor={`ingress-service-${index}`} className="text-xs">后端 Service</Label><Input id={`ingress-service-${index}`} value={path.service} onChange={(event) => updatePath(index, { service: event.target.value })} placeholder="web" /></div>
                <div className="space-y-1.5"><Label htmlFor={`ingress-port-${index}`} className="text-xs">端口</Label><Input id={`ingress-port-${index}`} value={path.port} onChange={(event) => updatePath(index, { port: event.target.value })} placeholder="80 / http" /></div>
                <Button type="button" variant="ghost" size="icon" aria-label={`删除路由 ${index + 1}`} onClick={() => update({ paths: form.paths.filter((_, itemIndex) => itemIndex !== index) })} disabled={form.paths.length === 1}><Trash2 className="h-4 w-4" /></Button>
              </div>
            ))}
          </div>
          <div className="space-y-3">
            <div className="flex items-center justify-between gap-3"><div><Label>TLS</Label><p className="text-xs text-muted-foreground">指定证书 Secret，可配置多组 Host。</p></div><Button type="button" variant="outline" size="sm" onClick={() => update({ tls: [...form.tls, { hosts: "", secretName: "" }] })}><Plus className="h-4 w-4" />添加 TLS</Button></div>
            {form.tls.map((entry, index) => (
              <div key={index} className="grid min-w-0 gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
                <div className="space-y-1.5"><Label htmlFor={`ingress-tls-hosts-${index}`} className="text-xs">Hosts</Label><Input id={`ingress-tls-hosts-${index}`} value={entry.hosts} onChange={(event) => updateTLS(index, { hosts: event.target.value })} placeholder="app.example.com, www.example.com" /></div>
                <div className="space-y-1.5"><Label htmlFor={`ingress-tls-secret-${index}`} className="text-xs">TLS Secret</Label><Input id={`ingress-tls-secret-${index}`} value={entry.secretName} onChange={(event) => updateTLS(index, { secretName: event.target.value })} placeholder="app-tls" /></div>
                <Button type="button" variant="ghost" size="icon" aria-label={`删除 TLS ${index + 1}`} onClick={() => update({ tls: form.tls.filter((_, itemIndex) => itemIndex !== index) })}><Trash2 className="h-4 w-4" /></Button>
              </div>
            ))}
          </div>
          {formError ? <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{formError}</p> : null}
        </div>
        <DialogFooter><Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button><Button onClick={() => save.mutate()} disabled={save.isPending}>{save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}{creating ? "创建 Ingress" : "保存修改"}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
