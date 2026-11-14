import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { ArrowLeft, FileCode2, Pencil, Trash2 } from "lucide-react";
import { api } from "@/api/client";
import type { ResourceDescriptor } from "@/api/types";
import { DetailItem } from "@/components/data/detail-item";
import { EmptyState, ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { MetadataCard } from "@/components/resources/metadata-card";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { ResourceEvents } from "@/features/events/resource-events";
import { GenericYamlPanel } from "@/features/resources/generic-yaml-panel";
import { GenericResourceEditorDialog } from "@/features/resources/generic-resource-editor-dialog";
import { findGovernanceDescriptor, governanceRelationships } from "@/features/resources/governance-resources";
import { DeleteResourceDialog } from "@/components/resources/delete-resource-dialog";
import { formatDate } from "@/lib/utils";

const detailTabs = ["overview", "events", "yaml"] as const;
type DetailTab = (typeof detailTabs)[number];

function isDetailTab(value: string | undefined): value is DetailTab {
  return detailTabs.includes(value as DetailTab);
}

function findDescriptor(descriptors: ResourceDescriptor[], group: string, version: string, resource: string) {
  return descriptors.find((item) => item.group === group && item.version === version && item.resource === resource);
}

export function ResourceDetailPage() {
  const params = useParams({ strict: false }) as { group: string; version: string; resource: string; name: string };
  const search = useSearch({ from: "/app/resources/$group/$version/$resource/$name" });
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const apiGroup = params.group === "_core" ? "" : params.group;
  const namespace = search.namespace ?? "";
  const tab = isDetailTab(search.tab) ? search.tab : "overview";
  const discovery = useQuery({ queryKey: ["resource-discovery", "all"], queryFn: () => api.resourceDiscovery(), staleTime: 60_000 });
  const descriptor = findDescriptor(discovery.data?.items ?? [], apiGroup, params.version, params.resource);
  const missingNamespace = Boolean(descriptor?.namespaced && !namespace);
  const capabilities = useQuery({
    queryKey: ["generic-resource-capabilities", apiGroup, params.version, params.resource, namespace],
    queryFn: () => api.genericResourceCapabilities({ group: apiGroup, version: params.version, resource: params.resource }, namespace || undefined),
    enabled: Boolean(descriptor) && !missingNamespace,
    staleTime: 30_000,
  });
  const query = useQuery({
    queryKey: ["generic-resource", apiGroup, params.version, params.resource, namespace, params.name],
    queryFn: () => api.resource({ group: apiGroup, version: params.version, resource: params.resource }, params.name, namespace),
    enabled: Boolean(descriptor) && !missingNamespace,
  });
  const changeTab = (value: string) => {
    if (!isDetailTab(value)) return;
    void navigate({
      to: "/resources/$group/$version/$resource/$name",
      params,
      search: { ...search, tab: value === "overview" ? undefined : value },
    });
  };
  const listLink = (
    <Button asChild variant="outline" size="sm">
      <Link to="/resources/$group/$version/$resource" params={{ group: params.group, version: params.version, resource: params.resource }} search={{ namespace: namespace || undefined }}>
        <ArrowLeft className="h-4 w-4" />返回列表
      </Link>
    </Button>
  );

  if (discovery.isError) return <ErrorState error={discovery.error} onRetry={() => void discovery.refetch()} />;
  if (!discovery.isLoading && !descriptor) {
    return <EmptyState title="资源类型不存在" description="该 API Group、版本和资源名不在当前 Discovery 结果中。" action={<Button asChild variant="outline"><Link to="/resources">返回资源浏览器</Link></Button>} />;
  }
  if (missingNamespace) {
    return <EmptyState title="缺少命名空间" description={`${descriptor?.kind || params.resource} 是 Namespaced 资源，请在列表页选择或携带 namespace 参数后重试。`} action={listLink} />;
  }
  if (query.isLoading || discovery.isLoading) return <LoadingState />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const detail = query.data;
  if (!detail) return null;
  const governance = descriptor ? findGovernanceDescriptor(descriptor) : undefined;
  const relationships = governanceRelationships(params.resource, detail.object);
  const canEdit = Boolean(capabilities.data?.update && !detail.redacted);
  const canDelete = Boolean(capabilities.data?.delete);
  return (
    <>
      <PageHeader
        title={detail.name}
        breadcrumb={(
          <>
            <Link to="/resources" className="hover:text-foreground hover:underline">资源浏览器</Link>
            {" / "}
            <Link to="/resources/$group/$version/$resource" params={{ group: params.group, version: params.version, resource: params.resource }} search={{ namespace: namespace || undefined }} className="hover:text-foreground hover:underline">
              {descriptor?.kind || params.resource}
            </Link>
            {` / ${detail.name}`}
          </>
        )}
        description={`${detail.apiVersion} · ${detail.kind}${detail.namespace ? ` · ${detail.namespace}` : ""}`}
        actions={<>{canEdit ? <Button variant="outline" size="sm" onClick={() => setEditOpen(true)}><Pencil className="h-4 w-4" />编辑 YAML</Button> : null}{canDelete ? <Button variant="destructive" size="sm" onClick={() => setDeleteOpen(true)}><Trash2 className="h-4 w-4" />删除</Button> : null}{listLink}</>}
      />
      {detail.warnings?.length ? (
        <div className="mb-3 rounded-md border border-warning/35 bg-warning/10 px-3 py-2 text-xs text-warning">{detail.warnings.join("；")}</div>
      ) : null}
      <Tabs value={tab} onValueChange={changeTab}>
        <TabsList>
          <TabsTrigger value="overview">概览</TabsTrigger>
          <TabsTrigger value="events">事件</TabsTrigger>
          <TabsTrigger value="yaml"><FileCode2 className="h-3.5 w-3.5" />YAML</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          <div className="grid gap-4 xl:grid-cols-2">
            <Card>
              <CardHeader><CardTitle>资源信息</CardTitle></CardHeader>
              <CardContent>
                <dl className="grid gap-5 sm:grid-cols-2">
                  <DetailItem label="名称" value={detail.name} mono />
                  <DetailItem label="Kind" value={detail.kind} mono />
                  <DetailItem label="API Version" value={detail.apiVersion} mono />
                  <DetailItem label="命名空间" value={detail.namespace || "—"} />
                  <DetailItem label="UID" value={detail.uid || "—"} mono />
                  <DetailItem label="Resource Version" value={detail.resourceVersion || "—"} mono />
                  <DetailItem label="创建时间" value={formatDate(detail.createdAt)} />
                  <DetailItem label="敏感性" value={detail.redacted ? "数据已遮蔽" : "无额外遮蔽"} />
                </dl>
              </CardContent>
            </Card>
            <Card>
              <CardHeader><CardTitle>对象字段</CardTitle></CardHeader>
              <CardContent>
                <div className="flex flex-wrap gap-1.5">
                  {Object.keys(detail.object).sort().map((key) => <span key={key} className="rounded border bg-muted/30 px-2 py-1 font-mono text-[11px]">{key}</span>)}
                </div>
                <p className="mt-3 text-xs leading-5 text-muted-foreground">通用视图只保证标准资源元数据；不同 CRD 的业务字段请结合 YAML、事件和对应 Operator 文档排查。</p>
              </CardContent>
            </Card>
          </div>
          {governance || relationships.length ? (
            <Card className="mt-4">
              <CardHeader><CardTitle>治理关系</CardTitle></CardHeader>
              <CardContent className="space-y-3">
                {governance ? <p className="text-xs leading-5 text-muted-foreground">{governance.description}</p> : null}
                {relationships.length ? (
                  <dl className="grid gap-4 sm:grid-cols-2">
                    {relationships.map((item) => <DetailItem key={item.label} label={item.label} value={item.value} />)}
                  </dl>
                ) : null}
              </CardContent>
            </Card>
          ) : null}
          <div className="mt-4"><MetadataCard labels={detail.labels} annotations={detail.annotations} /></div>
        </TabsContent>
        <TabsContent value="events"><ResourceEvents namespace={namespace} objectKind={detail.kind} objectName={detail.name} /></TabsContent>
        <TabsContent value="yaml"><GenericYamlPanel yaml={detail.yaml} /></TabsContent>
      </Tabs>
      {descriptor ? (
        <GenericResourceEditorDialog
          open={editOpen}
          onOpenChange={setEditOpen}
          descriptor={descriptor}
          reference={{ group: apiGroup, version: params.version, resource: params.resource }}
          namespace={namespace}
          mode="update"
          detail={detail}
          onSaved={async () => {
            await queryClient.invalidateQueries({ queryKey: ["generic-resource"] });
            await queryClient.invalidateQueries({ queryKey: ["generic-resources"] });
            await queryClient.invalidateQueries({ queryKey: ["resource-discovery"] });
            await queryClient.invalidateQueries({ queryKey: ["generic-resource-capabilities"] });
          }}
        />
      ) : null}
      <DeleteResourceDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        resourceKind={detail.kind}
        name={detail.name}
        description="通用资源删除会立即提交到 Kubernetes API，并记录审计事件。"
        onConfirm={async () => {
          await api.deleteGenericResource({ group: apiGroup, version: params.version, resource: params.resource }, detail.name, namespace, detail.resourceVersion, "Background");
          await queryClient.invalidateQueries({ queryKey: ["generic-resources"] });
          await navigate({ to: "/resources/$group/$version/$resource", params: { group: params.group, version: params.version, resource: params.resource }, search: { namespace: namespace || undefined } });
        }}
      />
    </>
  );
}
