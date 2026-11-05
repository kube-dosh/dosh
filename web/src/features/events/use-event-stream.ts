import { useCallback, useEffect, useRef, useState } from "react";
import { eventSource } from "@/api/client";
import type { EventItem } from "@/api/types";

export const MAX_RECONNECT_DELAY_MS = 30_000;

export function reconnectDelay(attempt: number, random: () => number = Math.random): number {
  const exponent = Math.max(0, Math.min(attempt - 1, 5));
  const base = Math.min(MAX_RECONNECT_DELAY_MS, 1_000 * 2 ** exponent);
  return Math.round(base * (0.8 + random() * 0.4));
}

export function eventIdentity(event: EventItem): string {
  return event.uid || [event.namespace, event.objectKind, event.objectName, event.reason, event.firstSeen].join("|");
}

export function mergeEventRows(current: EventItem[], incoming: EventItem[], limit = 300): EventItem[] {
  const unique = new Map<string, EventItem>();
  for (const item of [...incoming, ...current]) {
    const key = eventIdentity(item);
    if (!unique.has(key)) unique.set(key, item);
  }
  return [...unique.values()].slice(0, limit);
}

type StreamStatus = "idle" | "connecting" | "connected" | "reconnecting";

export function useEventStream(namespace: string, enabled: boolean) {
  const [events, setEvents] = useState<EventItem[]>([]);
  const [status, setStatus] = useState<StreamStatus>("idle");
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [generation, setGeneration] = useState(0);
  const sourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    setEvents([]);
    setError("");
    setAttempt(0);
    if (!enabled) {
      setStatus("idle");
      return;
    }

    let cancelled = false;
    let retryAttempt = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const connect = () => {
      if (cancelled) return;
      setStatus(retryAttempt === 0 ? "connecting" : "reconnecting");
      const source = eventSource(namespace || undefined);
      sourceRef.current = source;
      source.onopen = () => {
        if (cancelled || source !== sourceRef.current) return;
        retryAttempt = 0;
        setAttempt(0);
        setError("");
        setStatus("connected");
      };
      source.addEventListener("event", (message) => {
        if (cancelled || source !== sourceRef.current) return;
        try {
          const item = JSON.parse((message as MessageEvent<string>).data) as EventItem;
          setEvents((current) => mergeEventRows(current, [item]));
        } catch {
          setError("收到无法解析的事件数据");
        }
      });
      source.addEventListener("error", (event) => {
        if (cancelled || source !== sourceRef.current) return;
        source.close();
        retryAttempt += 1;
        const delay = reconnectDelay(retryAttempt);
        const detail = event instanceof MessageEvent && typeof event.data === "string" && event.data
          ? event.data
          : "实时事件连接中断";
        setAttempt(retryAttempt);
        setStatus("reconnecting");
        setError(`${detail}，${Math.ceil(delay / 1000)} 秒后进行第 ${retryAttempt} 次退避重连`);
        timer = setTimeout(connect, delay);
      });
    };
    connect();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      sourceRef.current?.close();
      sourceRef.current = null;
    };
  }, [enabled, generation, namespace]);

  const reconnect = useCallback(() => setGeneration((value) => value + 1), []);
  return { events, status, error, attempt, reconnect };
}
