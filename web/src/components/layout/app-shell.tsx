import { useState } from "react";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import {
  Activity, Bell, Boxes, ChevronDown, CircleDot, Database, FolderTree, HelpCircle,
  LayoutDashboard, LogOut, Menu, Moon, Network, ScrollText, Search, Server, Settings2, Sun, X,
  type LucideIcon,
} from "lucide-react";
import { api } from "@/api/client";
import type { Overview } from "@/api/types";
import { sessionQueryKey, useSessionQuery } from "@/features/auth/use-session";
import { useOverviewQuery } from "@/features/overview/use-overview-query";
import { GlobalSearch } from "@/components/layout/global-search";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { useTheme } from "@/lib/theme";

const nav: Array<{ href: string; label: string; icon: LucideIcon; adminOnly?: boolean; match: (path: string) => boolean }> = [
  { href: "/", label: "总览", icon: LayoutDashboard, match: (path: string) => path === "/" },
  { href: "/nodes", label: "节点管理", icon: Server, match: (path: string) => path.startsWith("/nodes") },
  { href: "/workloads", label: "工作负载", icon: Boxes, match: (path: string) => path.startsWith("/workloads") || path.startsWith("/pods") },
  { href: "/network", label: "服务与路由", icon: Network, match: (path: string) => path.startsWith("/network") },
  { href: "/configuration", label: "配置与密钥", icon: Settings2, match: (path: string) => path.startsWith("/configuration") },
  { href: "/storage", label: "存储", icon: Database, match: (path: string) => path.startsWith("/storage") },
  { href: "/namespaces", label: "命名空间", icon: FolderTree, match: (path: string) => path.startsWith("/namespaces") },
  { href: "/events", label: "事件", icon: Activity, match: (path: string) => path.startsWith("/events") },
  { href: "/audit", label: "审计日志", icon: ScrollText, adminOnly: true, match: (path: string) => path.startsWith("/audit") },
];

type ClusterPresentation = {
  name: string;
  label: string;
  connectionLabel: string;
  detail: string;
  dotClass: string;
};

function clusterPresentation(data: Overview | undefined, isError: boolean): ClusterPresentation {
  if (isError) {
    return {
      name: "集群状态不可用",
      label: "连接失败",
      connectionLabel: "集群状态不可用",
      detail: "无法读取集群信息",
      dotClass: "bg-destructive",
    };
  }
  if (!data?.cluster) {
    return {
      name: "读取集群信息",
      label: "连接中",
      connectionLabel: "正在读取集群状态",
      detail: "正在连接 Kubernetes API",
      dotClass: "animate-pulse bg-muted-foreground",
    };
  }
  const cluster = data.cluster;
  const name = cluster.name || "未知集群";
  const detail = `Kubernetes ${cluster.version || "版本未知"} · ${cluster.cni || "CNI 未知"}`;
  switch (cluster.status.toLowerCase()) {
    case "healthy":
      return { name, label: "正常", connectionLabel: "集群连接正常", detail, dotClass: "bg-success" };
    case "degraded":
      return { name, label: "部分异常", connectionLabel: "集群部分异常", detail, dotClass: "bg-warning" };
    case "unavailable":
      return { name, label: "不可用", connectionLabel: "集群状态不可用", detail, dotClass: "bg-destructive" };
    default:
      return { name, label: "状态未知", connectionLabel: "集群状态未知", detail, dotClass: "bg-muted-foreground" };
  }
}

export function AppShell() {
  const session = useSessionQuery();
  const overview = useOverviewQuery();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const [mobileOpen, setMobileOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const cluster = clusterPresentation(overview.data, overview.isError);
  const { theme, toggleTheme } = useTheme();

  const localLogout = async () => {
    try { await api.logout(); } finally {
      queryClient.removeQueries({ queryKey: sessionQueryKey });
      await navigate({ to: "/login", replace: true });
    }
  };

  return (
    <div className="min-h-screen w-full min-w-0 max-w-full overflow-x-clip bg-background">
      <header className="sticky top-0 z-40 flex h-14 w-full min-w-0 max-w-full items-center overflow-x-clip border-b bg-card/95 px-4 backdrop-blur md:px-5">
        <Button variant="ghost" size="icon" className="mr-2 shrink-0 md:hidden" onClick={() => setMobileOpen(true)} aria-label="打开导航" aria-expanded={mobileOpen} aria-controls="app-navigation"><Menu className="h-5 w-5" /></Button>
        <Link to="/" className="flex shrink-0 items-center gap-2.5">
          <div className="grid h-8 w-8 place-items-center rounded-lg bg-primary text-sm font-bold text-primary-foreground shadow-sm">D</div>
          <div className="hidden sm:block">
            <p className="text-sm font-semibold leading-4">Dosh Console</p>
            <p className="text-[10px] uppercase tracking-[0.16em] text-muted-foreground">Kubernetes Operations</p>
          </div>
        </Link>
        <div className="mx-5 hidden h-6 w-px bg-border lg:block" />
        <div className="hidden items-center gap-2 text-sm lg:flex" aria-label="当前集群">
          <span className={cn("h-2 w-2 rounded-full", cluster.dotClass)} aria-hidden="true" />
          <span className="max-w-44 truncate font-medium" title={cluster.name}>{cluster.name}</span>
          <span className="text-muted-foreground">/ {cluster.label}</span>
        </div>
        <div className="ml-auto flex min-w-0 shrink-0 items-center gap-1 sm:gap-1.5">
          <button type="button" onClick={() => setSearchOpen(true)} className="hidden h-9 w-64 items-center gap-2 rounded-md border bg-muted/35 px-3 text-sm text-muted-foreground transition-colors hover:bg-muted md:flex">
            <Search className="h-3.5 w-3.5" /><span>搜索资源</span><kbd className="ml-auto rounded border bg-card px-1.5 py-0.5 text-[10px]">⌘ K</kbd>
          </button>
          <Button variant="ghost" size="icon" className="md:hidden" onClick={() => setSearchOpen(true)} aria-label="搜索"><Search className="h-4 w-4" /></Button>
          <Button variant="ghost" size="icon" asChild aria-label="事件" className="hidden sm:inline-flex"><Link to="/events"><Bell className="h-4 w-4" /></Link></Button>
          <Button variant="ghost" size="icon" onClick={toggleTheme} aria-label={theme === "dark" ? "切换为浅色主题" : "切换为深色主题"} aria-pressed={theme === "dark"} className="hidden sm:inline-flex">
            {theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </Button>
          <Button variant="ghost" size="icon" onClick={() => setHelpOpen(true)} aria-label="帮助" className="hidden sm:inline-flex"><HelpCircle className="h-4 w-4" /></Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" className="h-9 gap-2 px-1.5 sm:px-2">
                <span className="grid h-6 w-6 place-items-center rounded-full bg-primary/10 text-xs font-semibold text-primary">{(session.data?.user.username || "A").slice(0, 1).toUpperCase()}</span>
                <span className="hidden max-w-28 truncate text-sm sm:block">{session.data?.user.username || "管理员"}</span>
                <ChevronDown className="hidden h-3.5 w-3.5 text-muted-foreground sm:block" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56 max-w-[calc(100vw-1rem)]">
              <DropdownMenuLabel><span className="block text-foreground">{session.data?.user.username || "管理员"}</span><span className="font-normal">{session.data?.authMode === "oidc" ? "企业身份" : session.data?.authMode === "token" ? "Kubernetes Token" : "本地管理员"}</span></DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem className="sm:hidden" asChild><Link to="/events"><Bell className="h-4 w-4" />集群事件</Link></DropdownMenuItem>
              <DropdownMenuItem className="sm:hidden" onClick={toggleTheme}>{theme === "dark" ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}{theme === "dark" ? "切换为浅色主题" : "切换为深色主题"}</DropdownMenuItem>
              <DropdownMenuItem className="sm:hidden" onClick={() => setHelpOpen(true)}><HelpCircle className="h-4 w-4" />帮助</DropdownMenuItem>
              <DropdownMenuSeparator className="sm:hidden" />
              {session.data?.authMode === "oidc" ? (
                <DropdownMenuItem asChild><a href="/api/v1/auth/oidc/logout"><LogOut className="h-4 w-4" />退出登录</a></DropdownMenuItem>
              ) : (
                <DropdownMenuItem onClick={() => void localLogout()}><LogOut className="h-4 w-4" />退出登录</DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>

      <div className="flex min-h-[calc(100vh-3.5rem)] w-full min-w-0 max-w-full">
        <aside id="app-navigation" aria-label="主导航" className={cn("fixed inset-y-0 left-0 z-50 w-60 border-r bg-card transition-transform md:sticky md:top-14 md:z-30 md:h-[calc(100vh-3.5rem)] md:translate-x-0", mobileOpen ? "translate-x-0" : "-translate-x-full")}>
          <div className="flex h-14 items-center justify-between border-b px-4 md:hidden">
            <span className="font-semibold">导航</span>
            <Button variant="ghost" size="icon" onClick={() => setMobileOpen(false)} aria-label="关闭导航"><X className="h-4 w-4" /></Button>
          </div>
          <nav className="space-y-1 p-3">
            <p className="px-3 pb-2 pt-1 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">集群管理</p>
            {nav.filter((item) => !item.adminOnly || session.data?.user.admin).map((item) => {
              const active = item.match(pathname);
              const Icon = item.icon;
              return (
                <Link key={item.href} to={item.href} onClick={() => setMobileOpen(false)} className={cn("flex h-9 items-center gap-3 rounded-md px-3 text-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", active ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-muted hover:text-foreground")}>
                  <Icon className="h-4 w-4" />{item.label}
                </Link>
              );
            })}
            <div className="pt-3">
              <p className="px-3 pb-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">快捷入口</p>
              <Link to="/pods" onClick={() => setMobileOpen(false)} className={cn("flex h-9 items-center gap-3 rounded-md px-3 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring", pathname.startsWith("/pods") && "bg-primary/10 font-medium text-primary")}><CircleDot className="h-4 w-4" />Pod 实例</Link>
            </div>
          </nav>
          <div className="absolute bottom-0 left-0 right-0 border-t p-3">
            <div className="rounded-md bg-muted/55 p-3 text-xs" role="status" aria-live="polite" aria-label="集群连接状态">
              <div className="flex items-center gap-2 font-medium">
                <span className={cn("h-2 w-2 rounded-full", cluster.dotClass)} aria-hidden="true" />
                {cluster.connectionLabel}
              </div>
              <p className="mt-1 text-muted-foreground">{cluster.detail}</p>
              {overview.isError ? <Button variant="ghost" size="sm" className="mt-2 h-7 px-2 text-xs" onClick={() => void overview.refetch()}>重试</Button> : null}
            </div>
          </div>
        </aside>
        {mobileOpen ? <button type="button" aria-label="关闭导航菜单" className="fixed inset-0 z-40 bg-slate-950/30 md:hidden" onClick={() => setMobileOpen(false)} /> : null}
        <main className="w-0 min-w-0 max-w-full flex-1 overflow-x-clip p-4 md:p-6"><div className="mx-auto min-w-0 max-w-[1600px] animate-fade-in"><Outlet /></div></main>
      </div>

      <GlobalSearch open={searchOpen} onOpenChange={setSearchOpen} />
      <Dialog open={helpOpen} onOpenChange={setHelpOpen}>
        <DialogContent>
          <DialogHeader><DialogTitle>关于 Dosh Console</DialogTitle><DialogDescription>面向 {overview.data?.cluster.name || "当前 Kubernetes 集群"} 的 Kubernetes 运维控制台。</DialogDescription></DialogHeader>
          <div className="space-y-2 text-sm text-muted-foreground">
            <p>支持集群总览、节点、工作负载、Pod、日志、事件、服务、配置、Secret 和存储资源查看。</p>
            <p>所有写操作均经过会话、CSRF、权限和审计检查。默认只读模式可在 Helm values 中切换。</p>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

