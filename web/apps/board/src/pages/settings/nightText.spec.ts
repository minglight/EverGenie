import { describe, expect, test } from "vitest";
import { addTodo, nightTemplate, oneLine, removeTodo, viewOf } from "./nightText";

const sample = `# 夜班清單

## 待做
- [ ] 整理 A｜加入 2026-10-20
  進度：做到一半
- [ ] 補測試 B｜加入 2026-10-21

## 要主人
- [ ] 要 sudo 的事｜加入 2026-10-19｜卡住：要 sudo

## 做完（留最近 14 天）
- [x] 舊事｜加入 2026-10-10｜結果：好｜完成 2026-10-20
`;

// Guards: REQ-19, REQ-111
test("addTodo：補充欄位變成縮排說明行，viewOf 讀成 notes", () => {
  const next =
    addTodo(undefined, "review PR", new Date(2026, 9, 29), ["要讀：docs/a.md", "  ", "產出：x"]) ??
    "";
  expect(next).toContain("- [ ] review PR｜加入 2026-10-29\n  要讀：docs/a.md\n  產出：x");
  expect(viewOf(next).todo[0]?.notes).toEqual(["要讀：docs/a.md", "產出：x"]);
});

describe("夜班清單的純文字改寫", () => {
  // Guards: REQ-111
  test("三段各自的項目、後面的欄位和縮排的進度說明", () => {
    const v = viewOf(sample);
    expect(v.todo.map((e) => e.text)).toEqual(["整理 A", "補測試 B"]);
    expect(v.todo[0]?.meta).toEqual(["加入 2026-10-20"]);
    expect(v.todo[0]?.notes).toEqual(["進度：做到一半"]);
    expect(v.blocked[0]?.meta).toEqual(["加入 2026-10-19", "卡住：要 sudo"]);
    expect(v.done[0]).toMatchObject({
      text: "舊事",
      meta: ["加入 2026-10-10", "結果：好", "完成 2026-10-20"],
    });
    expect(viewOf(undefined)).toEqual({ todo: [], blocked: [], done: [] });
    expect(viewOf("").todo).toEqual([]);
  });

  // Guards: REQ-111
  test("新增：加在待做最下面，單行；檔案還沒有就用範本；空的不加", () => {
    const next = addTodo(sample, "  寫 C\n 的文件｜順便 ", new Date(2026, 9, 25)) ?? "";
    expect(next).toContain(
      "- [ ] 補測試 B｜加入 2026-10-21\n- [ ] 寫 C 的文件|順便｜加入 2026-10-25\n\n## 要主人",
    );
    expect(viewOf(next).todo).toHaveLength(3);
    const fresh = addTodo(undefined, "第一項", new Date(2026, 9, 26)) ?? "";
    expect(fresh).toContain("# 夜班清單");
    expect(fresh).toContain("## 待做\n- [ ] 第一項｜加入 2026-10-26\n\n## 要主人");
    expect(fresh).toContain("主人排好");
    expect(addTodo(sample, "   ", new Date())).toBeNull();
    expect(oneLine("a｜b\n c")).toBe("a|b c");
  });

  // Guards: REQ-111
  test("刪除：連進度說明一起；序號不對回 null；其他段落不動", () => {
    const next = removeTodo(sample, 1) ?? "";
    expect(next).not.toContain("整理 A");
    expect(next).not.toContain("做到一半");
    expect(next).toContain("- [ ] 補測試 B");
    expect(next).toContain("要 sudo 的事");
    expect(next).toContain("舊事");
    expect(removeTodo(sample, 0)).toBeNull();
    expect(removeTodo(sample, 3)).toBeNull();
    expect(removeTodo(nightTemplate, 1)).toBeNull();
  });

  // Guards: REQ-111
  test("範本三段標題都在，解析再輸出不變", () => {
    for (const h of ["## 待做", "## 要主人", "## 做完（留最近 14 天）"]) {
      expect(nightTemplate).toContain(`${h}\n`);
    }
    const once = addTodo(nightTemplate, "x", new Date(2026, 9, 26)) ?? "";
    expect(addTodo(once, "y", new Date(2026, 9, 26))).toContain(
      "- [ ] x｜加入 2026-10-26\n- [ ] y｜加入 2026-10-26",
    );
  });
});
