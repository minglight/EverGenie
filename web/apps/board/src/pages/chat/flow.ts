import type { ChatMessage, MessageWait, SessionMessage } from "@evergenie/sdk-web";
import { parseIso, sameDay } from "../../lib/format";

/** 畫面上的一個項目：使用者的話、秘書的話、收起來的一串工具、不是主人說的話（秘書那邊的一小排）、置中的小膠囊。 */
export type FlowItem =
  | { type: "user"; key: string; text: string; ts?: string }
  | { type: "aside"; key: string; label: string; detail?: string; from?: string; ts?: string }
  | { type: "assistant"; key: string; text: string; ts?: string }
  | { type: "tools"; key: string; count: number; tools: ToolUse[]; ts?: string }
  | { type: "note"; key: string; text: string }
  | { type: "time"; key: string; at: string };

/** 收起來的工具串裡的一種：白話名稱（引擎給的 `tool_label`，舊資料沒有就退回原名）與用了幾次（WEB-125、WEB-126）。 */
export interface ToolUse {
  label: string;
  count: number;
}

/** 工具呼叫給人看的名字：引擎翻好的白話；舊資料或引擎說不出時用原名（看板不自己翻，SES-63）。 */
export function toolLabel(m: Pick<ChatMessage, "tool" | "tool_label">): string {
  return m.tool_label?.trim() || m.tool || "?";
}

/** 只在這次畫面裡的狀態小膠囊（「已允許：…」）；排在 anchor 那則訊息之後，沒有 anchor 就排最前面。 */
export interface LocalNote {
  id: string;
  anchor: string;
  text: string;
}

export interface QueuedMessage {
  key: string;
  text: string;
  /**
   * queued＝還在送；sent＝看板送出去了（`sentAt`），等秘書讀到；failed＝沒送到（可重送）；
   * interrupted＝她收到了、這一輪沒做完（引擎回報的，`error` 是白話原因；她可能做了一半，不給重送，只給「再說一次」，S5）。
   */
  status: "queued" | "sent" | "failed" | "interrupted";
  error?: string;
  /** interrupted 時平台說的原話（多半是英文）：收在「詳細」裡，不直接放畫面（DEV-156）。 */
  detail?: string;
  /** interrupted 是哪一種錯（伺服器給的 `reason_kind`）：auth、billing 要主人去處理，不給「再說一次」。 */
  kind?: string;
  sentAt?: number;
  /** 伺服器上的封包 id（送出後才有）。 */
  id?: string;
  /** 秘書在忙的時候中途送進去的（SES-41）：她做完手上那一步才會看到。 */
  interject?: boolean;
  /** 是伺服器帶回來的（退出再進來時畫回的），不是這次畫面送的；重送就當新的一句送（REQ-302）。 */
  fromServer?: boolean;
  /** 這一輪沒做完的那句已經在對話記錄裡（她收到了）：只畫紅字，不再畫一次泡泡（WEB-119）。 */
  inLog?: boolean;
  /**
   * 還沒收到的原因（伺服器給的，送出後 3 秒規則，REQ-384）：answer＝等你回答上面的問題、waking＝正在叫醒她、
   * busy＝在收前一句、interject＝中途送進去了、retrying＝上一次沒貼進去、正在重試。說不出原因就沒有，看板不自己猜；
   * 不認得的（含舊版那台送來的 late）照「已送出」灰字。
   */
  wait?: MessageWait;
  /** wait 是 limited 時，她的額度什麼時候恢復（伺服器給的，REQ-378）。 */
  waitUntil?: string;
  /** 伺服器記的送出時間（`session.messages[].created_at`），給 `dropReceived` 比對用。 */
  createdAt?: string;
}

/**
 * 把這次畫面送的（local）和伺服器記得的（session.messages）合成要畫的淡色泡泡：
 * - 這次送的照舊，伺服器說送失敗或是中途補話的就跟著改；
 * - 這次畫面沒有、伺服器還在排隊／等她讀到／送失敗的主人的話（退出再進來的情況），補成泡泡；
 * - 她已經收到的（received 之後）會出現在對話記錄裡，不畫。
 */
export function mergePending(
  local: readonly QueuedMessage[],
  server: readonly SessionMessage[] | undefined,
): QueuedMessage[] {
  const byId = new Map((server ?? []).map((m) => [m.id ?? "", m]));
  const out = local.map((q) => {
    const m = q.id ? byId.get(q.id) : undefined;
    if (!m) return q;
    if (m.lifecycle === "failed")
      return { ...q, status: "failed" as const, error: m.reason, createdAt: m.created_at };
    if (m.lifecycle === "interrupted")
      return {
        ...q,
        status: "interrupted" as const,
        error: m.reason,
        detail: m.reason_detail,
        kind: m.reason_kind,
        createdAt: m.created_at,
      };
    return {
      ...q,
      interject: q.interject || m.interject || undefined,
      wait: m.wait,
      waitUntil: m.wait_until,
    };
  });
  const known = new Set(local.map((q) => q.id).filter(Boolean));
  for (const m of server ?? []) {
    if (!m.id || known.has(m.id) || !(m.from ?? "").startsWith("owner(")) continue;
    if (
      m.lifecycle !== "queued" &&
      m.lifecycle !== "notified" &&
      m.lifecycle !== "failed" &&
      m.lifecycle !== "interrupted"
    )
      continue;
    out.push({
      key: `s-${m.id}`,
      id: m.id,
      text: m.body ?? "",
      status:
        m.lifecycle === "interrupted"
          ? "interrupted"
          : m.lifecycle === "failed"
            ? "failed"
            : "sent",
      error: m.reason,
      detail: m.lifecycle === "interrupted" ? m.reason_detail : undefined,
      kind: m.lifecycle === "interrupted" ? m.reason_kind : undefined,
      interject: m.interject,
      wait: m.wait,
      waitUntil: m.wait_until,
      fromServer: true,
      createdAt: m.created_at,
    });
  }
  return out;
}

/**
 * 「沒送到」其實她做了（WEB-90）：叫她停下時送進去的那句，核心先記「這句還沒開始，可以重送」，平台卻接著把它當下一輪做了；
 * 這時對話記錄裡有同一句、紅字還掛著，看起來畫了兩次又說沒送到。送失敗的泡泡在對話記錄裡有一則送出之後的、
 * 內容相同（trim 後）的主人的話，就不畫（跟核心 `resent` 一樣只比內容：同一句之後又出現就算送到了）。
 * 不知道送出時間的（這次畫面送、還沒拿到伺服器的時間）用 `sentAt`；兩個都沒有就照舊畫。
 * 「這一輪沒做完」（WEB-67）照舊留著，但對話記錄裡已經有那句時標 `inLog`：泡泡不再畫一次文字，只留紅字（WEB-119）。
 */
export function dropReceived(
  pending: readonly QueuedMessage[],
  messages: readonly ChatMessage[],
): QueuedMessage[] {
  const inLog = (q: QueuedMessage) => {
    const since = q.createdAt ? Date.parse(q.createdAt) : (q.sentAt ?? Number.NaN);
    if (Number.isNaN(since)) return false;
    const text = q.text.trim();
    return messages.some(
      (m) =>
        m.kind === "user" &&
        !m.from_session &&
        (m.text ?? "").trim() === text &&
        Date.parse(m.ts ?? "") >= since,
    );
  };
  const out: QueuedMessage[] = [];
  for (const q of pending) {
    if (q.status === "failed") {
      if (!inLog(q)) out.push(q);
    } else if (q.status === "interrupted") {
      // WEB-119：她收到了、對話記錄裡已經有那句，只留紅字＋「再說一次」，不再畫第二個一樣的泡泡。
      out.push(inLog(q) ? { ...q, inLog: true } : q);
    } else out.push(q);
  }
  return out;
}

/** 排程送進對話的那一句的開頭（核心 `job/events.go` 寫的「［排程 <id>］」）。 */
const ROUTINE_RE = /^［排程 ([^］]+)］/;

/**
 * 排程送給秘書的整段指令不畫成主人的泡泡（作者 2026-11-04：「你有辦法不要 show 這個東西嗎」）：
 * 只回一行小膠囊的文字——指令第一行是 `# 標題` 就用標題，不然用排程代號。不是排程的回 null。
 */
export function routineLabel(text: string): string | null {
  const m = ROUTINE_RE.exec(text.trimStart());
  if (!m) return null;
  const first = text.trimStart().slice(m[0].length).trimStart().split("\n", 1)[0] ?? "";
  const heading = /^#+\s+(.+)$/.exec(first.trim());
  return heading?.[1]?.trim() || (m[1] ?? "");
}

/**
 * 核心送進對話時加的外框：派送通知、核心自己發的、別的秘書傳的（`session/dispatch.go` 的 `pastedText`；舊對話多一個「傳話」）、
 * 待辦查證（`reminder/events.go`）、單接回（`session/work.go`）、開 Goal（`goal/service.go`）、新家第一位秘書的招呼（`agents/roster/firstday.go`）。職位 id 只有小寫英數，
 * 主人自己打的［備註（明天）］不會對到。
 */
const CORE_FRAME_RE =
  /^［(派送通知|核心(?:傳話)?|[^］（]+（[a-z0-9-]+）(?:回覆你|傳話)?|待辦 R-\d+|單 [^］]*回到進行中[^］]*|[A-Z]+-\d+(?=］主人開了一個 Goal))］/;

/** 手機一小排放得下的字數（360 寬約 19 個中文字，扣掉前綴）；超過的就能點開看全文。 */
const ASIDE_FITS = 14;

/** 圖片在一小排只寫「（圖片）」，點開才看圖。 */
const MD_IMAGE_RE = /!\[[^\]]*\]\([^)]*\)/g;

function asideText(who: string | null, body: string): { label: string; detail?: string } {
  const trimmed = body.trim();
  const first = trimmed.split("\n", 1)[0] ?? "";
  const shown = first.replace(MD_IMAGE_RE, "（圖片）");
  const label = who ? (shown ? `${who}：${shown}` : who) : shown;
  return first.length < trimmed.length || first.length > ASIDE_FITS
    ? { label, detail: trimmed }
    : { label };
}

/**
 * 核心轉來的話不是主人說的（作者 2026-11-04：「也不要用我的嘴巴說」）：回一小排要顯示的字與全文；不是的回 null。
 * 主人回答問題的外框（［主人回答 …］）是主人自己的話，不算。
 */
export function coreAside(text: string): { label: string; detail?: string } | null {
  const trimmed = text.trimStart();
  const m = CORE_FRAME_RE.exec(trimmed);
  if (!m) return null;
  const who = (m[1] ?? "").replace(/（[a-z0-9-]+）(?:回覆你|傳話)?$/, "").replace(/傳話$/, "");
  return asideText(who, trimmed.slice(m[0].length));
}

/**
 * 把引擎的對話記錄變成畫面項目：
 * - 每個項目帶訊息的時間 `ts`（工具串取第一個 tool_call 的）；
 * - tool_result 不顯示（不顯示工具的輸出），而且不會把前後的 tool_call 隔開；
 * - 連續的 tool_call 收成一個項目，只留工具的白話名稱；同一種合併成一列記次數，順序照第一次出現（WEB-125）；
 * - interrupt 變成「（被中斷）」膠囊；
 * - 不是主人說的話都放秘書那邊、縮成一小排（WEB-158）：平台的通知（kind notice）、排程指令（「排程：<標題>」，WEB-156）、
 *   核心轉來的話與別的 session 傳的（點開看全文）。
 */
export function buildFlow(
  messages: readonly ChatMessage[],
  notes: readonly LocalNote[],
  interrupted: string,
  routine: (label: string) => string = (label) => label,
): FlowItem[] {
  const out: FlowItem[] = [];
  let tools: Extract<FlowItem, { type: "tools" }> | null = null;
  const emitNotes = (anchor: string) => {
    for (const n of notes) {
      if (n.anchor === anchor) out.push({ type: "note", key: `note-${n.id}`, text: n.text });
    }
  };
  emitNotes("");
  for (const m of messages) {
    const uuid = m.uuid ?? "";
    switch (m.kind) {
      case "tool_call":
        if (!tools) {
          tools = { type: "tools", key: `tools-${uuid}`, count: 0, tools: [], ts: m.ts };
          out.push(tools);
        }
        {
          const label = toolLabel(m);
          const same = tools.tools.find((u) => u.label === label);
          if (same) same.count++;
          else tools.tools.push({ label, count: 1 });
          tools.count++;
        }
        break;
      case "tool_result":
        break;
      case "user": {
        tools = null;
        const text = m.text ?? "";
        if (m.from_session) {
          out.push({
            type: "aside",
            key: uuid,
            from: m.from_session,
            ...asideText(null, text),
            ts: m.ts,
          });
          break;
        }
        const label = routineLabel(text);
        if (label !== null) {
          out.push({ type: "aside", key: uuid, label: routine(label), ts: m.ts });
          break;
        }
        const aside = coreAside(text);
        if (aside) {
          out.push({ type: "aside", key: uuid, ...aside, ts: m.ts });
          break;
        }
        out.push({ type: "user", key: uuid, text, ts: m.ts });
        break;
      }
      case "notice":
        tools = null;
        out.push({ type: "aside", key: uuid, ...asideText(null, m.text ?? ""), ts: m.ts });
        break;
      case "assistant":
        tools = null;
        out.push({ type: "assistant", key: uuid, text: m.text ?? "", ts: m.ts });
        break;
      case "interrupt":
        tools = null;
        out.push({ type: "note", key: `interrupt-${uuid}`, text: interrupted });
        break;
      default:
        break;
    }
    if (uuid) emitNotes(uuid);
  }
  return out;
}

/** 兩則訊息相隔這麼久（含）就標一次時間。 */
export const TIME_MARK_GAP_MS = 10 * 60_000;

/** 平台載入技能的那一小排（ECL-59）；預設模式跟工具膠囊一樣不露。 */
const SKILL_NOTICE = "載入技能 ";

/** 預設模式拿掉工具字眼（WEB-160）；要在 withTimeMarks、avatarKeys 之前做，時間標記與頭像才算得對。 */
export function withoutTools(flow: readonly FlowItem[]): FlowItem[] {
  return flow.filter(
    (item) =>
      item.type !== "tools" &&
      !(item.type === "aside" && !item.from && item.label.startsWith(SKILL_NOTICE)),
  );
}

/**
 * 在訊息流裡加置中的時間標記（REQ-157）：第一個有 ts 的項目前面、比到目前為止最晚的時間晚 ≥ 10 分鐘、跨日
 * （瀏覽器時區）。時間往回跳的項目（補送的、別台時鐘不準）不加標記、也不把基準拉回去，免得下面出現比上面早的時間。沒有 ts 的項目（本地膠囊、排隊中的訊息）不加也不算。標記存原始時間，顯示的字由
 * `timeMark(at, now)` 在畫面上算，所以 `now` 這裡不需要（保留參數是契約的簽名）。
 */
export function withTimeMarks(flow: readonly FlowItem[], _now?: Date): FlowItem[] {
  const out: FlowItem[] = [];
  let prev: Date | null = null;
  for (const item of flow) {
    const ts =
      item.type === "user" ||
      item.type === "assistant" ||
      item.type === "tools" ||
      item.type === "aside"
        ? item.ts
        : undefined;
    const at = parseIso(ts);
    if (at && ts && (prev === null || at.getTime() >= prev.getTime())) {
      const dayChanged = prev !== null && !sameDay(prev, at);
      const gap = prev !== null && at.getTime() - prev.getTime() >= TIME_MARK_GAP_MS;
      if (prev === null || dayChanged || gap) {
        out.push({ type: "time", key: `time-${item.key}`, at: ts });
      }
      prev = at;
    }
    out.push(item);
  }
  return out;
}

/**
 * 排隊中的泡泡對應：派送器對 chat 是原樣貼上 body，所以「內容相同」用 trim() 後完全相等比對；
 * 同一段文字送兩次時，依序對到第一個還沒對上的泡泡。回傳還沒對上的泡泡。
 */
export function matchQueued(queued: readonly QueuedMessage[], userText: string): QueuedMessage[] {
  const at = queued.findIndex(
    (q) => q.status !== "failed" && q.status !== "interrupted" && q.text.trim() === userText.trim(),
  );
  if (at < 0) return queued as QueuedMessage[];
  return queued.filter((_, i) => i !== at);
}

/**
 * 秘書連續的訊息（文字與工具膠囊）只在最後一則左邊放頭像，其他留同寬空白（REQ-156）。
 * 回傳要放頭像的項目 key。
 */
export function avatarKeys(flow: readonly FlowItem[]): Set<string> {
  const isAgent = (item: FlowItem | undefined) =>
    item?.type === "assistant" || item?.type === "tools" || item?.type === "aside";
  const out = new Set<string>();
  flow.forEach((item, i) => {
    if (isAgent(item) && !isAgent(flow[i + 1])) out.add(item.key);
  });
  return out;
}
