import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

export const THEME_STORAGE_KEY = "dosh-theme";

export type Theme = "light" | "dark";
export type ThemePreference = Theme | "system";

type ThemeContextValue = {
  preference: ThemePreference;
  theme: Theme;
  setPreference: (preference: ThemePreference) => void;
  toggleTheme: () => void;
};

const ThemeContext = createContext<ThemeContextValue | null>(null);

function isThemePreference(value: unknown): value is ThemePreference {
  return value === "light" || value === "dark" || value === "system";
}

export function readThemePreference(storage: Pick<Storage, "getItem"> | undefined = globalThis.localStorage): ThemePreference {
  try {
    const stored = storage?.getItem(THEME_STORAGE_KEY);
    return isThemePreference(stored) ? stored : "system";
  } catch {
    return "system";
  }
}

export function systemPrefersDark(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function"
    ? window.matchMedia("(prefers-color-scheme: dark)").matches
    : false;
}

export function resolveTheme(preference: ThemePreference, prefersDark = systemPrefersDark()): Theme {
  if (preference === "system") return prefersDark ? "dark" : "light";
  return preference;
}

export function applyTheme(theme: Theme): void {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  root.classList.toggle("dark", theme === "dark");
  root.dataset.theme = theme;
  root.style.colorScheme = theme;
  const themeColor = document.querySelector('meta[name="theme-color"]');
  if (themeColor) themeColor.setAttribute("content", theme === "dark" ? "#10151f" : "#0B63CE");
}

export function initializeTheme(): void {
  applyTheme(resolveTheme(readThemePreference()));
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<ThemePreference>(readThemePreference);
  const [prefersDark, setPrefersDark] = useState(systemPrefersDark);
  const theme = resolveTheme(preference, prefersDark);

  useEffect(() => {
    if (typeof window === "undefined" || typeof window.matchMedia !== "function") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const update = (event: MediaQueryListEvent) => setPrefersDark(event.matches);
    setPrefersDark(media.matches);
    media.addEventListener?.("change", update);
    return () => media.removeEventListener?.("change", update);
  }, []);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    const sync = (event: StorageEvent) => {
      if (event.key === THEME_STORAGE_KEY) setPreferenceState(readThemePreference());
    };
    window.addEventListener("storage", sync);
    return () => window.removeEventListener("storage", sync);
  }, []);

  const setPreference = useCallback((next: ThemePreference) => {
    try {
      if (next === "system") localStorage.removeItem(THEME_STORAGE_KEY);
      else localStorage.setItem(THEME_STORAGE_KEY, next);
    } catch {
      // Storage may be unavailable in hardened browser contexts; the in-memory choice still applies.
    }
    setPreferenceState(next);
  }, []);

  const toggleTheme = useCallback(() => {
    setPreference(theme === "dark" ? "light" : "dark");
  }, [setPreference, theme]);

  const value = useMemo(() => ({ preference, theme, setPreference, toggleTheme }), [preference, setPreference, theme, toggleTheme]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme(): ThemeContextValue {
  const value = useContext(ThemeContext);
  if (!value) throw new Error("useTheme must be used inside ThemeProvider");
  return value;
}
