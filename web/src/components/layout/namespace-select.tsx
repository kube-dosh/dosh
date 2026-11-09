import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const ALL = "__all__";

export function NamespaceSelect({ value, onChange, className, allowAll = true, ariaLabel = "命名空间筛选" }: { value: string; onChange: (value: string) => void; className?: string; allowAll?: boolean; ariaLabel?: string }) {
  const query = useQuery({ queryKey: ["namespaces", "selector"], queryFn: () => api.namespaces(), staleTime: 30_000 });
  return (
    <Select value={value || ALL} onValueChange={(next) => onChange(next === ALL ? "" : next)}>
      <SelectTrigger className={className ?? "w-48"} aria-label={ariaLabel} title={query.isError ? "命名空间列表加载失败" : undefined}>
        <SelectValue placeholder={query.isError ? "命名空间加载失败" : "全部命名空间"} />
      </SelectTrigger>
      <SelectContent>
        {query.isError ? <SelectItem value="__namespace_error__" disabled>命名空间列表加载失败</SelectItem> : null}
        {allowAll ? <SelectItem value={ALL}>全部命名空间</SelectItem> : null}
        {query.data?.items.map((item) => <SelectItem key={item.name} value={item.name}>{item.name}</SelectItem>)}
      </SelectContent>
    </Select>
  );
}

