import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export function MetadataList({ title, data }: { title: string; data?: Record<string, string> }) {
  const entries = Object.entries(data || {});
  return (
    <div>
      <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</h4>
      <div className="max-h-64 overflow-auto rounded-md border scrollbar-thin">
        {entries.length ? entries.map(([key, value]) => (
          <div key={key} className="grid min-w-0 gap-1 border-b px-3 py-2 text-xs last:border-0 sm:grid-cols-[minmax(120px,0.4fr)_1fr] sm:gap-3">
            <span className="min-w-0 break-all font-mono text-muted-foreground">{key}</span>
            <span className="min-w-0 break-all font-mono">{value}</span>
          </div>
        )) : <p className="p-3 text-sm text-muted-foreground">无</p>}
      </div>
    </div>
  );
}

export function MetadataCard({ labels, annotations }: { labels?: Record<string, string>; annotations?: Record<string, string> }) {
  return (
    <Card>
      <CardHeader><CardTitle>标签与注解</CardTitle></CardHeader>
      <CardContent className="grid gap-5 lg:grid-cols-2">
        <MetadataList title="Labels" data={labels} />
        <MetadataList title="Annotations" data={annotations} />
      </CardContent>
    </Card>
  );
}
