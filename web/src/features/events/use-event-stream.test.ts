import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { EventItem } from "@/api/types";
import { eventIdentity, MAX_RECONNECT_DELAY_MS, mergeEventRows, reconnectDelay, useEventStream } from "@/features/events/use-event-stream";

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  onopen: (() => void) | null = null;
  closed = false;
  listeners = new Map<string, ((event: MessageEvent<string>) => void)[]>();

  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
    const current = this.listeners.get(type) ?? [];
    current.push(listener);
    this.listeners.set(type, current);
  }

  close() {
    this.closed = true;
  }

  emitOpen() {
    this.onopen?.();
  }

  emit(type: string, data = "") {
    this.listeners.get(type)?.forEach((listener) => listener(new MessageEvent(type, { data })));
  }
}

function event(overrides: Partial<EventItem> = {}): EventItem {
  return {
    uid: "event-1",
    namespace: "default",
    type: "Warning",
    reason: "BackOff",
    message: "restarting",
    objectKind: "Pod",
    objectName: "web",
    count: 1,
    firstSeen: "2026-11-04T00:00:00Z",
    lastSeen: "2026-11-04T00:00:00Z",
    ...overrides,
  };
}

describe("event stream helpers", () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
    vi.spyOn(Math, "random").mockReturnValue(0.5);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("uses capped exponential backoff with bounded jitter", () => {
    expect([1, 2, 3, 4, 5, 6, 7].map((attempt) => reconnectDelay(attempt, () => 0.5))).toEqual([
      1_000, 2_000, 4_000, 8_000, 16_000, 30_000, 30_000,
    ]);
    expect(reconnectDelay(1, () => 0)).toBe(800);
    expect(reconnectDelay(1, () => 1)).toBe(1_200);
    expect(reconnectDelay(20, () => 1)).toBeLessThanOrEqual(MAX_RECONNECT_DELAY_MS * 1.2);
  });

  it("deduplicates by immutable event UID and keeps the newest payload", () => {
    const initial = event({ count: 1 });
    const updated = event({ count: 4, lastSeen: "2026-11-04T00:01:00Z" });
    const other = event({ uid: "event-2", reason: "Pulled" });
    const merged = mergeEventRows([initial], [updated, other, updated]);
    expect(merged).toHaveLength(2);
    expect(merged[0]).toMatchObject({ uid: "event-1", count: 4 });
    expect(merged[1]).toMatchObject({ uid: "event-2" });
    expect(new Set(merged.map(eventIdentity)).size).toBe(merged.length);
  });

  it("falls back to a stable composite key when the API omits UID", () => {
    const legacy = event({ uid: "" });
    expect(eventIdentity(legacy)).toBe("default|Pod|web|BackOff|2026-11-04T00:00:00Z");
  });

  it("closes a failed EventSource and reconnects with backoff", () => {
    vi.useFakeTimers();
    const { result } = renderHook(() => useEventStream("default", true));
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(result.current.status).toBe("connecting");

    act(() => FakeEventSource.instances[0].emit("error", "upstream interrupted"));
    expect(FakeEventSource.instances[0].closed).toBe(true);
    expect(result.current).toMatchObject({ status: "reconnecting", attempt: 1 });
    expect(result.current.error).toContain("1 秒后进行第 1 次退避重连");

    act(() => vi.advanceTimersByTime(1_000));
    expect(FakeEventSource.instances).toHaveLength(2);
    act(() => FakeEventSource.instances[1].emitOpen());
    expect(result.current).toMatchObject({ status: "connected", attempt: 0, error: "" });
  });
});
