import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test } from "vitest";
import type { Site } from "../data/sites";
import { SiteNotice, siteNoticeText } from "./SiteNotice";

const office = (over: Partial<Site> = {}): Site => ({
  key: "amy-office",
  base: "https://genie-amy-office.example.ts.net",
  primary: false,
  label: "amy-office",
  machine: "office",
  env: "prod",
  version: "0.9.0",
  boardApi: 1,
  publicUrl: "https://genie-amy-office.example.ts.net",
  color: "",
  state: "ok",
  reason: "",
  ...over,
});

afterEach(cleanup);

describe("某台不能用時的一行提示", () => {
  test("能用的台（別台 ok、主伺服器自己）不畫", () => {
    for (const s of [office(), office({ primary: true, key: "", state: "down" })]) {
      expect(siteNoticeText(s)).toBeNull();
    }
    const { container } = render(<SiteNotice site={office()} />);
    expect(container.innerHTML).toBe("");
  });

  test("版本太舊講出那台的台名與版本；還在試連、不認得的狀態也各講一句", () => {
    expect(siteNoticeText(office({ state: "old" }))).toBe(
      "amy-office 的版本比這個看板舊（0.9.0），先更新那台，這裡才看得到它的資料。",
    );
    expect(siteNoticeText(office({ state: "checking" }))).toBe("正在連線到 amy-office…");
    expect(siteNoticeText(office({ state: "whatever" as unknown as Site["state"] }))).toBe(
      "amy-office 現在不能用。",
    );
  });

  test("被拒時有伺服器給的原因就照講，沒有就講可能的兩個原因", () => {
    expect(siteNoticeText(office({ state: "denied", reason: "這台只接受主人的裝置" }))).toBe(
      "amy-office 不讓這個看板連：這台只接受主人的裝置",
    );
    expect(siteNoticeText(office({ state: "denied" }))).toBe(
      "amy-office 沒有允許這個看板，或版本太舊。",
    );
  });

  test("從沒連上過（不知道台名）用網址的主機當名字", () => {
    const s = office({
      key: "https://x.example.ts.net",
      base: "https://x.example.ts.net",
      label: "",
      state: "down",
    });
    render(<SiteNotice site={s} />);
    const note = screen.getByRole("status");
    expect(note.textContent).toContain("x.example.ts.net 現在連不到");
    expect(note.className).toContain("text-muted");
    expect(note.getAttribute("data-state")).toBe("down");
    expect(note.getAttribute("data-site")).toBe("https://x.example.ts.net");
  });
});
