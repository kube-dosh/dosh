import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import CodeMirror from "@uiw/react-codemirror";
import { yaml as yamlLanguage } from "@codemirror/lang-yaml";
import { EditorView } from "@codemirror/view";
import { CheckCircle2, FileCode2, Loader2, Pencil, RotateCcw, Save } from "lucide-react";
import { toast } from "sonner";
import { api } from "@/api/client";
import { useCapabilities } from "@/features/auth/use-session";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ErrorState, LoadingState } from "@/components/feedback/states";
import { useTheme } from "@/lib/theme";

export function YamlPanel({ type, name, namespace, height = "58vh" }: {
  type: string;
  name: string;
  namespace?: string;
  height?: string;
}) {
  const queryClient = useQueryClient();
  const { theme } = useTheme();
  const capabilities = useCapabilities(namespace).data;
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState("");
  const query = useQuery({
    queryKey: ["yaml", type, namespace, name],
    queryFn: () => api.yaml(type, name, namespace),
    staleTime: 10_000,
  });
  useEffect(() => {
    if (query.data) setValue(query.data.yaml);
  }, [query.data]);
  const apply = useMutation({
    mutationFn: (dryRun: boolean) => api.applyYAML(value, undefined, dryRun),
    onSuccess: async (_, dryRun) => {
      toast.success(dryRun ? "Dry Run 校验通过" : "YAML 已应用");
      setEditing(false);
      await queryClient.invalidateQueries({ queryKey: ["yaml", type, namespace, name] });
    },
    onError: (error) => toast.error(error.message),
  });
  if (query.isLoading) return <LoadingState />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;
  if (!query.data) return null;
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <p className="text-sm text-muted-foreground">{type}{namespace ? ` · ${namespace}` : ""} · 敏感数据已脱敏</p>
        {capabilities?.editYAML ? (
          <Button variant="outline" size="sm" className="ml-auto" onClick={() => setEditing((current) => !current)}>
            {editing ? <RotateCcw className="h-3.5 w-3.5" /> : <Pencil className="h-3.5 w-3.5" />}{editing ? "取消编辑" : "编辑"}
          </Button>
        ) : null}
      </div>
      <div className="overflow-hidden rounded-md border">
        <CodeMirror value={value} onChange={setValue} extensions={[yamlLanguage(), EditorView.lineWrapping]} editable={editing} height={height} theme={theme} basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: editing }} />
      </div>
      {editing ? (
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => apply.mutate(true)} disabled={apply.isPending}><CheckCircle2 className="h-4 w-4" />Dry Run</Button>
          <Button onClick={() => apply.mutate(false)} disabled={apply.isPending}>{apply.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}应用</Button>
        </div>
      ) : null}
    </div>
  );
}

export function YamlDialog({ open, onOpenChange, type, name, namespace }: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  type: string;
  name: string;
  namespace?: string;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-5xl">
        <DialogHeader>
          <div className="flex items-start justify-between gap-4 pr-6">
            <div>
              <DialogTitle className="flex items-center gap-2"><FileCode2 className="h-5 w-5 text-primary" />{name}</DialogTitle>
              <DialogDescription>{type}{namespace ? ` · ${namespace}` : ""} · 只读 YAML 视图，敏感数据已脱敏</DialogDescription>
            </div>
          </div>
        </DialogHeader>
        {open ? <YamlPanel type={type} name={name} namespace={namespace} /> : null}
      </DialogContent>
    </Dialog>
  );
}

