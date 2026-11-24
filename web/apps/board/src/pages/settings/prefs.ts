import type { CategoryPref, DeliveryMode, NotifyPrefs, TaskFollowPrefs } from "@evergenie/sdk-web";

// 通知偏好的純邏輯（設定頁的「通知」分頁）：欄位怎麼改、改壞了怎麼還原、安靜時段怎麼拆合。
// 業務規則（例如「要你同意」不能關）由伺服器把關（FEN-N3）；這裡只是不把做不到的選項擺出來。

export const categoryIds = ["approve", "done", "due", "digest", "opening"] as const;
export type CategoryId = (typeof categoryIds)[number];

/** 預設值：伺服器沒給的時候畫面上顯示什麼（跟 notify-prefs 的預設表一致）。 */
export const DEFAULT_DIGEST_HOURS = 3;

/** 一個欄位的識別：自動儲存失敗時，用它找出要還原哪一格、錯誤寫在哪一格下面。 */
export type FieldKey =
  | `mode:${CategoryId}`
  | `hours:${CategoryId}`
  | "quiet"
  | "preset"
  | `agent:${string}:${CategoryId}`
  | `tasks:${TaskField}`;

/** 任務單跟進（notify-prefs 的 [tasks]，D-22）的四格。 */
export const taskFields = ["nudge_at", "stale_days", "nudge_every_days", "nudge_max"] as const;
export type TaskField = (typeof taskFields)[number];

/** 伺服器沒給的時候畫面上顯示什麼（跟 notify-prefs 的預設一致）。 */
export const DEFAULT_TASKS: Required<TaskFollowPrefs> = {
  nudge_at: "09:00",
  stale_days: 3,
  nudge_every_days: 3,
  nudge_max: 3,
};

/** 數字欄位的範圍（跟伺服器的檢查一致；最後仍以伺服器為準）。 */
export const taskRanges = {
  stale_days: { min: 1, max: 30 },
  nudge_every_days: { min: 1, max: 30 },
  nudge_max: { min: 0, max: 10 },
} as const;

const allModes: DeliveryMode[] = ["instant", "digest", "board", "off"];

/** 「要你同意」最低只能是「只放看板」（REQ-26），所以沒有「關閉」。 */
export function modesFor(category: CategoryId): DeliveryMode[] {
  return category === "approve" ? allModes.filter((m) => m !== "off") : allModes;
}

export function clonePrefs(p: NotifyPrefs): NotifyPrefs {
  return structuredClone(p);
}

export function modeOf(p: NotifyPrefs, category: CategoryId): DeliveryMode {
  return p.categories?.[category]?.mode ?? "board";
}

export function hoursOf(p: NotifyPrefs, category: CategoryId): number {
  const h = p.categories?.[category]?.digest_hours ?? 0;
  return h > 0 ? h : DEFAULT_DIGEST_HOURS;
}

function pref(p: NotifyPrefs, category: CategoryId) {
  const categories = { ...p.categories };
  const current = categories[category] ?? { digest_hours: DEFAULT_DIGEST_HOURS };
  const next = { ...current, mode: current.mode ?? ("board" as DeliveryMode) };
  categories[category] = next;
  return { categories, next };
}

export function withMode(p: NotifyPrefs, category: CategoryId, mode: DeliveryMode): NotifyPrefs {
  const { categories, next } = pref(p, category);
  next.mode = mode;
  // 切到「合併摘要」時間隔一定要大於零，否則伺服器會拒絕。
  if (mode === "digest" && !(next.digest_hours > 0)) next.digest_hours = DEFAULT_DIGEST_HOURS;
  return { ...p, categories };
}

export function withHours(p: NotifyPrefs, category: CategoryId, hours: number): NotifyPrefs {
  const { categories, next } = pref(p, category);
  next.digest_hours = hours;
  return { ...p, categories };
}

export function withQuiet(p: NotifyPrefs, quiet: string): NotifyPrefs {
  return { ...p, quiet_hours: quiet };
}

/** 把 draft 裡的某一格換回伺服器上的值（伺服器拒絕時還原那一格）。 */
export function revertField(draft: NotifyPrefs, server: NotifyPrefs, key: FieldKey): NotifyPrefs {
  if (key === "quiet") return withQuiet(draft, server.quiet_hours ?? "");
  if (key === "preset") {
    return {
      ...draft,
      preset: server.preset,
      categories: structuredClone(server.categories ?? {}),
      agents: structuredClone(server.agents ?? []),
    };
  }
  if (key.startsWith("agent:")) {
    const [, agent = "", category] = key.split(":") as ["agent", string, CategoryId];
    return withAgentMode(draft, agent, category, agentModeOf(server, agent, category));
  }
  if (key.startsWith("tasks:")) {
    const field = key.slice("tasks:".length) as TaskField;
    return withTask(draft, field, tasksOf(server)[field]);
  }
  const [kind, category] = key.split(":") as ["mode" | "hours", CategoryId];
  if (kind === "mode") {
    const next = withMode(draft, category, modeOf(server, category));
    return withHours(next, category, hoursOf(draft, category));
  }
  return withHours(draft, category, hoursOf(server, category));
}

/** 「HH:MM-HH:MM」拆成兩半；沒設或格式不對都是兩個空字串。 */
export function quietParts(quiet: string | undefined): { from: string; to: string } {
  const m = /^(\d{2}:\d{2})-(\d{2}:\d{2})$/.exec(quiet ?? "");
  return m ? { from: m[1] ?? "", to: m[2] ?? "" } : { from: "", to: "" };
}

/** 兩邊都填 → "HH:MM-HH:MM"；兩邊都空 → ""；只填一邊 → null（還不能存）。 */
export function joinQuiet(from: string, to: string): string | null {
  if (from === "" && to === "") return "";
  if (from === "" || to === "") return null;
  return `${from}-${to}`;
}

/** 摘要間隔：1 以上的整數；其他回 null。 */
export function parseHours(text: string): number | null {
  const s = text.trim();
  if (!/^\d{1,3}$/.test(s)) return null;
  const n = Number(s);
  return n >= 1 ? n : null;
}

export function tasksOf(p: NotifyPrefs): Required<TaskFollowPrefs> {
  return { ...DEFAULT_TASKS, ...p.tasks, nudge_at: p.tasks?.nudge_at || DEFAULT_TASKS.nudge_at };
}

export function withTask(p: NotifyPrefs, field: TaskField, value: string | number): NotifyPrefs {
  return { ...p, tasks: { ...tasksOf(p), [field]: value } };
}

/** 任務單跟進的數字：範圍內的整數；其他回 null。 */
export function parseTaskNumber(
  field: Exclude<TaskField, "nudge_at">,
  text: string,
): number | null {
  const s = text.trim();
  if (!/^\d{1,2}$/.test(s)) return null;
  const n = Number(s);
  const { min, max } = taskRanges[field];
  return n >= min && n <= max ? n : null;
}

/** 檢查時間：HH:MM。 */
export function isClock(text: string): boolean {
  return /^([01]\d|2[0-3]):[0-5]\d$/.test(text);
}

// ---- 簡單／完整模式（P6-6、REQ-25）----

export const presetIds = ["quiet", "standard", "active"] as const;
export type PresetId = (typeof presetIds)[number];

/** 預設組某一類的送法（核心給的預設表；沒有就是 undefined）。 */
export function presetPref(p: NotifyPrefs, preset: string, category: CategoryId) {
  return p.presets?.[preset]?.[category];
}

function samePref(a: CategoryPref | undefined, b: CategoryPref | undefined): boolean {
  if (!a || !b) return a === b;
  return a.mode === b.mode && (a.mode !== "digest" || a.digest_hours === b.digest_hours);
}

/** 單一秘書另設的分類總數。 */
export function agentOverrideTotal(p: NotifyPrefs): number {
  return (p.agents ?? []).reduce((n, a) => n + Object.keys(a.categories ?? {}).length, 0);
}

/**
 * 「自己調過」的項目數：跟目前預設組不一樣的分類，加上單一秘書另設的分類。
 * 簡單模式用它提醒「切到完整模式才看得到」，選預設組前要確認「會清掉 N 項」。
 * 預設表讀不到（舊伺服器）就只算單一秘書另設，不亂報。
 */
export function overrideCount(p: NotifyPrefs): number {
  const preset = p.preset ?? "standard";
  const changed = categoryIds.filter(
    (c) =>
      p.presets?.[preset] !== undefined && !samePref(p.categories?.[c], presetPref(p, preset, c)),
  ).length;
  return changed + agentOverrideTotal(p);
}

/** 選一個預設組：只改 preset，逐類設定換成預設表、單一秘書另設清空（伺服器也只存跟預設不一樣的，所以檔案裡不會有 [categories.*]）。 */
export function withPreset(p: NotifyPrefs, preset: string): NotifyPrefs {
  const row = p.presets?.[preset];
  const categories: Record<string, CategoryPref> = {};
  for (const c of categoryIds) {
    const base = row?.[c];
    categories[c] = base ? { ...base } : { mode: "board", digest_hours: DEFAULT_DIGEST_HOURS };
  }
  return { ...p, preset, categories, agents: [] };
}

/** 預設組每一類的白話：「即時」「合併摘要（每 3 小時）」…；文字由呼叫者的字典決定。 */
export function presetRows(
  p: NotifyPrefs,
  preset: string,
): { category: CategoryId; mode: DeliveryMode; hours: number }[] {
  return categoryIds.map((category) => {
    const base = presetPref(p, preset, category);
    return {
      category,
      mode: base?.mode ?? "board",
      hours: base?.digest_hours && base.digest_hours > 0 ? base.digest_hours : DEFAULT_DIGEST_HOURS,
    };
  });
}

/** 單一秘書另設的選項：「跟全域一樣」加四種送法（「要你同意」最低只能到「只放看板」，REQ-26）。 */
export type AgentMode = DeliveryMode | "same";
export function agentModesFor(category: CategoryId): AgentMode[] {
  return ["same", ...modesFor(category)];
}

export function agentModeOf(p: NotifyPrefs, agent: string, category: CategoryId): AgentMode {
  const found = (p.agents ?? []).find((a) => a.agent === agent);
  return found?.categories?.[category]?.mode ?? "same";
}

export function agentOverrideCount(p: NotifyPrefs, agent: string): number {
  const found = (p.agents ?? []).find((a) => a.agent === agent);
  return Object.keys(found?.categories ?? {}).length;
}

/** 改某位秘書某一類的送法；"same" 就是拿掉那一項，一項都不剩就整位拿掉。 */
export function withAgentMode(
  p: NotifyPrefs,
  agent: string,
  category: CategoryId,
  mode: AgentMode,
): NotifyPrefs {
  const others = (p.agents ?? []).filter((a) => a.agent !== agent);
  const current = (p.agents ?? []).find((a) => a.agent === agent);
  const categories = { ...current?.categories };
  if (mode === "same") {
    delete categories[category];
  } else {
    categories[category] = {
      mode,
      // 合併摘要的間隔跟全域那一類一樣（沒有就預設）；伺服器要求摘要間隔大於零。
      digest_hours: hoursOf(p, category),
    };
  }
  const agents = Object.keys(categories).length > 0 ? [...others, { agent, categories }] : others;
  agents.sort((a, b) => (a.agent ?? "").localeCompare(b.agent ?? ""));
  return { ...p, agents };
}
