import { api, type Job } from "@evergenie/sdk-web";
import { keepPreviousData, queryOptions } from "@tanstack/react-query";
import { t } from "../../i18n/zh-TW";
import { clockText, parseIso, timeLabel } from "../../lib/format";
import type { StatusTone } from "../../lib/statusTone";

// 排程（任務單 → 排程，REQ-18）：已封存的不列；秘書沒有的（系統級）排最後一組。

export const jobsQuery = queryOptions({
  queryKey: ["jobs"],
  queryFn: async () => (await api.scheduler.listJobs({})).jobs ?? [],
  placeholderData: keepPreviousData,
  staleTime: 10_000,
  refetchInterval: 30_000,
});

export interface JobGroup {
  /** 秘書的職位 id；系統級是空字串。 */
  agent: string;
  jobs: Job[];
}

/** 依秘書分組：秘書照名冊順序（沒在名冊的接在後面），系統級最後；組內照名稱排（逐字比大小）。 */
export function groupJobs(jobs: readonly Job[], roster: readonly string[]): JobGroup[] {
  const by = new Map<string, Job[]>();
  for (const j of jobs) {
    if (j.archived) continue;
    const key = j.agent ?? "";
    by.set(key, [...(by.get(key) ?? []), j]);
  }
  const known = roster.filter((id) => by.has(id));
  const others = [...by.keys()].filter((id) => id !== "" && !roster.includes(id)).sort();
  return [...known, ...others, ...(by.has("") ? [""] : [])].map((agent) => ({
    agent,
    jobs: [...(by.get(agent) ?? [])].sort((a, b) => {
      const x = a.name || a.id || "";
      const y = b.name || b.id || "";
      return x < y ? -1 : x > y ? 1 : 0; // 逐字比大小，各瀏覽器與語系結果一樣
    }),
  }));
}

/** 新排程的 id（職位 id／代號）：名稱裡的英數字轉成小寫、用減號連起來；沒有英數字（例如純中文）就用時間戳。 */
export function newJobId(agent: string, name: string, at: number): string {
  const slug = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/g, "");
  return `${agent}/${slug || `job-${at}`}`;
}

/** 排程 id 的後半（顯示或比對用）。 */
export const localId = (id: string | undefined) => (id ?? "").split("/").slice(1).join("/");

export type JobSourceKind = "user" | "migrate" | "builtin";

export function sourceKind(source: string | undefined): JobSourceKind {
  if (source === "user") return "user";
  if (source === "migrate") return "migrate";
  return "builtin";
}

/**
 * 最近一次的顏色照狀態五色（UXC-12、`lib/statusTone.ts`）：還在做＝藍（busy）、沒跑成＝紅（trouble）；
 * 跑過與略過是灰（idle，灰字一定配文字）——跑過是排程的平常樣子，不用綠（綠是「有新的」）；略過不是等你處理，不用橘。
 */
export interface LastRunView {
  text: string;
  tone: StatusTone;
}

/**
 * 排程最近一次的那一行（SCH-16）：「今天 08:00 跑過」「上次 11/1 08:00 跑過」「今天 08:00 沒跑成：<原因>」；
 * 今天跑了兩次以上，後面加「· 共 N 次」（今天有跑，最近一次就是今天，所以不再寫一次「今天」）。
 * 「今天」照瀏覽器的時區判斷、`runs_today` 照家的時區算；主人的手機與家同時區，不同時區時兩者可能差一天（已知限制）。從沒跑過（或伺服器還沒帶這個欄位）回 null，不顯示。
 * 系統級排程（fired）只知道觸發了，跟秘書回覆了（ok）一樣寫「跑過」。
 */
export function lastRunView(job: Job, now: Date): LastRunView | null {
  const at = parseIso(job.last_run);
  if (!at || !job.last_result) return null;
  const today = at.toDateString() === now.toDateString();
  const when = today
    ? t("jobs.last.today", { at: clockText(job.last_run) })
    : t("jobs.last.before", { at: timeLabel(job.last_run, now) });
  const reason = job.last_reason ?? "";
  let view: LastRunView;
  switch (job.last_result) {
    case "running":
      view = { text: t("jobs.last.running", { when }), tone: "busy" };
      break;
    case "failed":
      view = {
        text: reason
          ? t("jobs.last.failed", { when, reason })
          : t("jobs.last.failedNoReason", { when }),
        tone: "trouble",
      };
      break;
    case "skipped":
      view = {
        text: reason
          ? t("jobs.last.skipped", { when, reason })
          : t("jobs.last.skippedNoReason", { when }),
        tone: "idle",
      };
      break;
    default:
      view = { text: t("jobs.last.ok", { when }), tone: "idle" };
  }
  const n = job.runs_today ?? 0;
  if (n > 1) view.text += ` · ${t("jobs.last.count", { n })}`;
  return view;
}
