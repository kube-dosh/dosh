import { useEffect, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type { ServicePortInput, ServiceSummary } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";

const DNS_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const SERVICE_TYPES = ["ClusterIP", "NodePort", "LoadBalancer"] as const;
const PROTOCOLS = ["TCP", "UDP", "SCTP"] as const;

type PortDraft = {
  name: string;
  protocol: string;
  port: string;
  targetPort: string;
};

type FormState = {
  name: string;
  type: string;
  selector: string;
  ports: PortDraft[];
};

function selectorText(selector?: Record<string, string>): string {
  return Object.entries(selector || {}).map(([key, value]) => `${key}=${value}`).join("\n");
}

function initialForm(workloadName: string, selector: Record<string, string>, service?: ServiceSummary): FormState {
  return {
    name: service?.name || workloadName,
    type: service?.type || "ClusterIP",
    selector: selectorText(service?.selector || selector),
    ports: service?.ports.length
      ? service.ports.map((port) => ({
          name: port.name || "",
          protocol: port.protocol || "TCP",
          port: String(port.port),
          targetPort: port.targetPort || String(port.port),
        }))
      : [{ name: "http", protocol: "TCP", port: "80", targetPort: "80" }],
  };
}

function parseSelector(value: string): Record<string, string> | null {
  const selector: Record<string, string> = {};
  for (const rawLine of value.split("\n")) {
    const line = rawLine.trim();
    if (!line) continue;
    const separator = line.indexOf("=");
    if (separator <= 0) return null;
    const key = line.slice(0, separator).trim();
    const labelValue = line.slice(separator + 1).trim();
    if (!key || Object.prototype.hasOwnProperty.call(selector, key)) return null;
    selector[key] = labelValue;
  }
  return Object.keys(selector).length ? selector : null;
}

function parsePorts(ports: PortDraft[]): ServicePortInput[] | null {
  if (!ports.length || ports.length > 10) return null;
  const parsed: ServicePortInput[] = [];
  for (const draft of ports) {
    const port = Number(draft.port);
    const targetPort = draft.targetPort.trim() || String(port);
    if (!Number.isInteger(port) || port < 1 || port > 65535) return null;
    if (/^\d+$/.test(targetPort)) {
      const numeric = Number(targetPort);
      if (!Number.isInteger(numeric) || numeric < 1 || numeric > 65535) return null;
    } else if (targetPort.length > 15 || !/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/.test(targetPort)) return null;
    if (ports.length > 1 && !draft.name.trim()) return null;
    parsed.push({
      name: draft.name.trim() || undefined,
      protocol: draft.protocol,
      port,
      targetPort,
    });
  }
  return parsed;
}

export function ServiceFormDialog({ open, onOpenChange, namespace, workloadName, workloadSelector, service, onSaved }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  namespace: string;
  workloadName: string;
  workloadSelector: Record<string, string>;
  service?: ServiceSummary;
  onSaved: () => Promise<void> | void;
}) {
  const creating = !service;
  const [form, setForm] = useState<FormState>(() => initialForm(workloadName, workloadSelector, service));
  const [formError, setFormError] = useState("");

  useEffect(() => {
    if (!open) return;
    setForm(initialForm(workloadName, workloadSelector, service));
    setFormError("");
  }, [open, service, workloadName, workloadSelector]);

  const save = useMutation({
    mutationFn: async () => {
      const selector = parseSelector(form.selector);
      const ports = parsePorts(form.ports);
      if (creating && (!form.name || form.name.length > 63 || !DNS_LABEL.test(form.name))) {
        throw new Error("Service 名称必须是小写 DNS-1123 标签，且不超过 63 个字符");
      }
      if (!selector) throw new Error("请至少填写一个有效的 Selector 标签，格式为 key=value");
      if (!ports) throw new Error("请填写 1–10 个有效端口；多端口时每个端口都必须命名");
      if (creating) {
        return api.createService({ name: form.name, namespace, type: form.type, selector, ports });
      }
      return api.updateService(namespace, service.name, {
        type: form.type,
        selector,
        ports,
        resourceVersion: service.resourceVersion,
      });
    },
    onSuccess: async (saved) => {
      toast.success(creating ? `Service ${saved.name} 已创建` : `Service ${saved.name} 已更新`);
      await onSaved();
      onOpenChange(false);
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        setFormError("Service 已被其他操作更新，请关闭后刷新再试");
        return;
      }
      setFormError(error.message);
    },
  });

  const update = (patch: Partial<FormState>) => {
    setForm((current) => ({ ...current, ...patch }));
    setFormError("");
  };
  const updatePort = (index: number, patch: Partial<PortDraft>) => {
    setForm((current) => ({
      ...current,
      ports: current.ports.map((port, portIndex) => portIndex === index ? { ...port, ...patch } : port),
    }));
    setFormError("");
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{creating ? "配置访问方式" : "编辑 Service"}</DialogTitle>
          <DialogDescription>
            {namespace} / {workloadName} · Selector 将 Service 关联到此工作负载的 Pod
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="service-name">Service 名称</Label>
              <Input id="service-name" value={form.name} onChange={(event) => update({ name: event.target.value })} disabled={!creating} autoComplete="off" spellCheck={false} />
              <p className="text-xs text-muted-foreground">创建后名称不可修改。</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="service-type">访问类型</Label>
              <Select value={form.type} onValueChange={(value) => update({ type: value })}>
                <SelectTrigger id="service-type" aria-label="访问类型"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {SERVICE_TYPES.map((type) => <SelectItem key={type} value={type}>{type}</SelectItem>)}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">NodePort / LoadBalancer 的节点端口由 Kubernetes 自动分配。</p>
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="service-selector">Selector</Label>
            <Textarea id="service-selector" value={form.selector} onChange={(event) => update({ selector: event.target.value })} className="min-h-24 font-mono text-xs" spellCheck={false} placeholder={"app=web\ncomponent=api"} />
            <p className="text-xs text-muted-foreground">每行一个 key=value，默认已填入工作负载 Selector。</p>
          </div>

          <div className="space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <Label>端口</Label>
                <p className="text-xs text-muted-foreground">Service 端口 → Pod 目标端口，最多 10 个。</p>
              </div>
              <Button type="button" variant="outline" size="sm" onClick={() => update({ ports: [...form.ports, { name: "", protocol: "TCP", port: "", targetPort: "" }] })} disabled={form.ports.length >= 10}>
                <Plus className="h-4 w-4" />添加端口
              </Button>
            </div>
            <div className="space-y-2">
              {form.ports.map((port, index) => (
                <div key={index} className="grid min-w-0 gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_110px_110px_110px_auto] sm:items-end">
                  <div className="space-y-1.5"><Label htmlFor={`service-port-name-${index}`} className="text-xs">名称</Label><Input id={`service-port-name-${index}`} value={port.name} onChange={(event) => updatePort(index, { name: event.target.value })} placeholder={form.ports.length > 1 ? "http" : "可选"} /></div>
                  <div className="space-y-1.5"><Label htmlFor={`service-port-protocol-${index}`} className="text-xs">协议</Label><Select value={port.protocol} onValueChange={(value) => updatePort(index, { protocol: value })}><SelectTrigger id={`service-port-protocol-${index}`} aria-label={`端口 ${index + 1} 协议`}><SelectValue /></SelectTrigger><SelectContent>{PROTOCOLS.map((protocol) => <SelectItem key={protocol} value={protocol}>{protocol}</SelectItem>)}</SelectContent></Select></div>
                  <div className="space-y-1.5"><Label htmlFor={`service-port-${index}`} className="text-xs">Service 端口</Label><Input id={`service-port-${index}`} inputMode="numeric" value={port.port} onChange={(event) => updatePort(index, { port: event.target.value })} placeholder="80" /></div>
                  <div className="space-y-1.5"><Label htmlFor={`service-target-port-${index}`} className="text-xs">目标端口</Label><Input id={`service-target-port-${index}`} value={port.targetPort} onChange={(event) => updatePort(index, { targetPort: event.target.value })} placeholder="8080 / http" /></div>
                  <Button type="button" variant="ghost" size="icon" aria-label={`删除端口 ${index + 1}`} onClick={() => update({ ports: form.ports.filter((_, portIndex) => portIndex !== index) })} disabled={form.ports.length === 1}><Trash2 className="h-4 w-4" /></Button>
                </div>
              ))}
            </div>
          </div>
          {formError ? <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive-strong">{formError}</p> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={save.isPending}>取消</Button>
          <Button onClick={() => save.mutate()} disabled={save.isPending}>
            {save.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : null}
            {creating ? "创建 Service" : "保存修改"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
