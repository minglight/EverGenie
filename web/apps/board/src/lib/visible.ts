import { useSyncExternalStore } from "react";

/** 這個分頁現在看得到嗎（沒被切到背景）。 */
export function useDocumentVisible(): boolean {
  return useSyncExternalStore(
    (onChange) => {
      document.addEventListener("visibilitychange", onChange);
      return () => document.removeEventListener("visibilitychange", onChange);
    },
    () => document.visibilityState === "visible",
    () => true,
  );
}
