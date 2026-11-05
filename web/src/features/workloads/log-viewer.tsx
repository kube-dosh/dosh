import { useEffect, useRef, useState } from "react";
import { CircleStop, Clock3, Loader2, Play, RotateCw, Trash2 } from "lucide-react";
import { streamLogs } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { createLogSanitizer } from "@/features/workloads/log-sanitizer";
import { createLogStreamParser, type LogStreamFrame } from "@/features/workloads/log-stream";
import { cn } from "@/lib/utils";

const maxLogBytes = 1_500_000;

export function LogViewer({ namespace, name, containers }: { namespace: string; name: string; containers: { name: string; ready: boolean }[] }) {
  const [container, setContainer] = useState(containers[0]?.name || "");
  const [follow, setFollow] = useState(true);
  const [tailLines, setTailLines] = useState(300);
  const [timestamps, setTimestamps] = useState(false);
  const [logs, setLogs] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [generation, setGeneration] = useState(0);
  const outputRef = useRef<HTMLPreElement>(null);
  const stickToBottom = useRef(true);

  useEffect(() => {
    if (!container) return;
    const controller = new AbortController();
    let active = true;
    setLogs("");
    setError("");
    setLoading(true);
    stickToBottom.current = true;
    const run = async () => {
      try {
        const reader = await streamLogs(namespace, name, { container, tailLines, follow, timestamps, signal: controller.signal });
        const decoder = new TextDecoder();
        const sanitizer = createLogSanitizer();
        const parser = createLogStreamParser();
        let terminalError = "";
        let ended = false;
        const handleFrames = (frames: LogStreamFrame[]) => {
          for (const frame of frames) {
            if (frame.type === "log") {
              const chunk = sanitizer.push(frame.data);
              if (chunk) setLogs((current) => (current + chunk).slice(-maxLogBytes));
            } else if (frame.type === "error") {
              terminalError = frame.requestId ? `${frame.message}（请求 ID：${frame.requestId}）` : frame.message;
              return;
            } else if (frame.type === "end") {
              ended = true;
              return;
            }
          }
        };
        while (active) {
          const { value, done } = await reader.read();
          if (done) {
            const trailing = decoder.decode();
            if (trailing) handleFrames(parser.push(trailing));
            handleFrames(parser.flush());
            break;
          }
          handleFrames(parser.push(decoder.decode(value, { stream: true })));
          if (terminalError || ended) {
            await reader.cancel();
            break;
          }
        }
        if (active && !controller.signal.aborted) {
          if (terminalError) setError(terminalError);
          else if (!ended) setError("日志流意外中断（未收到结束帧）");
        }
      } catch (streamError) {
        if (!controller.signal.aborted) setError(streamError instanceof Error ? streamError.message : "日志流中断");
      } finally {
        if (active) setLoading(false);
      }
    };
    void run();
    return () => { active = false; controller.abort(); };
  }, [container, follow, generation, name, namespace, tailLines, timestamps]);

  useEffect(() => {
    if (stickToBottom.current && outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [logs]);

  return (
    <div className="overflow-hidden rounded-lg border bg-slate-950">
      <div className="flex flex-wrap items-center gap-2 border-b border-slate-800 bg-slate-900 px-3 py-2.5">
        <Select value={container} onValueChange={setContainer}>
          <SelectTrigger className="h-8 w-52 border-slate-700 bg-slate-900 text-xs text-slate-200"><SelectValue placeholder="选择容器" /></SelectTrigger>
          <SelectContent>{containers.map((item) => <SelectItem key={item.name} value={item.name}>{item.name}{item.ready ? " · Ready" : ""}</SelectItem>)}</SelectContent>
        </Select>
        <Select value={String(tailLines)} onValueChange={(value) => setTailLines(Number(value))}>
          <SelectTrigger className="h-8 w-32 border-slate-700 bg-slate-900 text-xs text-slate-200"><SelectValue /></SelectTrigger>
          <SelectContent>{[100, 300, 1000, 3000].map((value) => <SelectItem key={value} value={String(value)}>最近 {value} 行</SelectItem>)}</SelectContent>
        </Select>
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => setFollow((value) => !value)}>
          {follow ? <CircleStop className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}{follow ? "停止跟随" : "跟随日志"}
        </Button>
        <Button variant="ghost" size="sm" aria-pressed={timestamps} className={cn("h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white", timestamps && "bg-slate-800 text-white")} onClick={() => setTimestamps((value) => !value)}>
          <Clock3 className="h-3.5 w-3.5" />{timestamps ? "隐藏时间戳" : "显示时间戳"}
        </Button>
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => setGeneration((value) => value + 1)}><RotateCw className="h-3.5 w-3.5" />重新加载</Button>
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => setLogs("")}><Trash2 className="h-3.5 w-3.5" />清屏</Button>
        <span role="status" aria-live="polite" className={cn("ml-auto flex items-center gap-1.5 text-[11px]", error ? "text-red-400" : loading ? "text-amber-300" : "text-emerald-400")}>
          <span className={cn("h-1.5 w-1.5 rounded-full", error ? "bg-red-400" : loading ? "animate-pulse bg-amber-300" : "bg-emerald-400")} />
          {error ? error : loading ? (follow ? "正在跟随" : "正在读取") : follow ? "流已结束" : "静态日志"}
        </span>
        {loading ? <Loader2 className="h-4 w-4 animate-spin text-slate-400" /> : null}
      </div>
      <pre
        ref={outputRef}
        className="h-[58vh] min-h-96 overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-xs leading-5 text-slate-200 scrollbar-thin"
        onScroll={(event) => {
          const element = event.currentTarget;
          stickToBottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < 24;
        }}
      >{logs || (loading ? "正在连接日志流…" : "暂无日志输出")}</pre>
      <div className="border-t border-slate-800 bg-slate-900 px-3 py-2 text-[11px] text-slate-400">日志仅用于当前会话查看，不会写入浏览器持久存储。</div>
    </div>
  );
}
