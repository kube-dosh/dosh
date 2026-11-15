import { useEffect, useRef, useState } from "react";
import type { FitAddon } from "@xterm/addon-fit";
import type { Terminal as XTerm } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { CircleStop, Focus, Loader2, Play, RotateCw, TerminalSquare, Trash2 } from "lucide-react";
import type { ContainerInfo } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { cn } from "@/lib/utils";

type TerminalFrame = {
  type: "ready" | "output" | "exit" | "error";
  data?: string;
  message?: string;
};

type ConnectionState = "idle" | "connecting" | "connected" | "closed" | "error";

type TerminalClient = {
  terminal: XTerm;
  fitAddon: FitAddon;
};

const textEncoder = new TextEncoder();

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function base64ToBytes(value: string): Uint8Array {
  const binary = atob(value);
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}

export function PodTerminal({ namespace, name, containers }: { namespace: string; name: string; containers: ContainerInfo[] }) {
  const [container, setContainer] = useState(containers.find((item) => item.state === "Running")?.name || containers[0]?.name || "");
  const [shell, setShell] = useState("sh");
  const [terminalReady, setTerminalReady] = useState(false);
  const [connection, setConnection] = useState<ConnectionState>("idle");
  const [error, setError] = useState("");
  const hostRef = useRef<HTMLDivElement>(null);
  const clientRef = useRef<TerminalClient | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const selectedContainer = containers.find((item) => item.name === container);
  const running = selectedContainer?.state === "Running";
  const busy = connection === "connecting";
  const connected = connection === "connected";

  useEffect(() => {
    if (!containers.some((item) => item.name === container)) {
      setContainer(containers.find((item) => item.state === "Running")?.name || containers[0]?.name || "");
    }
  }, [container, containers]);

  const sendMessage = (message: Record<string, unknown>) => {
    const socket = socketRef.current;
    if (socket?.readyState !== WebSocket.OPEN) return false;
    socket.send(JSON.stringify(message));
    return true;
  };

  const sendInput = (value: string) => sendMessage({ type: "input", data: bytesToBase64(textEncoder.encode(value)) });

  useEffect(() => {
    let disposed = false;
    let client: TerminalClient | undefined;

    void Promise.all([import("@xterm/xterm"), import("@xterm/addon-fit")]).then(([{ Terminal }, { FitAddon }]) => {
      if (disposed || !hostRef.current) return;
      const terminal = new Terminal({
        allowProposedApi: false,
        convertEol: false,
        cursorBlink: true,
        cursorStyle: "block",
        fontFamily: '"JetBrains Mono", "SFMono-Regular", Consolas, monospace',
        fontSize: 13,
        lineHeight: 1.25,
        scrollback: 10_000,
        screenReaderMode: true,
        theme: {
          background: "#020617",
          foreground: "#e2e8f0",
          cursor: "#34d399",
          selectionBackground: "#334155",
          black: "#0f172a",
          red: "#fb7185",
          green: "#34d399",
          yellow: "#fbbf24",
          blue: "#60a5fa",
          magenta: "#c084fc",
          cyan: "#22d3ee",
          white: "#e2e8f0",
        },
      });
      const fitAddon = new FitAddon();
      terminal.loadAddon(fitAddon);
      terminal.open(hostRef.current);
      terminal.onData((data) => {
        if (socketRef.current?.readyState === WebSocket.OPEN) sendInput(data);
      });
      terminal.onResize(({ cols, rows }) => {
        if (socketRef.current?.readyState === WebSocket.OPEN) {
          sendMessage({ type: "resize", columns: cols, rows });
        }
      });
      client = { terminal, fitAddon };
      clientRef.current = client;
      requestAnimationFrame(() => {
        if (disposed) return;
        fitAddon.fit();
        terminal.focus();
        setTerminalReady(true);
      });
    }).catch(() => {
      if (!disposed) setError("终端组件加载失败，请刷新页面后重试。");
    });

    return () => {
      disposed = true;
      socketRef.current?.close();
      socketRef.current = null;
      client?.terminal.dispose();
      clientRef.current = null;
    };
  }, []);

  useEffect(() => {
    const host = hostRef.current;
    const client = clientRef.current;
    if (!host || !client || !terminalReady) return;
    const observer = new ResizeObserver(() => {
      requestAnimationFrame(() => client.fitAddon.fit());
    });
    observer.observe(host);
    return () => observer.disconnect();
  }, [terminalReady]);

  const connect = () => {
    const client = clientRef.current;
    if (!client || !container) return;
    socketRef.current?.close();
    setError("");
    setConnection("connecting");
    client.terminal.clear();
    client.terminal.write("\x1b[2J\x1b[H");

    const scheme = window.location.protocol === "https:" ? "wss:" : "ws:";
    const params = new URLSearchParams({ container, shell, cols: String(client.terminal.cols), rows: String(client.terminal.rows) });
    const socket = new WebSocket(`${scheme}//${window.location.host}/api/v1/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/terminal?${params}`);
    socketRef.current = socket;

    socket.onopen = () => {
      setConnection("connecting");
      client.terminal.write("正在协商容器终端…\r\n");
    };
    socket.onerror = () => {
      setConnection("error");
      setError("终端连接失败，请检查 pods/exec 权限、容器状态和 Shell 是否可用。");
    };
    socket.onclose = () => {
      if (socketRef.current !== socket) return;
      socketRef.current = null;
      setConnection((current) => current === "connected" || current === "connecting" ? "closed" : current);
    };
    socket.onmessage = (event) => {
      let frame: TerminalFrame;
      try {
        frame = JSON.parse(String(event.data)) as TerminalFrame;
      } catch {
        return;
      }
      if (frame.type === "ready") {
        setConnection("connected");
        setError("");
        client.terminal.focus();
        return;
      }
      if (frame.type === "output" && frame.data) {
        client.terminal.write(base64ToBytes(frame.data));
        return;
      }
      if (frame.type === "exit") {
        setConnection("closed");
        client.terminal.write(`\r\n\x1b[2m[会话结束] ${frame.message || ""}\x1b[0m\r\n`);
        return;
      }
      if (frame.type === "error") {
        setError(frame.message || "终端请求失败");
      }
    };
  };

  const disconnect = () => {
    const socket = socketRef.current;
    socketRef.current = null;
    socket?.close();
    setConnection("idle");
    clientRef.current?.terminal.write("\r\n\x1b[2m[已断开终端]\x1b[0m\r\n");
  };

  const clear = () => {
    const terminal = clientRef.current?.terminal;
    if (!terminal) return;
    terminal.clear();
    terminal.write("\x1b[2J\x1b[H");
  };

  const statusText = error || ({ idle: "未连接", connecting: "正在连接", connected: "已连接", closed: "会话已结束", error: "连接失败" } satisfies Record<ConnectionState, string>)[connection];

  return (
    <div className="min-w-0 overflow-hidden rounded-lg border border-slate-800 bg-slate-950 shadow-sm">
      <div className="flex min-w-0 flex-wrap items-center gap-2 border-b border-slate-800 bg-slate-900 px-3 py-2.5">
        <TerminalSquare className="h-4 w-4 shrink-0 text-slate-300" />
        <span className="mr-1 text-xs font-medium text-slate-200">Pod 终端</span>
        <Select value={container} onValueChange={setContainer} disabled={connected || busy}>
          <SelectTrigger className="h-8 w-full border-slate-700 bg-slate-900 text-xs text-slate-200 sm:w-56" aria-label="终端容器">
            <SelectValue placeholder="选择容器" />
          </SelectTrigger>
          <SelectContent>{containers.map((item) => <SelectItem key={item.name} value={item.name}>{item.name}{item.init ? " · Init/Sidecar" : ""}{item.state ? ` · ${item.state}` : ""}{item.ready ? " · Ready" : ""}</SelectItem>)}</SelectContent>
        </Select>
        <Select value={shell} onValueChange={setShell} disabled={connected || busy}>
          <SelectTrigger className="h-8 w-full border-slate-700 bg-slate-900 text-xs text-slate-200 sm:w-32" aria-label="终端 Shell"><SelectValue /></SelectTrigger>
          <SelectContent><SelectItem value="sh">/bin/sh</SelectItem><SelectItem value="bash">/bin/bash</SelectItem><SelectItem value="ash">/bin/ash</SelectItem></SelectContent>
        </Select>
        {connected || busy ? (
          <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={disconnect}><CircleStop className="h-3.5 w-3.5" />断开</Button>
        ) : (
          <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={connect} disabled={!terminalReady || !container || !running}>
            {!terminalReady || busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Play className="h-3.5 w-3.5" />}连接终端
          </Button>
        )}
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => clientRef.current?.terminal.focus()} disabled={!terminalReady} title="聚焦终端输入"><Focus className="h-3.5 w-3.5" />聚焦</Button>
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={() => sendInput("\u0003")} disabled={!connected} title="发送 Ctrl+C"><RotateCw className="h-3.5 w-3.5" />Ctrl+C</Button>
        <Button variant="ghost" size="sm" className="h-8 text-xs text-slate-300 hover:bg-slate-800 hover:text-white" onClick={clear} disabled={!terminalReady}><Trash2 className="h-3.5 w-3.5" />清屏</Button>
        <span role="status" aria-live="polite" className={cn("w-full text-[11px] sm:ml-auto sm:w-auto", error ? "text-red-400" : connected ? "text-emerald-400" : "text-slate-400")}>{statusText}</span>
      </div>
      {!running && selectedContainer ? <div role="alert" className="border-b border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-200">容器当前状态为 {selectedContainer.state || "未知"}，只有 Running 状态的容器可以进入终端。</div> : null}
      {error ? <div role="alert" className="border-b border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-200">{error}</div> : null}
      <div
        ref={hostRef}
        role="application"
        aria-label="Pod 容器终端"
        aria-describedby="pod-terminal-help"
        className="pod-terminal-host h-[52vh] min-h-72 w-full overflow-hidden bg-slate-950 p-2"
      />
      <p id="pod-terminal-help" className="border-t border-slate-800 bg-slate-900 px-3 py-2 text-[11px] leading-5 text-slate-400">
        点击终端后可直接输入命令，支持方向键、Tab 补全和全屏交互程序。会话受 Kubernetes <code>pods/exec</code> RBAC、同源 Origin 校验和审计约束。没有 Shell 的容器需要后续 Ephemeral Container 能力。
      </p>
    </div>
  );
}
