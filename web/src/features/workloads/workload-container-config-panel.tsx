import { useState, type ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import { AlertCircle, Boxes, Edit3, FolderOpen, HeartPulse, Loader2, Plus, Trash2, Variable } from "lucide-react";
import { toast } from "sonner";
import { ApiError, api } from "@/api/client";
import type {
  WorkloadContainer,
  WorkloadContainerConfigUpdateInput,
  WorkloadDetail,
  WorkloadEnvVar,
  WorkloadEnvValueFrom,
  WorkloadProbe,
  WorkloadVolume,
  WorkloadVolumeMount,
} from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";

type ProbeSlot = "livenessProbe" | "readinessProbe" | "startupProbe";
type SourceKind = "literal" | WorkloadEnvValueFrom["kind"];

type ContainerConfigPatch = {
  env?: WorkloadEnvVar[];
  volumeMounts?: WorkloadVolumeMount[];
  livenessProbe?: WorkloadProbe | null;
  readinessProbe?: WorkloadProbe | null;
  startupProbe?: WorkloadProbe | null;
};

type EnvRow = {
  name: string;
  value: string;
  sourceKind: SourceKind;
  sourceName: string;
  sourceKey: string;
  optional: boolean;
};

const probeLabels: Record<ProbeSlot, string> = {
  livenessProbe: "存活检查",
  readinessProbe: "就绪检查",
  startupProbe: "启动检查",
};

export function WorkloadContainerConfigPanel({ workload, canEdit, onSaved }: {
  workload: WorkloadDetail;
  canEdit: boolean;
  onSaved: () => Promise<void>;
}) {
  const [envEditor, setEnvEditor] = useState<WorkloadContainer | null>(null);
  const [mountEditor, setMountEditor] = useState<WorkloadContainer | null>(null);
  const [probeEditor, setProbeEditor] = useState<{ container: WorkloadContainer; slot: ProbeSlot } | null>(null);
  const saveMutation = useMutation({
    mutationFn: ({ container, patch }: { container: WorkloadContainer; patch: ContainerConfigPatch }) => {
      const input: WorkloadContainerConfigUpdateInput = {
        env: patch.env ?? container.env,
        volumeMounts: patch.volumeMounts ?? container.volumeMounts,
        livenessProbe: hasPatch(patch, "livenessProbe") ? patch.livenessProbe ?? undefined : container.livenessProbe,
        readinessProbe: hasPatch(patch, "readinessProbe") ? patch.readinessProbe ?? undefined : container.readinessProbe,
        startupProbe: hasPatch(patch, "startupProbe") ? patch.startupProbe ?? undefined : container.startupProbe,
        resourceVersion: workload.resourceVersion,
      };
      return api.updateWorkloadContainerConfig(workload.kind.toLowerCase(), workload.namespace, workload.name, container.name, input);
    },
    onSuccess: async () => {
      toast.success("容器配置已更新，滚动发布已触发");
      await onSaved();
    },
    onError: (error) => {
      if (error instanceof ApiError && error.status === 409) {
        toast.error("工作负载已被其他操作更新，请刷新后重试");
      } else {
        toast.error(error instanceof Error ? error.message : "更新容器配置失败");
      }
    },
  });

  const save = async (container: WorkloadContainer, patch: ContainerConfigPatch) => {
    await saveMutation.mutateAsync({ container, patch });
  };

  if (!workload.containers.length) {
    return <Card><CardContent className="flex flex-col items-center gap-2 py-12 text-sm text-muted-foreground"><Boxes className="h-7 w-7" /><span>该工作负载没有容器配置</span></CardContent></Card>;
  }

  const volumeBySource = new Map(workload.volumes.map((volume) => [volume.name, volume]));
  return (
    <>
      <div className="space-y-4">
        {workload.containers.map((container) => (
          <Card key={container.name} className="min-w-0 overflow-hidden">
            <CardHeader className="gap-3 border-b bg-muted/20 py-4">
              <div className="flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="min-w-0">
                  <CardTitle className="flex flex-wrap items-center gap-2"><Boxes className="h-4 w-4 text-muted-foreground" /><span className="break-all font-mono text-sm">{container.name}</span><Badge variant="outline">容器</Badge></CardTitle>
                  <p className="mt-2 break-all font-mono text-xs text-muted-foreground">{container.image}</p>
                </div>
                <div className="flex flex-wrap gap-1.5 text-xs">
                  <Badge variant="secondary">{container.env.length} 个环境变量</Badge>
                  <Badge variant="secondary">{container.volumeMounts.length} 个挂载</Badge>
                  <Badge variant={container.readinessProbe ? "success" : "outline"}>{container.readinessProbe ? "已配置就绪检查" : "未配置就绪检查"}</Badge>
                </div>
              </div>
            </CardHeader>
            <CardContent className="space-y-5 pt-5">
              <ContainerRuntimeSummary container={container} />
              <ConfigSection
                icon={<Variable className="h-4 w-4" />}
                title="环境变量"
                description="支持直接赋值，以及从 ConfigMap、Secret、字段和资源信息引用。"
                canEdit={canEdit}
                onEdit={() => setEnvEditor(container)}
              >
                <EnvironmentList env={container.env} />
              </ConfigSection>
              <ConfigSection
                icon={<FolderOpen className="h-4 w-4" />}
                title="数据存储"
                description="展示容器挂载路径、只读设置和对应 Pod 模板卷来源。"
                canEdit={canEdit}
                onEdit={() => setMountEditor(container)}
              >
                <VolumeMountList mounts={container.volumeMounts} volumes={volumeBySource} />
              </ConfigSection>
              <ConfigSection
                icon={<HeartPulse className="h-4 w-4" />}
                title="健康检查"
                description="配置 HTTP、TCP、Exec 或 gRPC 探针，滚动发布期间用于判断实例健康状态。"
                canEdit={false}
              >
                <div className="grid gap-3 lg:grid-cols-3">
                  {(["livenessProbe", "readinessProbe", "startupProbe"] as ProbeSlot[]).map((slot) => (
                    <ProbeSummary
                      key={slot}
                      label={probeLabels[slot]}
                      probe={container[slot]}
                      canEdit={canEdit}
                      onEdit={() => setProbeEditor({ container, slot })}
                    />
                  ))}
                </div>
              </ConfigSection>
            </CardContent>
          </Card>
        ))}
      </div>

      {envEditor ? (
        <EnvironmentEditorDialog
          key={`env-${envEditor.name}`}
          container={envEditor}
          onClose={() => setEnvEditor(null)}
          onSave={(env) => save(envEditor, { env })}
        />
      ) : null}
      {mountEditor ? (
        <MountEditorDialog
          key={`mount-${mountEditor.name}`}
          container={mountEditor}
          volumes={workload.volumes}
          onClose={() => setMountEditor(null)}
          onSave={(volumeMounts) => save(mountEditor, { volumeMounts })}
        />
      ) : null}
      {probeEditor ? (
        <ProbeEditorDialog
          key={`probe-${probeEditor.container.name}-${probeEditor.slot}`}
          slot={probeEditor.slot}
          probe={probeEditor.container[probeEditor.slot]}
          onClose={() => setProbeEditor(null)}
          onSave={(probe) => save(probeEditor.container, { [probeEditor.slot]: probe ?? null })}
        />
      ) : null}
    </>
  );
}

function hasPatch(patch: ContainerConfigPatch, key: keyof ContainerConfigPatch): boolean {
  return Object.prototype.hasOwnProperty.call(patch, key);
}

function ContainerRuntimeSummary({ container }: { container: WorkloadContainer }) {
  return (
    <dl className="grid gap-3 rounded-lg border bg-muted/15 p-3 text-sm sm:grid-cols-2 xl:grid-cols-4">
      <div className="min-w-0"><dt className="text-xs text-muted-foreground">容器端口</dt><dd className="mt-1 break-words font-mono text-xs">{container.ports.length ? container.ports.map((port) => `${port.name ? `${port.name}:` : ""}${port.containerPort}/${port.protocol}`).join(", ") : "—"}</dd></div>
      <div className="min-w-0"><dt className="text-xs text-muted-foreground">CPU 请求 / 限制</dt><dd className="mt-1 font-mono text-xs">{formatMilliCPU(container.resources.cpuRequestMilli)} / {formatMilliCPU(container.resources.cpuLimitMilli)}</dd></div>
      <div className="min-w-0"><dt className="text-xs text-muted-foreground">内存请求 / 限制</dt><dd className="mt-1 font-mono text-xs">{formatBytes(container.resources.memoryRequestBytes)} / {formatBytes(container.resources.memoryLimitBytes)}</dd></div>
      <div className="min-w-0"><dt className="text-xs text-muted-foreground">探针状态</dt><dd className="mt-1 flex flex-wrap gap-1">{(container.livenessProbe || container.readinessProbe || container.startupProbe) ? <Badge variant="success">已配置</Badge> : <Badge variant="outline">未配置</Badge>}</dd></div>
    </dl>
  );
}

function ConfigSection({ icon, title, description, canEdit, onEdit, children }: {
  icon: ReactNode;
  title: string;
  description: string;
  canEdit: boolean;
  onEdit?: () => void;
  children: ReactNode;
}) {
  return (
    <section className="min-w-0 rounded-lg border">
      <div className="flex flex-col gap-2 border-b bg-muted/15 px-3 py-2.5 sm:flex-row sm:items-center sm:justify-between">
        <div className="min-w-0"><h3 className="flex items-center gap-2 text-sm font-medium">{icon}{title}</h3><p className="mt-1 text-xs text-muted-foreground">{description}</p></div>
        {canEdit && onEdit ? <Button type="button" variant="outline" size="sm" className="shrink-0" onClick={onEdit}><Edit3 className="h-3.5 w-3.5" />编辑{title}</Button> : null}
      </div>
      <div className="min-w-0 p-3">{children}</div>
    </section>
  );
}

function EnvironmentList({ env }: { env: WorkloadEnvVar[] }) {
  if (!env.length) return <EmptyHint text="未配置环境变量" />;
  return (
    <div className="space-y-2">
      {env.map((variable) => (
        <div key={variable.name} className="grid min-w-0 gap-2 rounded-md border bg-card px-3 py-2 text-sm sm:grid-cols-[minmax(120px,0.8fr)_minmax(0,2fr)]">
          <div className="break-all font-mono text-xs font-medium">{variable.name}</div>
          {variable.valueFrom ? <div className="min-w-0 break-words text-xs text-muted-foreground"><Badge variant="outline" className="mr-1.5">{envSourceLabel(variable.valueFrom.kind)}</Badge><span className="font-mono">{variable.valueFrom.name || "—"}{variable.valueFrom.key ? ` / ${variable.valueFrom.key}` : ""}</span>{variable.valueFrom.optional ? <span className="ml-1 text-warning">可选</span> : null}</div> : <pre className="soft-wrap min-w-0 overflow-hidden whitespace-pre-wrap break-words font-mono text-xs text-muted-foreground">{variable.value || "（空值）"}</pre>}
        </div>
      ))}
    </div>
  );
}

function VolumeMountList({ mounts, volumes }: { mounts: WorkloadVolumeMount[]; volumes: Map<string, WorkloadVolume> }) {
  if (!mounts.length) return <EmptyHint text="未配置存储挂载" />;
  return (
    <div className="space-y-2">
      {mounts.map((mount) => {
        const volume = volumes.get(mount.name);
        return (
          <div key={`${mount.name}-${mount.mountPath}`} className="grid min-w-0 gap-2 rounded-md border bg-card px-3 py-2 text-sm sm:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
            <div className="min-w-0"><div className="break-all font-mono text-xs font-medium">{mount.mountPath}</div><div className="mt-1 text-xs text-muted-foreground">卷 {mount.name}{mount.subPath ? ` · 子路径 ${mount.subPath}` : ""}</div></div>
            <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-xs text-muted-foreground"><Badge variant="outline">{volume ? volume.type : "未知卷"}</Badge><span className="break-all">{volume?.source || "—"}</span>{mount.readOnly ? <Badge variant="secondary">只读</Badge> : null}</div>
          </div>
        );
      })}
    </div>
  );
}

function ProbeSummary({ label, probe, canEdit, onEdit }: { label: string; probe?: WorkloadProbe; canEdit: boolean; onEdit: () => void }) {
  return (
    <div className="min-w-0 rounded-md border bg-card p-3">
      <div className="flex items-center justify-between gap-2"><p className="text-xs font-medium">{label}</p>{canEdit ? <Button type="button" variant="ghost" size="sm" className="h-7 px-2" aria-label={`编辑${label}`} onClick={onEdit}><Edit3 className="h-3.5 w-3.5" />编辑</Button> : null}</div>
      {probe ? <div className="mt-2 space-y-2"><p className="break-words font-mono text-xs">{probeSummary(probe)}</p><div className="flex flex-wrap gap-1.5 text-[11px] text-muted-foreground"><span>延迟 {probe.initialDelaySeconds}s</span><span>周期 {probe.periodSeconds}s</span><span>超时 {probe.timeoutSeconds}s</span><span>失败阈值 {probe.failureThreshold}</span></div></div> : <p className="mt-2 text-xs text-muted-foreground">未配置</p>}
    </div>
  );
}

function EmptyHint({ text }: { text: string }) {
  return <p className="flex items-center gap-2 rounded-md border border-dashed px-3 py-4 text-xs text-muted-foreground"><AlertCircle className="h-4 w-4" />{text}</p>;
}

function EnvironmentEditorDialog({ container, onClose, onSave }: {
  container: WorkloadContainer;
  onClose: () => void;
  onSave: (env: WorkloadEnvVar[]) => Promise<void>;
}) {
  const [rows, setRows] = useState<EnvRow[]>(() => container.env.map((variable) => ({
    name: variable.name,
    value: variable.value,
    sourceKind: variable.valueFrom?.kind ?? "literal",
    sourceName: variable.valueFrom?.name ?? "",
    sourceKey: variable.valueFrom?.key ?? "",
    optional: variable.valueFrom?.optional ?? false,
  })));
  const [saving, setSaving] = useState(false);
  const updateRow = (index: number, patch: Partial<EnvRow>) => setRows((current) => current.map((row, i) => i === index ? { ...row, ...patch } : row));
  const valid = rows.every((row) => row.name.trim() && (row.sourceKind === "literal" || row.sourceName.trim()));
  const names = rows.map((row) => row.name.trim()).filter(Boolean);
  const unique = new Set(names).size === names.length;
  const submit = async () => {
    setSaving(true);
    try { await onSave(rows.map(envRowToValue)); onClose(); } catch { /* parent reports the API error */ } finally { setSaving(false); }
  };
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !saving) onClose(); }}>
      <DialogContent className="max-h-[88vh] max-w-3xl overflow-y-auto">
        <DialogHeader><DialogTitle>编辑环境变量</DialogTitle><DialogDescription>{container.name} · 提交后将更新 Pod 模板并触发滚动发布</DialogDescription></DialogHeader>
        <div className="space-y-3">
          {rows.length ? rows.map((row, index) => (
            <div key={index} className="space-y-3 rounded-lg border p-3">
              <div className="flex items-center justify-between gap-2"><span className="text-xs font-medium text-muted-foreground">变量 {index + 1}</span><Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-destructive" onClick={() => setRows((current) => current.filter((_, i) => i !== index))} disabled={saving}><Trash2 className="h-3.5 w-3.5" />删除</Button></div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5"><Label htmlFor={`env-name-${index}`}>变量名称</Label><Input id={`env-name-${index}`} aria-label={`变量 ${index + 1} 名称`} value={row.name} onChange={(event) => updateRow(index, { name: event.target.value })} placeholder="DATABASE_URL" /></div>
                <div className="space-y-1.5"><Label>变量来源</Label><Select value={row.sourceKind} onValueChange={(value) => updateRow(index, { sourceKind: value as SourceKind })}><SelectTrigger aria-label={`变量 ${index + 1} 来源`}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="literal">直接输入</SelectItem><SelectItem value="configMapKeyRef">ConfigMap</SelectItem><SelectItem value="secretKeyRef">Secret</SelectItem><SelectItem value="fieldRef">Pod 字段引用</SelectItem><SelectItem value="resourceFieldRef">容器资源引用</SelectItem></SelectContent></Select></div>
              </div>
              {row.sourceKind === "literal" ? <div className="space-y-1.5"><Label htmlFor={`env-value-${index}`}>变量值</Label><Textarea id={`env-value-${index}`} aria-label={`变量 ${index + 1} 值`} className="min-h-20 font-mono text-xs" value={row.value} onChange={(event) => updateRow(index, { value: event.target.value })} /></div> : (
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5"><Label htmlFor={`env-source-name-${index}`}>{row.sourceKind === "fieldRef" ? "字段路径" : row.sourceKind === "resourceFieldRef" ? "资源名称" : "引用名称"}</Label><Input id={`env-source-name-${index}`} aria-label={`变量 ${index + 1} 引用名称`} value={row.sourceName} onChange={(event) => updateRow(index, { sourceName: event.target.value })} placeholder={row.sourceKind === "fieldRef" ? "metadata.name" : row.sourceKind === "resourceFieldRef" ? "limits.cpu" : "config-name"} /></div>
                  {(row.sourceKind === "configMapKeyRef" || row.sourceKind === "secretKeyRef") ? <div className="space-y-1.5"><Label htmlFor={`env-source-key-${index}`}>键</Label><Input id={`env-source-key-${index}`} aria-label={`变量 ${index + 1} 引用键`} value={row.sourceKey} onChange={(event) => updateRow(index, { sourceKey: event.target.value })} placeholder="key" /></div> : null}
                  {(row.sourceKind === "configMapKeyRef" || row.sourceKind === "secretKeyRef") ? <label className="flex items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={row.optional} onChange={(event) => updateRow(index, { optional: event.target.checked })} />引用不存在时允许启动</label> : null}
                </div>
              )}
            </div>
          )) : <EmptyHint text="尚未配置环境变量" />}
          <Button type="button" variant="outline" size="sm" onClick={() => setRows((current) => [...current, { name: "", value: "", sourceKind: "literal", sourceName: "", sourceKey: "", optional: false }])} disabled={saving || rows.length >= 128}><Plus className="h-4 w-4" />添加环境变量</Button>
          {!unique ? <p className="text-xs text-destructive">变量名称不能重复。</p> : null}
        </div>
        <DialogFooter><Button variant="outline" onClick={onClose} disabled={saving}>取消</Button><Button onClick={submit} disabled={saving || !valid || !unique}>{saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}保存配置</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MountEditorDialog({ container, volumes, onClose, onSave }: {
  container: WorkloadContainer;
  volumes: WorkloadVolume[];
  onClose: () => void;
  onSave: (mounts: WorkloadVolumeMount[]) => Promise<void>;
}) {
  const [rows, setRows] = useState<WorkloadVolumeMount[]>(() => container.volumeMounts.map((mount) => ({ ...mount })));
  const [saving, setSaving] = useState(false);
  const updateRow = (index: number, patch: Partial<WorkloadVolumeMount>) => setRows((current) => current.map((row, i) => i === index ? { ...row, ...patch } : row));
  const valid = rows.every((row) => row.name && row.mountPath.startsWith("/"));
  const mountNames = rows.map((row) => row.name);
  const mountPaths = rows.map((row) => row.mountPath);
  const unique = new Set(mountNames).size === mountNames.length && new Set(mountPaths).size === mountPaths.length;
  const submit = async () => {
    setSaving(true);
    try { await onSave(rows); onClose(); } catch { /* parent reports the API error */ } finally { setSaving(false); }
  };
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !saving) onClose(); }}>
      <DialogContent className="max-h-[88vh] max-w-3xl overflow-y-auto">
        <DialogHeader><DialogTitle>编辑数据存储</DialogTitle><DialogDescription>{container.name} · 只能挂载当前 Pod 模板中已存在的卷</DialogDescription></DialogHeader>
        <div className="space-y-3">
          {volumes.length ? <div className="rounded-md border bg-muted/20 p-3 text-xs text-muted-foreground"><p className="mb-2 font-medium text-foreground">可用卷</p><div className="flex flex-wrap gap-1.5">{volumes.map((volume) => <Badge key={volume.name} variant="outline">{volume.name} · {volume.type}</Badge>)}</div></div> : <div className="rounded-md border border-warning/40 bg-warning/10 p-3 text-xs text-warning">当前 Pod 模板没有可挂载的卷，请先在 YAML 中定义 ConfigMap、Secret、PVC 或 EmptyDir 卷。</div>}
          {rows.length ? rows.map((row, index) => (
            <div key={index} className="space-y-3 rounded-lg border p-3">
              <div className="flex items-center justify-between gap-2"><span className="text-xs font-medium text-muted-foreground">挂载 {index + 1}</span><Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-destructive" onClick={() => setRows((current) => current.filter((_, i) => i !== index))} disabled={saving}><Trash2 className="h-3.5 w-3.5" />删除</Button></div>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="space-y-1.5"><Label>卷</Label><Select value={row.name} onValueChange={(value) => updateRow(index, { name: value })}><SelectTrigger aria-label={`挂载 ${index + 1} 卷`}><SelectValue placeholder="选择卷" /></SelectTrigger><SelectContent>{volumes.map((volume) => <SelectItem key={volume.name} value={volume.name}>{volume.name} · {volume.type}</SelectItem>)}</SelectContent></Select></div>
                <div className="space-y-1.5"><Label htmlFor={`mount-path-${index}`}>挂载路径</Label><Input id={`mount-path-${index}`} aria-label={`挂载 ${index + 1} 路径`} className="font-mono text-xs" value={row.mountPath} onChange={(event) => updateRow(index, { mountPath: event.target.value })} placeholder="/data" /></div>
                <div className="space-y-1.5"><Label htmlFor={`mount-subpath-${index}`}>子路径（可选）</Label><Input id={`mount-subpath-${index}`} aria-label={`挂载 ${index + 1} 子路径`} className="font-mono text-xs" value={row.subPath ?? ""} onChange={(event) => updateRow(index, { subPath: event.target.value })} placeholder="config/app.yaml" /></div>
                <label className="flex items-center gap-2 self-end pb-2 text-xs text-muted-foreground"><input type="checkbox" checked={row.readOnly} onChange={(event) => updateRow(index, { readOnly: event.target.checked })} />只读挂载</label>
              </div>
            </div>
          )) : <EmptyHint text="尚未配置存储挂载" />}
          <Button type="button" variant="outline" size="sm" onClick={() => setRows((current) => [...current, { name: volumes[0]?.name ?? "", mountPath: "", readOnly: false }])} disabled={saving || !volumes.length || rows.length >= 64}><Plus className="h-4 w-4" />添加挂载</Button>
          {!unique ? <p className="text-xs text-destructive">同一个卷和挂载路径都不能重复。</p> : null}
        </div>
        <DialogFooter><Button variant="outline" onClick={onClose} disabled={saving}>取消</Button><Button onClick={submit} disabled={saving || !valid || !unique}>{saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}保存配置</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ProbeEditorDialog({ slot, probe, onClose, onSave }: {
  slot: ProbeSlot;
  probe?: WorkloadProbe;
  onClose: () => void;
  onSave: (probe?: WorkloadProbe) => Promise<void>;
}) {
  const [type, setType] = useState<string>(probe?.type ?? "disabled");
  const [path, setPath] = useState(probe?.path ?? "/");
  const [host, setHost] = useState(probe?.host ?? "");
  const [scheme, setScheme] = useState<"HTTP" | "HTTPS">(probe?.scheme ?? "HTTP");
  const [port, setPort] = useState(probe?.port ?? "80");
  const [service, setService] = useState(probe?.service ?? "");
  const [command, setCommand] = useState((probe?.command ?? []).join("\n"));
  const [initialDelaySeconds, setInitialDelaySeconds] = useState(probe?.initialDelaySeconds ?? 0);
  const [periodSeconds, setPeriodSeconds] = useState(probe?.periodSeconds || 10);
  const [timeoutSeconds, setTimeoutSeconds] = useState(probe?.timeoutSeconds || 1);
  const [successThreshold, setSuccessThreshold] = useState(probe?.successThreshold || 1);
  const [failureThreshold, setFailureThreshold] = useState(probe?.failureThreshold || 3);
  const [saving, setSaving] = useState(false);
  const submit = async () => {
    setSaving(true);
    const next: WorkloadProbe | undefined = type === "disabled" ? undefined : {
      type: type as WorkloadProbe["type"],
      path: type === "httpGet" ? path : undefined,
      host: (type === "httpGet" || type === "tcpSocket") ? host : undefined,
      scheme: type === "httpGet" ? scheme : undefined,
      port: (type === "httpGet" || type === "tcpSocket" || type === "grpc") ? port : undefined,
      service: type === "grpc" ? service : undefined,
      command: type === "exec" ? command.split("\n").map((item) => item.trim()).filter(Boolean) : [],
      initialDelaySeconds,
      periodSeconds,
      timeoutSeconds,
      successThreshold,
      failureThreshold,
    };
    try { await onSave(next); onClose(); } catch { /* parent reports the API error */ } finally { setSaving(false); }
  };
  return (
    <Dialog open onOpenChange={(open) => { if (!open && !saving) onClose(); }}>
      <DialogContent className="max-h-[88vh] max-w-xl overflow-y-auto">
        <DialogHeader><DialogTitle>编辑{probeLabels[slot]}</DialogTitle><DialogDescription>未配置的探针不参与实例健康判定；修改后触发滚动发布。</DialogDescription></DialogHeader>
        <div className="space-y-4">
          <div className="space-y-1.5"><Label>检查类型</Label><Select value={type} onValueChange={setType}><SelectTrigger aria-label="检查类型"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="disabled">不启用</SelectItem><SelectItem value="httpGet">HTTP GET</SelectItem><SelectItem value="tcpSocket">TCP Socket</SelectItem><SelectItem value="exec">Exec 命令</SelectItem><SelectItem value="grpc">gRPC</SelectItem></SelectContent></Select></div>
          {type === "httpGet" ? <div className="grid gap-3 sm:grid-cols-2"><div className="space-y-1.5"><Label htmlFor="probe-path">请求路径</Label><Input id="probe-path" value={path} onChange={(event) => setPath(event.target.value)} placeholder="/healthz" /></div><div className="space-y-1.5"><Label htmlFor="probe-port">端口</Label><Input id="probe-port" value={port} onChange={(event) => setPort(event.target.value)} placeholder="8080" /></div><div className="space-y-1.5"><Label htmlFor="probe-host">Host（可选）</Label><Input id="probe-host" value={host} onChange={(event) => setHost(event.target.value)} /></div><div className="space-y-1.5"><Label>协议</Label><Select value={scheme} onValueChange={(value) => setScheme(value as "HTTP" | "HTTPS")}><SelectTrigger aria-label="HTTP 协议"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="HTTP">HTTP</SelectItem><SelectItem value="HTTPS">HTTPS</SelectItem></SelectContent></Select></div></div> : null}
          {type === "tcpSocket" ? <div className="grid gap-3 sm:grid-cols-2"><div className="space-y-1.5"><Label htmlFor="probe-port">端口</Label><Input id="probe-port" value={port} onChange={(event) => setPort(event.target.value)} placeholder="8080" /></div><div className="space-y-1.5"><Label htmlFor="probe-host">Host（可选）</Label><Input id="probe-host" value={host} onChange={(event) => setHost(event.target.value)} /></div></div> : null}
          {type === "exec" ? <div className="space-y-1.5"><Label htmlFor="probe-command">命令参数</Label><Textarea id="probe-command" className="min-h-24 font-mono text-xs" value={command} onChange={(event) => setCommand(event.target.value)} placeholder={"cat\n/tmp/healthy"} /><p className="text-xs text-muted-foreground">每行一个参数，第一行是命令。</p></div> : null}
          {type === "grpc" ? <div className="grid gap-3 sm:grid-cols-2"><div className="space-y-1.5"><Label htmlFor="probe-port">端口</Label><Input id="probe-port" value={port} onChange={(event) => setPort(event.target.value)} placeholder="50051" /></div><div className="space-y-1.5"><Label htmlFor="probe-service">服务名（可选）</Label><Input id="probe-service" value={service} onChange={(event) => setService(event.target.value)} /></div></div> : null}
          {type !== "disabled" ? <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3"><NumberField id="probe-initial-delay" label="初始延迟（秒）" value={initialDelaySeconds} onChange={setInitialDelaySeconds} min={0} /><NumberField id="probe-period" label="检查周期（秒）" value={periodSeconds} onChange={setPeriodSeconds} min={1} /><NumberField id="probe-timeout" label="超时（秒）" value={timeoutSeconds} onChange={setTimeoutSeconds} min={1} /><NumberField id="probe-success" label="成功阈值" value={successThreshold} onChange={setSuccessThreshold} min={1} /><NumberField id="probe-failure" label="失败阈值" value={failureThreshold} onChange={setFailureThreshold} min={1} /></div> : null}
        </div>
        <DialogFooter><Button variant="outline" onClick={onClose} disabled={saving}>取消</Button><Button onClick={submit} disabled={saving}>{saving ? <Loader2 className="h-4 w-4 animate-spin" /> : null}保存配置</Button></DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function NumberField({ id, label, value, onChange, min }: { id: string; label: string; value: number; onChange: (value: number) => void; min: number }) {
  return <div className="space-y-1.5"><Label htmlFor={id}>{label}</Label><Input id={id} type="number" min={min} value={value} onChange={(event) => onChange(Math.max(min, Number(event.target.value) || min))} /></div>;
}

function envRowToValue(row: EnvRow): WorkloadEnvVar {
  if (row.sourceKind === "literal") return { name: row.name.trim(), value: row.value };
  return {
    name: row.name.trim(),
    value: "",
    valueFrom: {
      kind: row.sourceKind,
      name: row.sourceName.trim(),
      ...(row.sourceKind === "configMapKeyRef" || row.sourceKind === "secretKeyRef" ? { key: row.sourceKey.trim(), optional: row.optional } : {}),
    },
  };
}

function envSourceLabel(kind: WorkloadEnvValueFrom["kind"]): string {
  switch (kind) {
    case "configMapKeyRef": return "ConfigMap";
    case "secretKeyRef": return "Secret";
    case "fieldRef": return "字段引用";
    case "resourceFieldRef": return "资源引用";
  }
}

function probeSummary(probe: WorkloadProbe): string {
  switch (probe.type) {
    case "httpGet": return `HTTP GET ${probe.scheme === "HTTPS" ? "https" : "http"}://${probe.host || "Pod"}${probe.path || "/"}:${probe.port}`;
    case "tcpSocket": return `TCP ${probe.host || "Pod"}:${probe.port}`;
    case "exec": return `Exec ${(probe.command ?? []).join(" ")}`;
    case "grpc": return `gRPC ${probe.service ? `${probe.service}:` : ""}${probe.port}`;
  }
}

function formatMilliCPU(value: number): string {
  if (!value) return "未设置";
  return value % 1000 === 0 ? `${value / 1000} 核` : `${value}m`;
}

function formatBytes(value: number): string {
  if (!value) return "未设置";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let amount = value;
  let unit = 0;
  while (amount >= 1024 && unit < units.length - 1) { amount /= 1024; unit += 1; }
  return `${Number.isInteger(amount) ? amount : amount.toFixed(1)} ${units[unit]}`;
}
