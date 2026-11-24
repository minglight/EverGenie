import { ApiError, api, type NotifyPrefs } from "@evergenie/sdk-web";
import { type QueryClient, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { clonePrefs, type FieldKey, revertField } from "./prefs";
import { notifyPrefsQuery } from "./queries";

export type SaveState =
  | { kind: "idle" }
  | { kind: "saving" }
  | { kind: "saved" }
  /** retriable：網路或服務暫時不通（可以按「重試」）；否則是伺服器拒絕，已經還原。 */
  | { kind: "failed"; retriable: boolean };

/** 改了多久沒再動就存（FEN-22）。 */
export const AUTOSAVE_MS = 600;
/** 「已儲存」留多久再淡出。 */
export const SAVED_SHOWN_MS = 2000;

// 伺服器沒有指出是哪一格有問題時，用錯誤的 field 猜：摘要間隔、安靜時段各自對得上。
function pickTarget(sent: FieldKey[], field: string | undefined): FieldKey | undefined {
  if (field === "approve" || field === "mode" || field === "categories") {
    // 「要你同意」被拒（單一秘書另設也可能）：優先指到「要你同意」那一格。
    const approve = sent.find((k) => k === "mode:approve" || k.endsWith(":approve"));
    if (field === "approve" && approve) return approve;
    const mode =
      sent.find((k) => k.startsWith("mode:")) ?? sent.find((k) => k.startsWith("agent:"));
    if (mode) return mode;
  }
  if (field?.startsWith("tasks.")) {
    const key = `tasks:${field.slice("tasks.".length)}` as FieldKey;
    return sent.includes(key) ? key : (sent.find((k) => k.startsWith("tasks:")) ?? sent[0]);
  }
  if (field === "digest_hours") return sent.find((k) => k.startsWith("hours:")) ?? sent[0];
  if (field === "quiet_hours") return sent.find((k) => k === "quiet") ?? sent[0];
  if (field === "mode" || field === "approve" || field === "categories")
    return sent.find((k) => k.startsWith("mode:")) ?? sent[0];
  return sent[0];
}

export interface AutosavedPrefs {
  prefs: NotifyPrefs | null;
  loading: boolean;
  loadError: boolean;
  reload: () => void;
  /** 改某一格：畫面立刻變，600ms 後自動存。 */
  edit: (key: FieldKey, change: (p: NotifyPrefs) => NotifyPrefs) => void;
  /** 每一格下面的錯誤（伺服器拒絕的原因）。 */
  errors: Partial<Record<FieldKey, string>>;
  state: SaveState;
  retry: () => void;
}

// 跟著 QueryClient 走的存檔狀態（一個看板一個 client＝整個看板共用；測試各自的 client 互不影響）（NTF-65）：
// - queue：所有存檔排成一條隊，上一次還沒回來就等它回來再存最新的，不會同時送兩份；
//   離開頁面時補存的那一次也在這條隊上，回到頁面再改別格會排在它後面。
// - leftover：離開頁面時還沒存好的那份畫面上的值。補存還沒回來就回到頁面，畫面與下一次存檔以它為底，
//   不會拿快取裡的舊值整份送出、把剛補存的那格蓋回去。補存回來（成功或失敗）就清掉。
interface SaveLine {
  queue: Promise<void>;
  leftover: { prefs: NotifyPrefs; done: Promise<void> } | null;
}
const lines = new WeakMap<QueryClient, SaveLine>();
function lineOf(client: QueryClient): SaveLine {
  let line = lines.get(client);
  if (!line) {
    line = { queue: Promise.resolve(), leftover: null };
    lines.set(client, line);
  }
  return line;
}

// 設定頁沒有「儲存」按鈕：改了 600ms 後自動存，頁首顯示儲存中／已儲存／沒存成功（FEN-22）。
// 伺服器拒絕：還原那一格，原因寫在那一格下面；網路不通：改的內容留著，可以重試。
export function useAutosavedPrefs(): AutosavedPrefs {
  const queryClient = useQueryClient();
  const query = useQuery(notifyPrefsQuery);
  // 上一個設定頁離開時還在補存：先接它的值（NTF-65）。
  const line = lineOf(queryClient);
  const [carried] = useState(() => line.leftover);
  const [draft, setDraft] = useState<NotifyPrefs | null>(() => carried?.prefs ?? null);
  const [errors, setErrors] = useState<Partial<Record<FieldKey, string>>>({});
  const [state, setState] = useState<SaveState>({ kind: "idle" });

  const latest = useRef<NotifyPrefs | null>(carried?.prefs ?? null);
  const dirty = useRef<Set<FieldKey>>(new Set());
  const saveTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const savedTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // 補存回來了：這頁還沒改過就改看伺服器上的值（重抓一次，免得中間有一次舊的讀取蓋掉快取）。
  // biome-ignore lint/correctness/useExhaustiveDependencies: 只在進頁時接一次
  useEffect(() => {
    if (!carried) return;
    let live = true;
    void carried.done.then(() => {
      if (!live) return;
      // 這頁改過別格：自己的存檔會帶回伺服器上的值，不用重抓（重抓的 GET 晚回來反而會蓋掉剛存的）。
      if (latest.current !== carried.prefs) return;
      latest.current = null;
      setDraft(null);
      void queryClient.invalidateQueries({ queryKey: notifyPrefsQuery.queryKey });
    });
    return () => {
      live = false;
    };
  }, []);

  // 離開頁面（換分頁、按返回）時還在等 600ms 的那一格立刻存，不默默丟掉（NTF-64）。
  const flushRef = useRef<() => Promise<void>>(async () => {});
  useEffect(
    () => () => {
      const pending = dirty.current.size > 0;
      clearTimeout(saveTimer.current);
      clearTimeout(savedTimer.current);
      if (!pending || !latest.current) return;
      const prefs = latest.current;
      const done = flushRef.current();
      const entry = { prefs, done };
      line.leftover = entry;
      void done.then(() => {
        if (line.leftover === entry) line.leftover = null;
      });
    },
    // line 跟著 QueryClient，一個看板裡不會變；只在離開頁面時跑一次。
    [line],
  );

  const saveOnce = useCallback(async () => {
    const current = latest.current;
    if (!current || dirty.current.size === 0) return;
    const sent = [...dirty.current];
    // 沒改任務單跟進就不送 tasks：伺服器原樣保留 [tasks]，不會用畫面上舊的數字蓋掉別處的修改。
    // presets 是核心給的唯讀預設表，也不送回去。
    const { presets: _presets, ...withoutPresets } = current;
    const payload: NotifyPrefs = sent.some((k) => k.startsWith("tasks:"))
      ? withoutPresets
      : (({ tasks: _tasks, ...rest }) => rest)(withoutPresets);
    dirty.current = new Set();
    clearTimeout(savedTimer.current);
    setState({ kind: "saving" });
    try {
      const res = await api.notify.setPrefs({ prefs: payload });
      const saved = res.prefs ?? payload;
      // 還在路上的舊讀取不能晚回來蓋掉剛存的值。
      await queryClient.cancelQueries({ queryKey: notifyPrefsQuery.queryKey });
      queryClient.setQueryData(notifyPrefsQuery.queryKey, saved);
      if (latest.current === current) {
        latest.current = null;
        setDraft(null);
      }
      setState({ kind: "saved" });
      savedTimer.current = setTimeout(() => setState({ kind: "idle" }), SAVED_SHOWN_MS);
    } catch (err) {
      const apiErr = err instanceof ApiError ? err : null;
      const retriable = !apiErr || apiErr.code === "unavailable";
      if (retriable) {
        for (const k of sent) dirty.current.add(k);
        setState({ kind: "failed", retriable: true });
      } else {
        // 伺服器拒絕：把這次送出的每一格換回伺服器上的值，原因寫在對應的那一格下面。
        const server = queryClient.getQueryData<NotifyPrefs | null>(notifyPrefsQuery.queryKey);
        const target = pickTarget(sent, apiErr.field);
        if (server && latest.current) {
          let next = latest.current;
          for (const k of sent) next = revertField(next, server, k);
          latest.current = next;
          setDraft(next);
        }
        if (target) setErrors((e) => ({ ...e, [target]: apiErr.message }));
        setState({ kind: "failed", retriable: false });
      }
    }
  }, [queryClient]);

  const flush = useCallback(() => {
    clearTimeout(saveTimer.current);
    // saveOnce 自己接住錯誤；這裡保險再接一次，隊伍不會因為一次例外整條卡死。
    const next = line.queue.then(saveOnce).catch(() => {});
    line.queue = next;
    return next;
  }, [saveOnce, line]);
  flushRef.current = flush;

  const edit = useCallback(
    (key: FieldKey, change: (p: NotifyPrefs) => NotifyPrefs) => {
      const base =
        latest.current ??
        queryClient.getQueryData<NotifyPrefs | null>(notifyPrefsQuery.queryKey) ??
        null;
      if (!base) return;
      const next = change(clonePrefs(base));
      latest.current = next;
      dirty.current.add(key);
      setDraft(next);
      setErrors((e) => {
        if (!(key in e)) return e;
        const { [key]: _gone, ...rest } = e;
        return rest;
      });
      clearTimeout(saveTimer.current);
      saveTimer.current = setTimeout(() => void flush(), AUTOSAVE_MS);
    },
    [flush, queryClient],
  );

  return {
    prefs: draft ?? query.data ?? null,
    loading: query.data === undefined && !query.isError,
    loadError: query.data === undefined && query.isError,
    reload: () => void query.refetch(),
    edit,
    errors,
    state,
    retry: () => void flush(),
  };
}
