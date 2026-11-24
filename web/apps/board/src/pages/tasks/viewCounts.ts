import type { SitePart } from "../../data/acrossSites";
import { matchAgent } from "../../data/agentFilter";
import type { TaskRow } from "../../data/queries";

/**
 * 任務單「未結 N」與狀態標籤（等你 N、進行中 N、等外部 N、待做 N）的數字：跟下面的清單同一個秘書篩選（WEB-96）——
 * 只看 Helper 時「等你」寫的是 Helper 的件數，不是全部的。沒選人就是全部。每台相加；一台都還沒讀到回 undefined（不先寫 0）。
 */
export function filteredViewCount(
  parts: readonly Pick<SitePart<TaskRow[]>, "site" | "data">[],
  picked: readonly string[],
  /** 只算符合的單（例：「待做」標籤只算 idea、搜尋框的字）；沒給就全算。 */
  only?: (row: TaskRow) => boolean,
): number | undefined {
  let seen = false;
  let total = 0;
  for (const p of parts) {
    if (p.data === undefined) continue;
    seen = true;
    total += p.data.filter(
      (r) => (!only || only(r)) && matchAgent(picked, r.task.agent, p.site.key),
    ).length;
  }
  return seen ? total : undefined;
}
