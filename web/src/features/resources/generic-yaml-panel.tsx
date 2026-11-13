import CodeMirror from "@uiw/react-codemirror";
import { yaml as yamlLanguage } from "@codemirror/lang-yaml";
import { EditorView } from "@codemirror/view";
import { FileCode2 } from "lucide-react";
import { useTheme } from "@/lib/theme";

export function GenericYamlPanel({ yaml, height = "64vh" }: { yaml: string; height?: string }) {
  const { theme } = useTheme();
  return (
    <div className="space-y-3">
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <FileCode2 className="h-4 w-4" />
        只读 YAML · 已移除 managedFields；Secret 数据在通用视图中始终遮蔽
      </p>
      <div className="overflow-hidden rounded-md border">
        <CodeMirror
          value={yaml}
          extensions={[yamlLanguage(), EditorView.lineWrapping]}
          editable={false}
          height={height}
          theme={theme}
          basicSetup={{ lineNumbers: true, foldGutter: true, highlightActiveLine: false }}
        />
      </div>
    </div>
  );
}
