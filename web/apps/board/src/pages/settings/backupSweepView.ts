import type { BackupSweepItem, BackupSweepReport } from "@evergenie/sdk-web";

// 備份掃描報告（DEV-163）畫面用的純函式：把伺服器分好的結果分成幾組，前端不自己判斷到期或在用（FEN-N3）。

export type SweepGroupKey = "deletable" | "inUse" | "unchecked" | "misnamed" | "skipped";

export interface SweepGroup {
  key: SweepGroupKey;
  items: BackupSweepItem[];
}

/** 報告分成五組（順序就是畫面的順序）；空的組也回，畫面決定要不要畫。 */
export function sweepGroups(rep: BackupSweepReport | undefined): SweepGroup[] {
  const expired = rep?.expired ?? [];
  return [
    { key: "deletable", items: expired.filter((it) => it.state === "expired") },
    { key: "inUse", items: expired.filter((it) => it.state === "in_use") },
    {
      key: "unchecked",
      items: expired.filter((it) => it.state !== "expired" && it.state !== "in_use"),
    },
    { key: "misnamed", items: rep?.misnamed ?? [] },
    { key: "skipped", items: rep?.skipped ?? [] },
  ];
}

/** 一組的數量（摘要那一行用）。 */
export function sweepCount(groups: SweepGroup[], key: SweepGroupKey): number {
  return groups.find((g) => g.key === key)?.items.length ?? 0;
}

/** 一項的細節那一行：可刪寫查過什麼、在用寫誰指到、沒查完寫原因、不合規寫哪裡不對與建議、略過寫原因。 */
export function sweepDetail(key: SweepGroupKey, it: BackupSweepItem): string {
  switch (key) {
    case "deletable":
    case "unchecked":
      return it.checked ?? "";
    case "inUse":
      return it.used_by ?? "";
    case "misnamed":
      return [it.problem, it.suggest].filter(Boolean).join("；");
    case "skipped":
      return it.reason ?? "";
  }
}
