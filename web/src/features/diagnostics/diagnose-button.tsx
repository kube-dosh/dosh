import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Stethoscope } from "lucide-react";
import { api } from "@/api/client";
import { Button } from "@/components/ui/button";
import { DiagnosisDialog } from "@/features/diagnostics/diagnosis-dialog";

export function DiagnoseButton({ kind, name, namespace, label = "一键诊断" }: {
  kind: string;
  name: string;
  namespace?: string;
  label?: string;
}) {
  const [open, setOpen] = useState(false);
  const query = useQuery({
    queryKey: ["diagnosis", kind, namespace, name],
    queryFn: () => api.diagnose(kind, name, namespace),
    enabled: open,
    retry: false,
    staleTime: 15_000,
  });
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        <Stethoscope className="h-4 w-4" />{label}
      </Button>
      <DiagnosisDialog
        open={open}
        onOpenChange={setOpen}
        diagnosis={query.data}
        loading={query.isLoading || (open && query.isFetching && !query.data)}
        error={query.isError ? query.error : null}
        onRetry={() => void query.refetch()}
      />
    </>
  );
}
