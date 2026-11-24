import type { ChangeEntry } from "@evergenie/sdk-web";
import { parseIso } from "../../lib/format";

// 設定→紀錄（REQ-111）的純邏輯：op、種類、誰的白話，一筆紀錄指向哪個詳情頁，依日期分組。
// 「編號旁邊要有白話」（UXC-2）：種類一律先寫白話名，再接編號。

/** 變更紀錄的 op 白話。create 是新增；put 是整筆更新（P6-6 之前的紀錄新增也寫 put，所以寫「更新」）。 */
const ops: Record<string, string> = {
  create: "新增",
  put: "更新",
  patch: "修改",
  append: "追加",
  archive: "封存",
  move: "搬移",
  "import-archived": "匯入封存",
};

export function opText(op: string | undefined): string {
  return ops[op ?? ""] ?? op ?? "";
}

const kinds: Record<string, string> = {
  task: "任務單",
  reminder: "待辦",
  goal: "Goal",
  policy_request: "權限申請",
  message: "訊息",
  habit: "習慣",
  agent: "秘書",
  addendum: "附加規則",
  memory: "記憶",
  profile: "個人檔案",
  journal: "日誌",
  note: "筆記",
  changelog: "更新紀錄",
  job: "排程",
};

/** 篩選下拉選單用的種類（套件的資料種類名稱含 `.`，各套件自己命名，不列在這裡）。 */
export const filterKinds = [
  "task",
  "reminder",
  "goal",
  "policy_request",
  "message",
  "agent",
  "job",
  "note",
] as const;

export function kindText(kind: string | undefined, id?: string): string {
  const k = kind ?? "";
  if (k === "note" && id === "system/night-queue") return "夜班清單";
  if (k.includes(".")) return `套件資料（${k}）`;
  return kinds[k] ?? k;
}

/** 誰：owner(…) 是主人；名冊上的職位 id 顯示她的名字；其他（核心自己做的）是系統。 */
export function whoText(
  by: string | undefined,
  nameOf: (id: string) => string | undefined,
): string {
  const b = by ?? "";
  if (b.startsWith("owner(")) return "主人";
  return nameOf(b) ?? "系統";
}

export type LogTarget =
  | { to: "/tasks/$id" | "/reminders/$id" | "/messages/$id"; id: string }
  | { to: "/policy" | "/settings/agents" | "/settings/machine" };

/** 這一筆能連到的詳情頁；沒有詳情頁的種類（例如 Goal 還沒有獨立頁）回 null。 */
export function targetOf(kind: string | undefined, id: string | undefined): LogTarget | null {
  const key = id ?? "";
  switch (kind) {
    case "task":
      return { to: "/tasks/$id", id: key };
    case "reminder":
      return { to: "/reminders/$id", id: key };
    case "message":
      return { to: "/messages/$id", id: key };
    case "policy_request":
      return { to: "/policy" };
    case "agent":
      return { to: "/settings/agents" };
    case "note":
      return key === "system/night-queue" ? { to: "/settings/machine" } : null;
    default:
      return null;
  }
}

export interface LogDay {
  /** 當地日期 YYYY-MM-DD。 */
  day: string;
  at: Date;
  items: ChangeEntry[];
}

const pad = (n: number) => String(n).padStart(2, "0");

/** 依當地日期分組（輸入已是新到舊，組與組內都維持這個順序）；時間讀不出來的略過。 */
export function groupByDay(items: ChangeEntry[]): LogDay[] {
  const out: LogDay[] = [];
  for (const it of items) {
    const at = parseIso(it.at);
    if (!at) continue;
    const day = `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())}`;
    const last = out[out.length - 1];
    if (last?.day === day) last.items.push(it);
    else out.push({ day, at, items: [it] });
  }
  return out;
}
