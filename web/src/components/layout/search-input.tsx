import { Search } from "lucide-react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

export function SearchInput({ value, onChange, placeholder = "按名称搜索", className }: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
}) {
  return (
    <div className={cn("relative w-full sm:w-64", className)}>
      <Search className="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
      <Input value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} className="pl-8" />
    </div>
  );
}

