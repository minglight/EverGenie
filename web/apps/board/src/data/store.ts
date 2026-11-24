import { useSyncExternalStore } from "react";

// 很小的外部狀態：畫面以外還要活著的狀態（送出中的答案、等著 5 秒後才送的動作）。
// 每次更新都換一個新的物件，useSyncExternalStore 才看得出變了。
export interface Store<T> {
  get: () => T;
  set: (next: T) => void;
  subscribe: (fn: () => void) => () => void;
}

export function createStore<T>(initial: T): Store<T> {
  let state = initial;
  const listeners = new Set<() => void>();
  return {
    get: () => state,
    set(next: T) {
      state = next;
      for (const fn of listeners) fn();
    },
    subscribe(fn) {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
  };
}

export function useStore<T>(store: Store<T>): T {
  return useSyncExternalStore(store.subscribe, store.get, store.get);
}
