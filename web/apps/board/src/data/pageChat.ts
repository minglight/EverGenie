import { createStore, useStore } from "./store";

// 套件頁旁邊的聊天跟誰聊（REQ-235）：每個「頁」（section，或沒有 section 的套件頁）記一位，
// 存 localStorage（方便類的資料，FEN-5 允許）；讀寫都包 try/catch，讀不到就用頁面宣告的預設秘書。

export const PAGE_CHAT_KEY = "eg.pageChat.v1";

export function parsePicks(raw: string | null): Record<string, string> {
  try {
    const v: unknown = JSON.parse(raw ?? "{}");
    if (!v || typeof v !== "object" || Array.isArray(v)) return {};
    return Object.fromEntries(
      Object.entries(v as Record<string, unknown>).filter(
        (e): e is [string, string] => typeof e[1] === "string",
      ),
    );
  } catch {
    return {};
  }
}

function read(): Record<string, string> {
  try {
    return parsePicks(localStorage.getItem(PAGE_CHAT_KEY));
  } catch {
    return {};
  }
}

const picksStore = createStore<Record<string, string>>(read());

/** 這一頁要跟誰聊：主人選過的優先，其次是頁面宣告的預設秘書（要在名冊上而且啟用）；都沒有回空字串。 */
export function chatAgentFor(
  picks: Record<string, string>,
  key: string,
  fallback: string,
  enabled: (id: string) => boolean,
): string {
  const picked = picks[key] ?? "";
  if (picked && enabled(picked)) return picked;
  return fallback && enabled(fallback) ? fallback : "";
}

export function usePageChat(key: string): {
  picks: Record<string, string>;
  pick: (id: string) => void;
} {
  const picks = useStore(picksStore);
  const pick = (id: string) => {
    const next = { ...picksStore.get(), [key]: id };
    picksStore.set(next);
    try {
      localStorage.setItem(PAGE_CHAT_KEY, JSON.stringify(next));
    } catch {
      // 寫不進去只是少了「記住」，這次還是照樣生效。
    }
  };
  return { picks, pick };
}
