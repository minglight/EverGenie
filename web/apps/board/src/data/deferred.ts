import { ApiError } from "@evergenie/sdk-web";
import { dismissToast, showToast } from "@evergenie/ui";
import { t } from "../i18n/zh-TW";
import { createStore, useStore } from "./store";

// 可逆的動作（待辦的「做了」「取消」）先在畫面上生效，toast 帶「復原」；5 秒內沒按才真的呼叫 API
// （FEN-21）。離開頁面（pagehide）或換路由時，所有等著的動作立刻送出。放在模組層，畫面換掉也不會掉。

interface Entry {
  key: string;
  run: () => Promise<void>;
  timer: ReturnType<typeof setTimeout>;
  toastId: string | number;
  settled: () => Promise<void> | void;
}

const queue = new Map<string, Entry>();
const hiddenStore = createStore<ReadonlySet<string>>(new Set());

/** 5 秒（FEN-21）。 */
export const DEFER_MS = 5000;

/** 已經在畫面上生效、但還沒真的送出的待辦編號（清單要把它們藏起來）。 */
export function useDeferredIds(): ReadonlySet<string> {
  return useStore(hiddenStore);
}

function setHidden(key: string, hidden: boolean) {
  const next = new Set(hiddenStore.get());
  if (hidden) next.add(key);
  else next.delete(key);
  hiddenStore.set(next);
}

async function fire(key: string) {
  const entry = queue.get(key);
  if (!entry) return;
  queue.delete(key);
  clearTimeout(entry.timer);
  dismissToast(entry.toastId);
  try {
    await entry.run();
    await entry.settled();
  } catch (err) {
    showToast(
      t("ui.reasonFailed", { reason: err instanceof ApiError ? err.message : String(err) }),
    );
  } finally {
    setHidden(key, false);
  }
}

export interface DeferOptions {
  /** 對象的編號（同一個編號同時只會有一個等著的動作）。 */
  key: string;
  /** toast 的字：「做完了：打給牙醫」。 */
  message: string;
  /** 5 秒後真的要做的事（呼叫 API）。 */
  run: () => Promise<void>;
  /** 做完之後（成功才會到這裡）：重抓資料。 */
  settled: () => Promise<void> | void;
}

export function deferAction({ key, message, run, settled }: DeferOptions) {
  if (queue.has(key)) return;
  setHidden(key, true);
  const toastId = showToast(message, {
    durationMs: DEFER_MS,
    undo: {
      label: t("ui.undo"),
      onClick: () => {
        const entry = queue.get(key);
        if (!entry) return;
        clearTimeout(entry.timer);
        queue.delete(key);
        setHidden(key, false);
      },
    },
  });
  queue.set(key, {
    key,
    run,
    settled,
    toastId,
    timer: setTimeout(() => void fire(key), DEFER_MS),
  });
}

/** 立刻送出所有等著的動作（pagehide、換路由）。 */
export function flushDeferred() {
  for (const key of [...queue.keys()]) void fire(key);
}

/** 目前有沒有等著的動作（測試與畫面用）。 */
export function hasDeferred(key: string): boolean {
  return queue.has(key);
}

let installed = false;

/** 掛上 pagehide；只裝一次。換路由由 router 的 onBeforeNavigate 呼叫 flushDeferred。 */
export function installDeferredFlush() {
  if (installed || typeof window === "undefined") return;
  installed = true;
  window.addEventListener("pagehide", flushDeferred);
}
