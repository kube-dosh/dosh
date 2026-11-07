import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { createColumnHelper } from "@tanstack/react-table";
import { CloudCog, Network, RefreshCw, Server, TriangleAlert } from "lucide-react";
import { api } from "@/api/client";
import type { ClusterNodeCIDR } from "@/api/types";
import { DataTable } from "@/components/data/data-table";
import { DetailItem } from "@/components/data/detail-item";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { PageHeader } from "@/components/layout/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

const nodeColumn = createColumnHelper<ClusterNodeCIDR>();

function ConfigValue({ value }: { value?: string }) {
  return value ? <span className="break-all font-mono text-xs">{value}</span> : <span className="text-muted-foreground">未检测到</span>;
}

export function ClusterConfigurationPage() {
  const query = useQuery({
    queryKey: ["cluster-configuration"],
    queryFn: api.clusterConfiguration,
    staleTime: 30_000,
  });
  if (query.isLoading) return <LoadingState label="正在加载集群配置" />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  const data = query.data;
  if (!data) return null;
  const warnings = data.warnings ?? [];

  return (
    <>
      <PageHeader
        title="集群配置"
        breadcrumb="总览 / 集群配置"
        description="查看 Kubernetes 集群身份、控制面地址和网络地址规划。配置仅作为运维参考，修改仍需通过集群管理平台或 YAML。"
        actions={<Button variant="outline" onClick={() => void query.refetch()}><RefreshCw className="h-4 w-4" />刷新</Button>}
      />
      {warnings.length ? (
        <div className="mb-4 flex items-start gap-2 rounded-lg border border-warning/35 bg-warning/10 px-4 py-3 text-sm text-warning">
          <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <div><p className="font-medium">部分配置无法读取</p><ul className="mt-1 list-disc space-y-0.5 pl-4 text-xs">{warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul></div>
        </div>
      ) : null}

      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><CloudCog className="h-4 w-4 text-primary" />基本信息</CardTitle>
            <CardDescription>集群身份、版本和容器网络实现</CardDescription>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-5 sm:grid-cols-2">
              <DetailItem label="集群名称" value={data.name} />
              <DetailItem label="Kubernetes 版本" value={data.version || "未检测到"} mono />
              <DetailItem label="运行平台" value={data.platform || "未检测到"} mono />
              <DetailItem label="容器网络" value={data.cni ? <Badge variant="secondary">{data.cni}</Badge> : "未检测到"} />
              <DetailItem label="配置来源" value={data.source ? <Badge variant="outline">{data.source}</Badge> : <span className="text-muted-foreground">未检测到（可能不是 kubeadm 集群）</span>} />
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2"><Network className="h-4 w-4 text-primary" />网络配置</CardTitle>
            <CardDescription>控制面、Pod、Service 与集群 DNS 地址规划</CardDescription>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-5 sm:grid-cols-2">
              <DetailItem label="控制面 Endpoint" value={<ConfigValue value={data.controlPlaneEndpoint} />} />
              <DetailItem label="集群 DNS 域名" value={<ConfigValue value={data.dnsDomain} />} />
              <DetailItem label="Pod 网段" value={<ConfigValue value={data.podCIDR} />} />
              <DetailItem label="Service 网段" value={<ConfigValue value={data.serviceCIDR} />} />
              <DetailItem label="Kubernetes Service IP" value={<ConfigValue value={data.kubernetesServiceIP} />} />
              <DetailItem label="集群 DNS Service IP" value={<ConfigValue value={data.dnsServiceIP} />} />
            </dl>
          </CardContent>
        </Card>
      </div>

      <Card className="mt-4">
        <CardHeader>
          <CardTitle className="flex items-center gap-2"><Server className="h-4 w-4 text-primary" />节点 Pod 网段</CardTitle>
          <CardDescription>{data.nodeCIDRs.length} 个节点的 Pod CIDR 分配；点击节点名称可进入节点详情。</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          <DataTable
            columns={[
              nodeColumn.accessor("name", { header: "节点", cell: (info) => <Link to="/nodes/$name" params={{ name: info.getValue() }} search={{}} className="font-medium text-primary hover:underline">{info.getValue()}</Link> }),
              nodeColumn.accessor("internalIP", { header: "内网 IP", cell: (info) => <span className="font-mono text-xs">{info.getValue() || "—"}</span> }),
              nodeColumn.accessor("podCIDRs", { header: "Pod CIDR", enableSorting: false, cell: (info) => <div className="flex flex-wrap gap-1.5">{info.getValue().length ? info.getValue().map((cidr: string) => <Badge key={cidr} variant="outline" className="font-mono">{cidr}</Badge>) : <span className="text-muted-foreground">未分配</span>}</div> }),
            ]}
            data={data.nodeCIDRs}
            empty="当前没有节点 Pod 网段信息"
            pageSize={20}
            stickyFirstColumn={false}
          />
        </CardContent>
      </Card>
    </>
  );
}
