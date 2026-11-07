import type { ReactNode } from "react";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";

export function ActionTooltip({ content, children }: { content?: string; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="inline-flex" tabIndex={content ? 0 : undefined}>{children}</span>
      </TooltipTrigger>
      {content ? <TooltipContent>{content}</TooltipContent> : null}
    </Tooltip>
  );
}
