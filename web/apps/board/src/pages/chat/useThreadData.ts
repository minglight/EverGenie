import { ApiError, type ChatMessage, type Session } from "@evergenie/sdk-web";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useReducer, useRef, useState } from "react";
import { threadsQuery } from "../../data/queries";
import { apiFor, PRIMARY, type SiteRef } from "../../data/sites";
import { useDocumentVisible } from "../../lib/visible";
import { matchQueued, type QueuedMessage } from "./flow";

export interface ChatState {
  messages: ChatMessage[];
  queued: QueuedMessage[];
  /** 秘書還在打、還沒寫進記錄的那段（DEV-150）；空字串＝沒有。 */
  draft: string;
}

type ChatAction =
  | { type: "reset" }
  | { type: "message"; message: ChatMessage }
  | { type: "queue"; item: QueuedMessage }
  | { type: "sent"; key: string; at: number; id?: string }
  | { type: "fail"; key: string; error: string }
  | { type: "retry"; key: string };

export function reduce(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case "reset":
      return { messages: [], queued: [], draft: "" };
    case "message": {
      const m = action.message;
      // 還在打的那段：只換掉淡色那則，不進訊息流、不動已讀游標。
      if (m.draft)
        return state.draft === (m.text ?? "") ? state : { ...state, draft: m.text ?? "" };
      // 重連後伺服器會從 since_uuid 之後接著送；已經有的 uuid 不重複加。
      if (m.uuid && state.messages.some((x) => x.uuid === m.uuid)) return state;
      const queued = m.kind === "user" ? matchQueued(state.queued, m.text ?? "") : state.queued;
      const draft = m.kind === "assistant" || m.kind === "user" ? "" : state.draft;
      return { messages: [...state.messages, m], queued, draft };
    }
    case "queue":
      return { ...state, queued: [...state.queued, action.item] };
    case "sent":
      return {
        ...state,
        queued: state.queued.map((q) =>
          q.key === action.key && q.status === "queued"
            ? { ...q, status: "sent", sentAt: action.at, id: action.id }
            : q,
        ),
      };
    case "fail":
      return {
        ...state,
        queued: state.queued.map((q) =>
          q.key === action.key ? { ...q, status: "failed", error: action.error } : q,
        ),
      };
    case "retry":
      return {
        ...state,
        queued: state.queued.map((q) =>
          q.key === action.key
            ? { ...q, status: "queued", error: undefined, sentAt: undefined }
            : q,
        ),
      };
  }
}

function errorText(err: unknown): string {
  return err instanceof ApiError ? err.message : String(err);
}

let queuedSeq = 0;

// 還有話在排隊或等她讀到時（狀態不一定會變），每幾秒補問一次（REQ-302）；其他時候全靠 session.watch 推。
export const UNFINISHED_POLL_MS = 3000;
const MESSAGES_DEBOUNCE_MS = 250;

export interface ChatData {
  messages: ChatMessage[];
  queued: QueuedMessage[];
  /** 秘書還在打、還沒寫進記錄的那段（看板先用淡色顯示；正式那則到了就收掉）。 */
  draft: string;
  /** 對話記錄的第一批已經到了（或等得夠久、確定是空的）。 */
  ready: boolean;
  send: (text: string) => void;
  resend: (key: string) => void;
}

const EMPTY_GRACE_MS = 800;

/**
 * 討論串的對話：session.chat（follow）從頭讀，斷線重連帶 since_uuid = 最後一則的 uuid；
 * 送出的話先變成淡色的泡泡（傳送中→已送出，等秘書讀到），收到內容相同的 user 訊息後換成正式的（補充契約 S4 第 4、6 點）。
 */
export function useChatData(session: string, site: SiteRef = PRIMARY): ChatData {
  const [state, dispatch] = useReducer(reduce, { messages: [], queued: [], draft: "" });
  // 這一串在哪台（S3-6）：串流與送話都打那台；base 一樣就是同一個 api 物件（apiFor 記住的），換台才重連。
  const api = apiFor(site);
  const [grace, setGrace] = useState(false);
  const last = useRef("");

  useEffect(() => {
    dispatch({ type: "reset" });
    setGrace(false);
    last.current = "";
    const abort = new AbortController();
    const timer = setTimeout(() => setGrace(true), EMPTY_GRACE_MS);
    // 重連時 transport 會重新 JSON.stringify 同一個請求物件，所以 since_uuid 用 getter 取當下最後一則。
    const req = {
      session,

      follow: true,
      get since_uuid(): string | undefined {
        return last.current || undefined;
      },
    };
    api.session
      .chat(
        req,
        (item) => {
          if (!item.message) return;
          if (item.message.uuid && !item.message.draft) last.current = item.message.uuid;
          dispatch({ type: "message", message: item.message });
        },
        abort.signal,
      )
      .catch(() => {
        // 這個討論串不存在或串流被拒絕：畫面由 session.get 的結果決定要不要顯示「找不到」。
      });
    return () => {
      abort.abort();
      clearTimeout(timer);
    };
  }, [session, api]);

  const post = useCallback(
    (key: string, text: string) => {
      api.bus
        .send({ to: session, kind: "chat", body: text })
        .then((r) => dispatch({ type: "sent", key, at: Date.now(), id: r.envelope?.id }))
        .catch((err: unknown) => {
          dispatch({ type: "fail", key, error: errorText(err) });
        });
    },
    [session, api],
  );
  const send = useCallback(
    (text: string) => {
      const key = `q-${++queuedSeq}`;
      dispatch({ type: "queue", item: { key, text, status: "queued" } });
      post(key, text);
    },
    [post],
  );
  const resend = useCallback(
    (key: string) => {
      const item = state.queued.find((q) => q.key === key);
      if (!item) return;
      dispatch({ type: "retry", key });
      post(key, item.text);
    },
    [post, state.queued],
  );

  return {
    messages: state.messages,
    queued: state.queued,
    draft: state.draft,
    ready: state.messages.length > 0 || grace,
    send,
    resend,
  };
}

/**
 * 這個 session 現在的樣子（pending 在裡面）：session.get 先抓一次，session.watch 有變化就更新；
 * 斷線重連後重抓一次。找不到這個 session 時 notFound 為 true。
 */
export function useSessionLive(
  name: string,
  site: SiteRef = PRIMARY,
): {
  session?: Session;
  notFound: boolean;
  refresh: () => void;
} {
  const client = useQueryClient();
  const api = apiFor(site);
  // 讓這台的討論串清單重抓（用 ref：effect 不必因為 client／key 物件換了而重連）。
  const refreshList = useRef(() => {});
  refreshList.current = () =>
    void client.invalidateQueries({ queryKey: threadsQuery(site).queryKey });
  const [session, setSession] = useState<Session | undefined>();
  const [notFound, setNotFound] = useState(false);
  const reload = useRef<() => void>(() => {});

  useEffect(() => {
    setSession(undefined);
    setNotFound(false);
    const abort = new AbortController();
    let soon: number | undefined;
    const load = () => {
      api.session
        .get({ name })
        .then((r) => {
          if (abort.signal.aborted) return;
          setSession(r.session);
          setNotFound(false);
        })
        .catch((err: unknown) => {
          if (!abort.signal.aborted && err instanceof ApiError && err.code === "not_found") {
            setNotFound(true);
          }
        });
    };
    // watch 推來的狀態不帶 messages（還沒結束的話，REQ-302）：先沿用舊的，稍後用 session.get 補齊。
    const loadSoon = () => {
      window.clearTimeout(soon);
      soon = window.setTimeout(load, MESSAGES_DEBOUNCE_MS);
    };
    reload.current = loadSoon;
    load();
    // 狀態靠推送（WEB-69），不每 3 秒問一次；串流斷了會自己重連，連上時（onReconnect）重抓一次。
    api.session
      .watch(
        {},
        (item) => {
          const next = item.session;
          if (next?.name === name) {
            setSession((prev) => ({ ...next, messages: prev?.messages }));
            setNotFound(false);
            loadSoon();
            // 清單（每台的）由外殼的即時更新重抓：每台能用的台各一條 watch_threads（S3-8，data/live.ts）。
          }
        },
        abort.signal,
        load,
      )
      .catch(() => {});
    return () => {
      abort.abort();
      window.clearTimeout(soon);
    };
  }, [name, api]);

  // 還有話在排隊或等她讀到時，狀態不一定會變（她一直在忙），每幾秒問一次，送失敗才看得到（REQ-302）。
  const unfinished = (session?.messages ?? []).some(
    (m) => m.lifecycle === "queued" || m.lifecycle === "notified",
  );
  useEffect(() => {
    if (!unfinished) return;
    const timer = window.setInterval(() => reload.current(), UNFINISHED_POLL_MS);
    return () => window.clearInterval(timer);
  }, [unfinished]);
  const refresh = useCallback(() => reload.current(), []);

  // 問題被回答、狀態變了，清單與徽章也要跟著（watch_threads 本來就會推，這裡讓它立刻對齊）。
  const pendingId = session?.pending?.id ?? "";
  // biome-ignore lint/correctness/useExhaustiveDependencies: pendingId 變了才重抓
  useEffect(() => {
    refreshList.current();
  }, [pendingId]);

  return { session, notFound, refresh };
}

const SEEN_DEBOUNCE_MS = 300;

/**
 * 看過了（REQ-143）：打開時、以及畫面可見時收到新的訊息，把已讀位置移到最後一則的 uuid。
 * 黃點不受影響（只有問題被回答才消失）。
 */
export function useMarkSeen(
  session: string,
  messages: readonly ChatMessage[],
  site: SiteRef = PRIMARY,
) {
  const client = useQueryClient();
  const api = apiFor(site);
  const threadsKey = threadsQuery(site).queryKey;
  const keyRef = useRef(threadsKey);
  keyRef.current = threadsKey;
  const visible = useDocumentVisible();
  const marked = useRef("");
  const lastUuid = messages[messages.length - 1]?.uuid ?? "";

  // biome-ignore lint/correctness/useExhaustiveDependencies: 換串就重來
  useEffect(() => {
    marked.current = "";
  }, [session]);

  useEffect(() => {
    if (!visible || !lastUuid || marked.current === lastUuid) return;
    const timer = setTimeout(() => {
      marked.current = lastUuid;
      api.session
        .markSeen({ session, uuid: lastUuid })
        .then(() => client.invalidateQueries({ queryKey: keyRef.current }))
        .catch(() => {
          // 下次有新訊息或回到畫面時會再試。
          marked.current = "";
        });
    }, SEEN_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [client, lastUuid, session, visible, api]);
}

/**
 * 主人打開聊天頁就先叫醒她（REQ-386、WEB-71）：她的主要 session 停了（隨選的閒置收掉、電腦重開）時送一次
 * session.open（wake），伺服器判斷能不能叫醒（停用、mode none、不是主要 session 的不動）。回傳要不要寫「正在叫醒她」：
 * 伺服器說正在開（starting），或剛送出叫醒、還沒等到伺服器的新狀態。
 */
export function useWakeOnOpen(
  name: string,
  session: Session | undefined,
  site: SiteRef = PRIMARY,
): boolean {
  const asked = useRef("");
  const api = apiFor(site);
  const [pending, setPending] = useState(false);
  const key = name;
  const stopped =
    session?.state === "stopped" && session.kind === "resident" && session.name === session.agent;
  useEffect(() => {
    if (!stopped || asked.current === key) return;
    asked.current = key;
    setPending(true);
    api.session
      .open({ name, agent: name, wake: true })
      .then((r) => {
        if (r.session?.state === "stopped") setPending(false); // 伺服器說不能叫醒
      })
      .catch(() => setPending(false));
  }, [stopped, key, name, api]);
  const state = session?.state;
  useEffect(() => {
    if (state && state !== "stopped" && state !== "starting") setPending(false);
  }, [state]);
  return state === "starting" || (pending && state !== "idle" && state !== "busy");
}
