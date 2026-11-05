import { useQuery } from "@tanstack/react-query";
import { api } from "@/api/client";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

const ALL = "__all__";

export function NamespaceSelect({ value, onChange, className, allowAll = true }: { value: string; onChange: (value: string) => void; className?: string; allowAll?: boolean }) {
  const query = useQuery({ queryKey: ["namespaces", "selector"], queryFn: () => api.namespaces(), staleTime: 30_000 });
  return (
    <Select value={value || ALL} onValueChange={(next) => onChange(next === ALL ? "" : next)}>
      <SelectTrigger className={className ?? "w-48"} aria-label="命名空间筛选">
        <SelectValue placeholder="全部命名空间" />
      </SelectTrigger>
      <SelectContent>
        {allowAll ? <SelectItem value={ALL}>全部命名空间</SelectItem> : null}
        {query.data?.items.map((item) => <SelectItem key={item.name} value={item.name}>{item.name}</SelectItem>)}
      </SelectContent>
    </Select>
  );
}

