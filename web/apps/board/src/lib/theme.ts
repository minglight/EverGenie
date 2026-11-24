import { useCallback, useSyncExternalStore } from "react";

export type Theme = "light" | "dark";

const KEY = "eg.theme";

function read(): Theme | null {
  try {
    const v = localStorage.getItem(KEY);
    return v === "light" || v === "dark" ? v : null;
  } catch {
    return null;
  }
}

function write(theme: Theme) {
  try {
    localStorage.setItem(KEY, theme);
  } catch {
    // 無痕模式等寫不進去：只是少了「記住」，不影響使用。
  }
}

/** 開頁面時套用上次選的主題；沒選過就跟系統。 */
export function applyStoredTheme() {
  const stored = read();
  if (stored) document.documentElement.dataset.theme = stored;
}

function effective(): Theme {
  const forced = document.documentElement.dataset.theme;
  if (forced === "light" || forced === "dark") return forced;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

const listeners = new Set<() => void>();

function subscribe(fn: () => void) {
  listeners.add(fn);
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  mq.addEventListener("change", fn);
  return () => {
    listeners.delete(fn);
    mq.removeEventListener("change", fn);
  };
}

export function useTheme(): { theme: Theme; toggle: () => void } {
  const theme = useSyncExternalStore(subscribe, effective, () => "light" as Theme);
  const toggle = useCallback(() => {
    const next: Theme = effective() === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    write(next);
    for (const fn of listeners) fn();
  }, []);
  return { theme, toggle };
}
