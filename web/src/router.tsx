import { createRootRoute, createRoute, createRouter, Outlet } from "@tanstack/react-router";
import { AppShell } from "@/components/layout/app-shell";
import { ErrorState } from "@/components/feedback/states";
import { LoginPage } from "@/routes/login-page";
import { OverviewPage } from "@/features/overview/overview-page";
import { NodesPage } from "@/features/nodes/nodes-page";
import { NodeDetailPage } from "@/features/nodes/node-detail-page";
import { WorkloadsPage } from "@/features/workloads/workloads-page";
import { WorkloadDetailPage } from "@/features/workloads/workload-detail-page";
import { PodsPage } from "@/features/workloads/pods-page";
import { PodDetailPage } from "@/features/workloads/pod-detail-page";
import { NetworkPage } from "@/features/network/network-page";
import { ServiceDetailPage } from "@/features/network/service-detail-page";
import { IngressDetailPage } from "@/features/network/ingress-detail-page";
import { ConfigurationPage } from "@/features/configuration/configuration-page";
import { StoragePage } from "@/features/storage/storage-page";
import { PersistentVolumeClaimDetailPage, PersistentVolumeDetailPage, StorageClassDetailPage } from "@/features/storage/storage-detail-page";
import { NamespacesPage } from "@/features/namespaces/namespaces-page";
import { NamespaceDetailPage } from "@/features/namespaces/namespace-detail-page";
import { EventsPage } from "@/features/events/events-page";
import { AuditPage } from "@/features/audit/audit-page";
import { Button } from "@/components/ui/button";
import { Link } from "@tanstack/react-router";
import { parseListSearch } from "@/lib/list-search";
import { useUnauthorizedRedirect } from "@/features/auth/use-session";

function RootRouteComponent() {
  useUnauthorizedRedirect();
  return <Outlet />;
}

const rootRoute = createRootRoute({
  component: RootRouteComponent,
  notFoundComponent: () => (
    <div className="grid min-h-screen place-items-center bg-background p-6">
      <div className="w-full max-w-md rounded-xl border bg-card p-8 text-center shadow-panel">
        <p className="text-5xl font-semibold tracking-tight text-primary">404</p>
        <h1 className="mt-4 text-lg font-semibold">页面不存在</h1>
        <p className="mt-2 text-sm text-muted-foreground">请求的页面可能已移动，或地址输入有误。</p>
        <Button asChild className="mt-6"><Link to="/">返回总览</Link></Button>
      </div>
    </div>
  ),
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: LoginPage,
});

const appRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "app",
  component: AppShell,
  errorComponent: ({ error }) => <ErrorState error={error} className="m-6" />,
});

const indexRoute = createRoute({ getParentRoute: () => appRoute, path: "/", component: OverviewPage });
const nodesRoute = createRoute({ getParentRoute: () => appRoute, path: "/nodes", component: NodesPage, validateSearch: parseListSearch });
const nodeDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/nodes/$name", component: NodeDetailPage, validateSearch: parseListSearch });
const workloadsRoute = createRoute({ getParentRoute: () => appRoute, path: "/workloads", component: WorkloadsPage, validateSearch: parseListSearch });
const workloadDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/workloads/$kind/$namespace/$name", component: WorkloadDetailPage, validateSearch: parseListSearch });
const podsRoute = createRoute({ getParentRoute: () => appRoute, path: "/pods", component: PodsPage, validateSearch: parseListSearch });
const podDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/pods/$namespace/$name", component: PodDetailPage, validateSearch: parseListSearch });
const networkRoute = createRoute({ getParentRoute: () => appRoute, path: "/network", component: NetworkPage, validateSearch: parseListSearch });
const serviceDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/network/services/$namespace/$name", component: ServiceDetailPage, validateSearch: parseListSearch });
const ingressDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/network/ingresses/$namespace/$name", component: IngressDetailPage, validateSearch: parseListSearch });
const configurationRoute = createRoute({ getParentRoute: () => appRoute, path: "/configuration", component: ConfigurationPage, validateSearch: parseListSearch });
const storageRoute = createRoute({ getParentRoute: () => appRoute, path: "/storage", component: StoragePage, validateSearch: parseListSearch });
const storageClassDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/storage/classes/$name", component: StorageClassDetailPage, validateSearch: parseListSearch });
const persistentVolumeDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/storage/volumes/$name", component: PersistentVolumeDetailPage, validateSearch: parseListSearch });
const persistentVolumeClaimDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/storage/claims/$namespace/$name", component: PersistentVolumeClaimDetailPage, validateSearch: parseListSearch });
const namespacesRoute = createRoute({ getParentRoute: () => appRoute, path: "/namespaces", component: NamespacesPage, validateSearch: parseListSearch });
const namespaceDetailRoute = createRoute({ getParentRoute: () => appRoute, path: "/namespaces/$name", component: NamespaceDetailPage, validateSearch: parseListSearch });
const eventsRoute = createRoute({ getParentRoute: () => appRoute, path: "/events", component: EventsPage, validateSearch: parseListSearch });
const auditRoute = createRoute({ getParentRoute: () => appRoute, path: "/audit", component: AuditPage, validateSearch: parseListSearch });

export const routeTree = rootRoute.addChildren([
  loginRoute,
  appRoute.addChildren([
    indexRoute,
    nodesRoute,
    nodeDetailRoute,
    workloadsRoute,
    workloadDetailRoute,
    podsRoute,
    podDetailRoute,
    networkRoute,
    serviceDetailRoute,
    ingressDetailRoute,
    configurationRoute,
    storageRoute,
    storageClassDetailRoute,
    persistentVolumeDetailRoute,
    persistentVolumeClaimDetailRoute,
    namespacesRoute,
  namespaceDetailRoute,
    eventsRoute,
    auditRoute,
  ]),
]);

export const router = createRouter({ routeTree, defaultPreload: "intent" });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
