import { Link } from "@tanstack/react-router";
import { cn } from "@/lib/utils";

type HelmSection = "charts" | "releases" | "repositories";

const items: Array<{ id: HelmSection; label: string; to: string }> = [
  { id: "charts", label: "Chart 目录", to: "/helm/charts" },
  { id: "releases", label: "Releases", to: "/helm/releases" },
  { id: "repositories", label: "仓库管理", to: "/helm/repositories" },
];

export function HelmSectionNav({ active }: { active: HelmSection }) {
  return (
    <nav aria-label="Helm 功能" className="mb-4 grid w-full grid-cols-3 gap-1 rounded-lg border bg-muted/50 p-1 text-muted-foreground sm:inline-flex sm:w-auto">
      {items.map((item) => {
        const current = item.id === active;
        return (
          <Link
            key={item.id}
            to={item.to}
            aria-current={current ? "page" : undefined}
            className={cn(
              "inline-flex min-w-0 items-center justify-center rounded-md px-2 py-1.5 text-center text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring sm:px-4 sm:text-sm",
              current ? "bg-card text-foreground shadow-sm" : "hover:bg-background/70 hover:text-foreground",
            )}
          >
            <span className="truncate">{item.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
