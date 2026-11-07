import type { ReactNode } from "react";
import { MoreHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

export interface ResourceAction {
  label: string;
  icon?: ReactNode;
  onSelect: () => void;
  destructive?: boolean;
  disabled?: boolean;
  separatorBefore?: boolean;
}

export function ResourceActionsMenu({ resource, actions }: { resource: string; actions: ResourceAction[] }) {
  if (!actions.length) return <span className="text-xs text-muted-foreground">—</span>;
  return (
    <div data-row-action onClick={(event) => event.stopPropagation()} onKeyDown={(event) => event.stopPropagation()}>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label={`操作 ${resource}`}>
            <MoreHorizontal className="h-4 w-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="min-w-40">
          {actions.map((action, index) => (
            <div key={action.label}>
              {action.separatorBefore && index > 0 ? <DropdownMenuSeparator /> : null}
              <DropdownMenuItem
                disabled={action.disabled}
                onSelect={action.onSelect}
                className={cn(action.destructive && "text-destructive-strong focus:text-destructive-strong")}
              >
                {action.icon}
                {action.label}
              </DropdownMenuItem>
            </div>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}
