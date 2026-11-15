import { useEffect, useMemo, useRef, useState } from "react";
import { CircleStop, Loader2, Play, RotateCw, Send, TerminalSquare, Trash2 } from "lucide-react";
import type { ContainerInfo } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { createLogSanitizer } from "@/features/workloads/log-sanitizer";
import { cn } from "@/lib/utils";

type TerminalFrame = {
  type: "ready" | "output" | "exit" | "error";
  data?: string;
  message?: string;
};

const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();

export function PodTerminal({ namespace, name, containers }: { namespace: string; name: string; containers: ContainerInfo[] }) {
  const [container, setContainer] = useState(containers[0]?.name || "");
  const [shell, setShell] = useState("sh");
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [output, setOutput] = useState("点击“连接终端”启动 Pod 会话。\n");
  const [error, setError] = useState("");
  const [command, setCommand] = useState("");
  const socketRef = useRef<WebSocket | null>(null);
  const outputRef = useRef<HTMLPreElement>(null);
  const stickToBottom = useRef(true);
  const sanitizer = useMemo(() => createLogSanitizer(), []);

  useEffect(() => () => {
    socketRef.current?.close();
    socketRef.current = null;
  }, []);

  useEffect(() => {
    if (stickToBottom.current && outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight;
    }
  }, [output]);

  const send = (message: Record<string, unknown>) => {
    const socket = socketRef.current;
    if (socket?.readyState !== WebSocket.OPEN) return false;
    socket.send(JSON.stringify(message));
    return true;
  };

  const sendInput = (value: string) => {
    const bytes = textEncoder.encode(value);
    let binary = "";
    bytes.forEach((byte) => { binary += String.fromCharCode(byte); });
    return send({ type: "input", data: btoa(binary) });
  };

  const connect = () => {
    socketRef.current?.close();
    setConnecting(true);
    setError("");
    setOutput("");
    const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
    const params = new URLSearchParams({ container, shell, cols: "100", rows: "30" });
    const socket = new WebSocket(`${scheme}//${window.location.host}/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/terminal?${params}`);
    socketRef.current = socket;
    socket.onopen = () => { setConnecting(false); setConnected(true); };
    socket.onerror = () => { setConnecting(false); setConnected(false); setError("终端连接失败，请检查 Pod Exec 权限或容器 shell"); };
    socket.onclose = () => { setConnected(false); setConnecting(false); };
    socket.onmessage = (event) => {
      let frame: TerminalFrame;
      try { frame = JSON.parse(String(event.data)) as TerminalFrame; } catch { return; }
      if (frame.type === "ready") {
        setConnecting(false);
        setConnected(true);
        setOutput((value) => `${value}终端已连接。\n`);
        return;
      }
      if (frame.type === "output" && frame.data) {
        const binary = atob(frame.data);
        const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
        const text = sanitizer.push(textDecoder.decode(bytes, { stream: true }));
        setOutput((value) => `${value}${text}`.slice(-1_500_000));
        return;
      }
      if (frame.type === "exit") {
        setConnected(false);
        setOutput((value) => `${value}\n[会话结束] ${frame.message || ""}\n`);
        return;
      }
      if (frame.type === "error") {
        setError(frame.message || "终端请求失败");
      }
    };
  };

  const disconnect = () => {
    socketRef.current?.close();
    socketRef.current = null;
    setConnected(false);
    setConnecting(false);
  };

  const resize = () => {
    const element = outputRef.current;
    if (!element || !connected) return;
    const columns = Math.max(20, Math.min(300, Math.floor(element.clientWidth / 7.5)));
    const rows = Math.max(5, Math.min(100, Math.floor(element.clientHeight / 20)));
    send({ type: "resize", columns, rows });
  };

  useEffect(() => {
    const element = outputRef.current;
    if (!element) return;
    const observer = new ResizeObserver(resize);
    observer.observe(element);
    return () => observer.disconnect();
  });

  return (
    <div className="min-w-0 overflow-hidden rounded-lg border bg-slate-950">
      <div className="flex min-w-0 flex-wrap items-center gap-2 border-b border-slate-800 bg-slate-900 px-3 py-2.5">
        <TerminalSquare className="h-4 w-4 shrink-0 text-slate-300" />
        <Select value={container} onValueChange={setContainer} disabled={connected || connecting}>
          <SelectTrigger className="h-8 w-full border-slate-700 bg-slate-900 text-xs text-slate-200 sm:w-52" aria-label="终端容器"><SelectValue placeholder="选择容器" /></SelectTrigger>
          <SelectContent>{containers.map((item) => <SelectItem key={item.name} value={item.name}>{item.name}{item.ready ? " · Ready" : ""}</SelectItem>)}</SelectContent>
        </Select>
        <Select value={shell} onValueChange={setShell} disabled={connected || connecting}>
          <SelectTrigger className="h-8 w-full border-slate-700 bg-slate-900 text-xs text-slate-200 sm:w-32" aria-label="终端 Shell"><SelectValue /></SelectTrigger>
          <SelectContent><SelectItem value="sh">/bin/sh</SelectItem><SelectItem value="bash">/bin/bash</SelectItem><SelectItem value="ash">/bin/ash</SelectItem></SelectContent>
        </Select>
        {connected ? (
          <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={disconnect}><CircleStop className="h-3.5 w-3.5" />断开</Button>
        ) : (
          <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={connect} disabled={connecting || !container}>
            {connecting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}连接终端
          </Button>
        )}
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => setOutput("")}><Trash2 className="h-3.5 w-3.5" />清屏</Button>
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => sendInput("\u0003")} disabled={!connected} title="发送 Ctrl+C"><RotateCw className="h-3.5 w-3.5" />Ctrl+C</Button>
        <span role="status" aria-live="polite" className={cn("ml-auto text-[11px]", error ? "text-red-400" : connected ? "text-emerald-400" : "text-slate-400")}>
          {error || (connected ? "已连接" : connecting ? "正在连接" : "未连接")}
        </span>
      </div>
      <pre
        ref={outputRef}
        role="log"
        aria-label="终端输出"
        tabIndex={0}
        onScroll={(event) => {
          const element = event.currentTarget;
          stickToBottom.current = element.scrollHeight - element.scrollTop - element.clientHeight < 24;
        }}
        className="h-[52vh] min-h-72 max-w-full overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-xs leading-5 text-slate-200 outline-none scrollbar-thin focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-sky-500"
      >{output || (connecting ? "正在建立终端连接…" : "终端输出将显示在这里")}</pre>
      <form
        className="flex min-w-0 items-center gap-2 border-t border-slate-800 bg-slate-900 px-3 py-2"
        onSubmit={(event) => {
          event.preventDefault();
          if (!sendInput(`${command}\r`)) {
            setError("终端尚未连接");
            return;
          }
          setCommand("");
          setError("");
        }}
      >
        <span aria-hidden="true" className="shrink-0 font-mono text-xs text-emerald-400">$</span>
        <Input value={command} onChange={(event) => setCommand(event.target.value)} disabled={!connected} placeholder={connected ? "输入命令并按 Enter" : "连接后可输入命令"} aria-label="终端命令" className="h-8 min-w-0 border-slate-700 bg-slate-950 font-mono text-xs text-slate-200 placeholder:text-slate-500" />
        <Button type="submit" size="sm" className="h-8 shrink-0" disabled={!connected}><Send className="h-3.5 w-3.5" />发送</Button>
      </form>
      <p className="border-t border-slate-800 bg-slate-900 px-3 py-2 text-[11px] text-slate-400">
        终端会话受 Kubernetes <code>pods/exec</code> RBAC、会话审计和浏览器同源策略约束。命令输入模式适合移动端；交互式全屏程序请使用 kubectl exec。
      </p>
    </div>
  );
}
