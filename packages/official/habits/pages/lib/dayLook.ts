import type { CalendarCell } from "./habits";

// 本週七格與月曆共用的長相（HAB-17）：做了綠底 ✓、斷了淡紅 ✕、跳過「－」（第一次淡灰、這週第二次起淡紫虛線框）、
// 還沒做空白、今天另外加框。不只靠顏色分，每種都有符號；顏色只用 token（FEN-7）。
export function dayClass(c: Pick<CalendarCell, "state" | "skipNo">): string {
  switch (c.state) {
    case "done":
      return "bg-ok text-on-ok font-semibold";
    case "broken":
      return "border border-danger bg-danger-soft text-danger font-semibold";
    case "skipped":
      // 不用橘：橘只給「等你處理」（WEB-70）。第一次淡灰，第二次起淡紫加虛線框（快到每週上限了）。
      return c.skipNo >= 2
        ? "border border-dashed border-accent bg-accent-soft text-accent font-semibold"
        : "bg-surface-2 text-muted";
    case "open":
      return "border border-border bg-surface text-fg";
    default:
      return "text-muted opacity-40";
  }
}

export function dayMark(c: Pick<CalendarCell, "state">): string {
  return c.state === "done" ? "✓" : c.state === "broken" ? "✕" : c.state === "skipped" ? "－" : "";
}

export function dayWord(c: Pick<CalendarCell, "state" | "skipNo">): string {
  switch (c.state) {
    case "done":
      return "做了";
    case "broken":
      return "沒做，斷了";
    case "skipped":
      return `跳過（這週第 ${c.skipNo} 次）`;
    case "open":
      return "還沒做";
    default:
      return "";
  }
}
