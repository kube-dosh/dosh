import { useEffect, useMemo, useRef, useState } from "react";
import { CircleStop, Clock3, History, Layers3, Loader2, Play, RotateCw, Search, Trash2 } from "lucide-react";
import { streamWorkloadLogs } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { createLogSanitizer } from "@/features/workloads/log-sanitizer";
import { createLogStreamParser, type LogStreamFrame } from "@/features/workloads/log-stream";
import { cn } from "@/lib/utils";

const maxLogBytes = 1_500_000;

export function WorkloadLogViewer({ kind, namespace, name, containers, initialContainer, lockContainer = false }: {
  kind: string;
  namespace: string;
  name: string;
  containers: { name: string; init?: boolean }[];
  initialContainer?: string;
  lockContainer?: boolean;
}) {
  const logContainers = containers.filter((item) => item.name);
  const [container, setContainer] = useState(initialContainer || logContainers.find((item) => !item.init)?.name || logContainers[0]?.name || "");
  const [follow, setFollow] = useState(true);
  const [tailLines, setTailLines] = useState(300);
  const [timestamps, setTimestamps] = useState(false);
  const [sinceSeconds, setSinceSeconds] = useState(0);
  const [previous, setPrevious] = useState(false);
  const [filter, setFilter] = useState("");
  const [logs, setLogs] = useState("");
  const [warnings, setWarnings] = useState<string[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [generation, setGeneration] = useState(0);
  const outputRef = useRef<HTMLPreElement>(null);
  const stickToBottom = useRef(true);

  useEffect(() => {
    if (!logContainers.length) return;
    const controller = new AbortController();
    let active = true;
    setLogs("");
    setWarnings([]);
    setError("");
    setLoading(true);
    stickToBottom.current = true;
    const run = async () => {
      try {
        const reader = await streamWorkloadLogs(kind, namespace, name, {
          container: container || undefined,
          tailLines,
          follow: follow && !previous,
          previous,
          timestamps,
          sinceSeconds,
          signal: controller.signal,
        });
        const decoder = new TextDecoder();
        const sanitizer = createLogSanitizer();
        const parser = createLogStreamParser();
        let terminalError = "";
        let ended = false;
        const append = (frame: Extract<LogStreamFrame, { type: "log" }>) => {
          const chunk = sanitizer.push(frame.data);
          if (!chunk) return;
          const source = frame.pod ? `[${frame.pod}${frame.container ? `/${frame.container}` : ""}] ` : "";
          const prefixed = source ? chunk.split("\n").map((line) => line ? source + line : line).join("\n") : chunk;
          setLogs((current) => (current + prefixed).slice(-maxLogBytes));
        };
        const handleFrames = (frames: LogStreamFrame[]) => {
          for (const frame of frames) {
            if (frame.type === "log") append(frame);
            else if (frame.type === "warning") setWarnings((current) => current.includes(frame.message) ? current : [...current, frame.message]);
            else if (frame.type === "error") terminalError = frame.requestId ? `${frame.message}（请求 ID：${frame.requestId}）` : frame.message;
            else if (frame.type === "end") ended = true;
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
  }, [container, follow, generation, kind, name, namespace, previous, sinceSeconds, tailLines, timestamps, logContainers.length]);

  useEffect(() => {
    if (initialContainer && containers.some((item) => item.name === initialContainer)) setContainer(initialContainer);
  }, [containers, initialContainer]);

  const visibleLogs = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return logs;
    return logs.split("\n").filter((line) => line.toLowerCase().includes(needle)).join("\n");
  }, [filter, logs]);

  useEffect(() => {
    if (stickToBottom.current && outputRef.current) outputRef.current.scrollTop = outputRef.current.scrollHeight;
  }, [logs]);

  if (!logContainers.length) {
    return <p className="rounded-lg border border-dashed px-5 py-10 text-center text-sm text-muted-foreground">该工作负载没有可读取日志的容器。</p>;
  }

  return (
    <div className="min-w-0 overflow-hidden rounded-lg border bg-slate-950">
      <div className="flex flex-wrap items-center gap-2 border-b border-slate-800 bg-slate-900 px-3 py-2.5">
        <span className="inline-flex items-center gap-1.5 rounded-md border border-slate-700 px-2 py-1 text-[11px] text-slate-300"><Layers3 className="h-3.5 w-3.5" />多实例聚合</span>
        <Select value={container || (lockContainer ? "" : "all")} onValueChange={(value) => setContainer(value === "all" ? "" : value)} disabled={lockContainer}>
          <SelectTrigger className="h-8 w-44 border-slate-700 bg-slate-900 text-xs text-slate-200" aria-label="选择容器"><SelectValue /></SelectTrigger>
          <SelectContent>
            {!lockContainer ? <SelectItem value="all">全部容器</SelectItem> : null}
            {logContainers.map((item) => <SelectItem key={item.name} value={item.name}>{item.name}{item.init ? " · Init/Sidecar" : ""}</SelectItem>)}
          </SelectContent>
        </Select>
        <Select value={String(tailLines)} onValueChange={(value) => setTailLines(Number(value))}>
          <SelectTrigger className="h-8 w-28 border-slate-700 bg-slate-900 text-xs text-slate-200" aria-label="每个实例日志行数"><SelectValue /></SelectTrigger>
          <SelectContent>{[100, 300, 1000, 3000].map((value) => <SelectItem key={value} value={String(value)}>每流 {value} 行</SelectItem>)}</SelectContent>
        </Select>
        <Select value={String(sinceSeconds)} onValueChange={(value) => setSinceSeconds(Number(value))}>
          <SelectTrigger className="h-8 w-32 border-slate-700 bg-slate-900 text-xs text-slate-200" aria-label="日志时间范围"><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value="0">全部时间</SelectItem>
            <SelectItem value="300">最近 5 分钟</SelectItem>
            <SelectItem value="900">最近 15 分钟</SelectItem>
            <SelectItem value="3600">最近 1 小时</SelectItem>
            <SelectItem value="21600">最近 6 小时</SelectItem>
          </SelectContent>
        </Select>
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => { setPrevious(false); setFollow((value) => !value); }}>
          {follow ? <CircleStop className="h-3.5 w-3.5" /> : <Play className="h-3.5 w-3.5" />}{follow ? "停止跟随" : "跟随日志"}
        </Button>
        <Button variant="ghost" size="sm" aria-pressed={previous} className={cn("h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white", previous && "bg-slate-800 text-white")} onClick={() => { setFollow(false); setPrevious((value) => !value); }}>
          <History className="h-3.5 w-3.5" />{previous ? "读取当前实例" : "上一次实例"}
        </Button>
        <Button variant="ghost" size="sm" aria-pressed={timestamps} className={cn("h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white", timestamps && "bg-slate-800 text-white")} onClick={() => setTimestamps((value) => !value)}>
          <Clock3 className="h-3.5 w-3.5" />{timestamps ? "隐藏时间戳" : "显示时间戳"}
        </Button>
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => setGeneration((value) => value + 1)}><RotateCw className="h-3.5 w-3.5" />重新加载</Button>
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => setLogs("")}><Trash2 className="h-3.5 w-3.5" />清屏</Button>
        <div className="relative min-w-44 flex-1 sm:max-w-64">
          <Search className="absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-slate-500" />
          <Input value={filter} onChange={(event) => setFilter(event.target.value)} placeholder="筛选当前日志窗口" aria-label="筛选当前聚合日志窗口" className="h-8 border-slate-700 bg-slate-900 pl-8 text-xs text-slate-200 placeholder:text-slate-500" />
        </div>
        <span role="status" aria-live="polite" className={cn("ml-auto flex min-w-0 items-center gap-1.5 text-[11px]", error ? "text-red-400" : loading ? "text-amber-300" : "text-emerald-400")}>
          <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", error ? "bg-red-400" : loading ? "animate-pulse bg-amber-300" : "bg-emerald-400")} />
          <span className="truncate">{error ? error : loading ? (follow ? "正在跟随多个实例" : "正在读取多个实例") : follow ? "聚合流已结束" : "静态聚合日志"}</span>
        </span>
        {loading ? <Loader2 className="h-4 w-4 animate-spin text-slate-400" /> : null}
      </div>
      {warnings.length ? <div role="status" className="border-b border-amber-900/60 bg-amber-950/40 px-3 py-2 text-[11px] leading-5 text-amber-200">{warnings.join("；")}</div> : null}
      <pre
        ref={outputRef}
        className="h-[58vh] min-h-96 overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-xs leading-5 text-slate-200 scrollbar-thin"
        onScroll={(event) => {
          const element = event.currentTarget;
          stickToBottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < 24;
        }}
      >{visibleLogs || (loading ? "正在连接工作负载日志流…" : filter ? "当前日志窗口没有匹配内容" : "暂无日志输出")}</pre>
      <div className="border-t border-slate-800 bg-slate-900 px-3 py-2 text-[11px] leading-5 text-slate-400">
        每行以 [Pod/容器] 标记来源；最多同时聚合 20 条流，超出部分由服务端提示。{previous ? " 正在读取上一次容器实例。" : ""}{filter ? ` 筛选结果 ${visibleLogs.split("\n").filter(Boolean).length} 行。` : ""}
      </div>
    </div>
  );
}
