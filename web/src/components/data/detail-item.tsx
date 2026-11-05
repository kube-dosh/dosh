import type { ReactNode } from "react";

export function DetailItem({ label, value, mono, children }: { label: string; value?: ReactNode; mono?: boolean; children?: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? "mt-1 break-all font-mono text-xs font-medium" : "mt-1 break-words text-sm font-medium"}>{value ?? children ?? "—"}</dd>
    </div>
  );
}

