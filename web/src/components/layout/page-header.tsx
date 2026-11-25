import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export function PageHeader({ title, description, breadcrumb, actions, className }: {
  title: ReactNode;
  description?: ReactNode;
  breadcrumb?: ReactNode;
  actions?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("mb-5 flex flex-col gap-3", className)}>
      {breadcrumb ? <div className="text-xs text-muted-foreground">{breadcrumb}</div> : null}
      <div className="flex min-w-0 flex-col justify-between gap-3 md:flex-row md:items-start">
        <div className="min-w-0 md:min-w-56 md:flex-1">
          <h1 className="truncate text-xl font-semibold tracking-tight text-foreground">{title}</h1>
          {description ? <div className="mt-1 text-sm text-muted-foreground">{description}</div> : null}
        </div>
        {actions ? <div className="flex min-w-0 max-w-full flex-wrap items-center gap-2 md:max-w-[70%] md:justify-end">{actions}</div> : null}
      </div>
    </div>
  );
}

