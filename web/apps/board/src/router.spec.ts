import { createMemoryHistory } from "@tanstack/react-router";
import { describe, expect, test } from "vitest";
import { router } from "./router";

async function land(url: string) {
  router.update({ ...router.options, history: createMemoryHistory({ initialEntries: [url] }) });
  await router.load();
  return router.state.location;
}

describe("舊網址轉址", () => {
  // 角色庫 2026-11-04 從「設定 → 角色庫」搬到「角色」頁（DEV-117、WEB-138）：舊連結與書籤照樣開得到同一張卡。
  // DEV-143 起一位角色一頁：帶 ?card= 的轉到 /personas/<代號>。
  test("/settings/personas 轉到 /personas，帶 ?card= 的轉到那位的頁", async () => {
    const withCard = await land("/settings/personas?card=athena");
    expect(withCard.pathname).toBe("/personas/athena");
    expect(withCard.search).toEqual({});
    const plain = await land("/settings/personas");
    expect(plain.pathname).toBe("/personas");
    expect(plain.search).toEqual({});
  });

  // 通知「去角色頁看看」、舊書籤的 /personas?card=<代號>（DEV-143 以前的詳情）轉到 /personas/<代號>。
  test("/personas?card= 轉到 /personas/<代號>；分頁記在 ?tab=", async () => {
    const old = await land("/personas?card=luna");
    expect(old.pathname).toBe("/personas/luna");
    const tab = await land("/personas/luna?tab=demo.tab");
    expect(tab.pathname).toBe("/personas/luna");
    expect(tab.search).toEqual({ tab: "demo.tab" });
    const add = await land("/personas?new=1");
    expect(add.pathname).toBe("/personas");
    expect(router.state.matches.at(-1)?.search).toEqual({ new: true });
  });
});
