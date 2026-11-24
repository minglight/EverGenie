import { describe, expect, test } from "vitest";
import { commandLabel, isSlashText, parseSlash, slashItems } from "./slash";

describe("輸入框的「/」指令選單（WEB-83）", () => {
  test("開頭打 / 才開選單，列出四個指令；前面有空白或多行就是一般訊息", () => {
    expect(slashItems("/")?.map((i) => i.label)).toEqual([
      "/clear",
      "/compact",
      "/model",
      "/effort",
    ]);
    expect(slashItems(" /clear")).toBeNull();
    expect(slashItems("你好 /clear")).toBeNull();
    expect(slashItems("/clear\n第二行")).toBeNull();
    expect(isSlashText("/clear")).toBe(true);
  });

  test("打字篩選指令；不認得的列空的（選單不開）", () => {
    expect(slashItems("/c")?.map((i) => i.label)).toEqual(["/clear", "/compact"]);
    expect(slashItems("/CO")?.map((i) => i.label)).toEqual(["/compact"]);
    expect(slashItems("/xyz")).toEqual([]);
  });

  test("/model、/effort 後面打空白是第二層選項，照打的字篩選；沒有選項的指令沒有第二層", () => {
    expect(slashItems("/model ")?.map((i) => i.arg)).toEqual(["sonnet", "opus", "fable", "haiku"]);
    expect(slashItems("/model o")?.map((i) => i.arg)).toEqual(["opus"]);
    expect(slashItems("/effort ")?.map((i) => i.arg)).toEqual([
      "low",
      "medium",
      "high",
      "xhigh",
      "max",
      "auto",
    ]);
    expect(slashItems("/effort m")?.map((i) => i.arg)).toEqual(["medium", "max"]);
    // xhigh 在畫面上寫 extra（作者 2026-11-03），打 e 找得到、送的還是 xhigh。
    expect(slashItems("/effort e")?.map((i) => [i.label, i.arg])).toEqual([["extra", "xhigh"]]);
    expect(parseSlash("/effort extra", 0)).toMatchObject({ kind: "command", arg: "xhigh" });
    expect(commandLabel("effort", "xhigh")).toBe("/effort extra");
    expect(slashItems("/clear now")).toEqual([]);
    expect(slashItems("/nope x")).toEqual([]);
  });

  test("每一列都有白話說明", () => {
    for (const raw of ["/", "/model ", "/effort "]) {
      for (const item of slashItems(raw) ?? []) expect(item.hint.length).toBeGreaterThan(0);
    }
  });

  test("送出時：打完整的指令就是指令、要參數的提醒選、不認得的不送、有附件或前面有空白是一般訊息", () => {
    expect(parseSlash("/clear", 0)).toMatchObject({ kind: "command", arg: "" });
    expect(parseSlash("/clear", 0)).toMatchObject({ spec: { name: "clear", confirm: true } });
    expect(parseSlash("/model Opus", 0)).toMatchObject({
      kind: "command",
      spec: { name: "model" },
      arg: "opus",
    });
    expect(parseSlash("/effort max ", 0)).toMatchObject({ kind: "command", arg: "max" });
    expect(parseSlash("/model", 0)).toMatchObject({ kind: "needArg", spec: { name: "model" } });
    expect(parseSlash("/model gpt", 0)).toEqual({ kind: "unknown" });
    expect(parseSlash("/clear all", 0)).toEqual({ kind: "unknown" });
    expect(parseSlash("/help", 0)).toEqual({ kind: "unknown" });
    expect(parseSlash("/tmp 為什麼滿了", 0)).toEqual({ kind: "message" });
    expect(parseSlash("/model opus now", 0)).toEqual({ kind: "unknown" });
    expect(parseSlash("/clear", 1)).toEqual({ kind: "message" });
    expect(parseSlash(" /clear", 0)).toEqual({ kind: "message" });
    expect(parseSlash("早安", 0)).toEqual({ kind: "message" });
  });
});
