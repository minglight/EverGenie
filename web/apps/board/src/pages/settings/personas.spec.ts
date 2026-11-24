import type { PersonaCard } from "@evergenie/sdk-web";
import { ApiError } from "@evergenie/sdk-web";
import { describe, expect, test } from "vitest";
import {
  assignablePersonas,
  fieldError,
  groupPersonas,
  hasErrors,
  isDirty,
  parseAka,
  saveFailure,
  slugPattern,
  tagsOf,
  validateNewCard,
} from "./personas";

describe("角色庫的純函式", () => {
  // Guards: REQ-130
  test("標籤：來源、設計中、坐在誰那裡、被官方蓋住，依序", () => {
    expect(tagsOf({ source: "official", status: "live", seats: [], shadowed: false })).toEqual([
      { kind: "official" },
    ]);
    expect(
      tagsOf({ source: "mine", status: "design", seats: ["code", "life"], shadowed: false }),
    ).toEqual([
      { kind: "mine" },
      { kind: "design" },
      { kind: "seat", agent: "code" },
      { kind: "seat", agent: "life" },
    ]);
    expect(tagsOf({ source: "mine", status: "live", seats: [], shadowed: true })).toEqual([
      { kind: "mine" },
      { kind: "shadowed" },
    ]);
    // 伺服器沒給 seats（欄位省略）也不會壞。
    expect(tagsOf({ source: "mine", status: "live", shadowed: false })).toEqual([{ kind: "mine" }]);
  });

  // Guards: REQ-325
  test("來自套件的卡（source = package）：標籤標套件、不能指派的加提示；沒看過的來源當我的", () => {
    expect(
      tagsOf({
        source: "package",
        package: "garden",
        unassignable: "已停用",
        status: "live",
        seats: [],
        shadowed: false,
      }),
    ).toEqual([
      { kind: "package", pkg: "garden" },
      { kind: "unassignable", reason: "已停用" },
    ]);
    expect(tagsOf({ source: "whatever", status: "live", seats: [], shadowed: false })).toEqual([
      { kind: "mine" },
    ]);
  });

  // Guards: REQ-325
  test("角色分組：官方、我的、各套件（依套件 id 排）；能指派的只留上線、沒被蓋住、套件有效的", () => {
    const cards = [
      { slug: "garden.fairy", source: "package", package: "garden", status: "live" },
      { slug: "mina", source: "mine", status: "live" },
      { slug: "athena", source: "official", status: "live" },
      { slug: "zoo.lion", source: "package", package: "zoo", status: "live", unassignable: "被拒" },
      { slug: "park.elf", source: "package", package: "park", status: "design" },
      { slug: "old", source: "mine", status: "live", shadowed: true },
    ] as unknown as PersonaCard[];
    expect(groupPersonas(cards).map((g) => [g.key, g.cards.map((c) => c.slug)])).toEqual([
      ["official", ["athena"]],
      ["mine", ["mina", "old"]],
      ["package-garden", ["garden.fairy"]],
      ["package-park", ["park.elf"]],
      ["package-zoo", ["zoo.lion"]],
    ]);
    expect(assignablePersonas(cards).map((c) => c.slug)).toEqual([
      "garden.fairy",
      "mina",
      "athena",
    ]);
  });

  // Guards: REQ-130
  test("新增表單：代號與名字的必填與格式，撞名交給伺服器", () => {
    expect(validateNewCard({ slug: "", name: "" })).toEqual({
      slug: "settings.personas.form.slug.required",
      name: "settings.personas.form.name.required",
    });
    expect(validateNewCard({ slug: "Hotaru", name: "螢" }).slug).toBe(
      "settings.personas.form.slug.bad",
    );
    expect(validateNewCard({ slug: "hotaru_x", name: "螢" }).slug).toBe(
      "settings.personas.form.slug.bad",
    );
    expect(validateNewCard({ slug: "my-card", name: "螢" }).slug).toBe(
      "settings.personas.form.slug.bad",
    );
    const ok = validateNewCard({ slug: " hotaru2 ", name: " 螢 " });
    expect(hasErrors(ok)).toBe(false);
    expect(slugPattern.exec("a")).not.toBeNull();
    expect(slugPattern.exec("my_hotaru")).not.toBeNull();
    expect(slugPattern.exec("my_")).toBeNull();
  });

  // Guards: REQ-130
  test("別名輸入：逗號、頓號或換行分開，去空白與重複", () => {
    expect(parseAka(" 小螢, firefly，小螢\n、 ")).toEqual(["小螢", "firefly"]);
    expect(parseAka("")).toEqual([]);
  });

  // Guards: REQ-130
  test("存檔失敗：樂觀鎖衝突要提示重新載入，其他照伺服器的白話原因", () => {
    const conflict = new ApiError("conflict", "卡片被別人改過，請重新載入");
    expect(saveFailure(conflict)).toEqual({ kind: "conflict", text: "卡片被別人改過，請重新載入" });
    expect(saveFailure(new ApiError("invalid", "名字「X」跟角色 athena撞名，換一個"))).toEqual({
      kind: "other",
      text: "名字「X」跟角色 athena撞名，換一個",
    });
    // 網路或瀏覽器自己丟的英文錯誤，換成一句白話（UXC-8）。
    const other = saveFailure(new TypeError("Failed to fetch"));
    expect(other.kind).toBe("other");
    expect(other.text).not.toMatch(/fetch/i);
  });

  // Guards: REQ-130
  test("伺服器的錯誤對到欄位：field 對得上寫在欄位下，對不上是整張表單的錯", () => {
    expect(fieldError(new ApiError("invalid", "撞名", "name"), ["slug", "name"])).toEqual({
      field: "name",
      text: "撞名",
    });
    expect(fieldError(new ApiError("conflict", "已經有角色 x"), ["slug", "name"])).toEqual({
      text: "已經有角色 x",
    });
    expect(fieldError(new ApiError("invalid", "x", "other"), ["slug", "name"]).field).toBe(
      undefined,
    );
  });

  // Guards: REQ-130
  test("有沒有改過：名字、別名（內容與順序）、內文", () => {
    const card = { name: "螢", aka: ["小螢", "firefly"] };
    const same = { name: "螢", aka: ["小螢", "firefly"], body: "內文" };
    expect(isDirty(card, "內文", same)).toBe(false);
    expect(isDirty(card, "內文", { ...same, name: "螢2" })).toBe(true);
    expect(isDirty(card, "內文", { ...same, aka: ["firefly", "小螢"] })).toBe(true);
    expect(isDirty(card, "內文", { ...same, aka: ["小螢"] })).toBe(true);
    expect(isDirty(card, "內文", { ...same, body: "內文\n" })).toBe(true);
    expect(isDirty({ name: "螢" }, "", { name: "螢", aka: [], body: "" })).toBe(false);
  });
});
