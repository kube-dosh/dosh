import { useEffect, useRef } from "react";
import { api } from "@/api/client";
import { mergeServerResourcePreferences, resourcePreferencesSnapshot, useResourcePreferencesRevision } from "@/lib/resource-preferences";

function comparableSnapshot() {
  return JSON.stringify({ ...resourcePreferencesSnapshot(), updatedAt: undefined });
}

export function useResourcePreferencesSync(enabled: boolean) {
  const revision = useResourcePreferencesRevision();
  const loaded = useRef(false);
  const lastSaved = useRef("");

  useEffect(() => {
    if (!enabled || loaded.current) return;
    let active = true;
    void api.preferences().then((preferences) => {
      if (!active) return;
      mergeServerResourcePreferences(preferences);
      loaded.current = true;
      lastSaved.current = comparableSnapshot();
    }).catch(() => {
      if (!active) return;
      loaded.current = true;
      lastSaved.current = comparableSnapshot();
    });
    return () => { active = false; };
  }, [enabled]);

  useEffect(() => {
    if (!enabled || !loaded.current) return;
    const current = comparableSnapshot();
    if (current === lastSaved.current) return;
    const timer = window.setTimeout(() => {
      void api.savePreferences(resourcePreferencesSnapshot()).then(() => {
        lastSaved.current = current;
      }).catch(() => {
        // Cross-device sync is best effort; localStorage remains the fallback.
      });
    }, 900);
    return () => window.clearTimeout(timer);
  }, [enabled, revision]);
}
