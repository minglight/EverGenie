import type { Package } from "@evergenie/sdk-web";
import { expect, test } from "vitest";
import { hiddenDevOnly } from "./OptionalPackages";

const admin: Package = { id: "admin", optional: true, dev_only: true, enabled: false };

// 開發工坊這類 dev_only 套件：正式家的設定頁不列（一般人用不到）；開發家照列；
// 正式家已經開著的照列，才關得掉；家的身分還沒讀到時當成正式家（作者 2026-11-04）。
test("TestDevOnlyPackageListedOnlyOnDevHome：正式家不列沒開的 dev_only 套件", () => {
  expect(hiddenDevOnly(admin, false)).toBe(true);
  expect(hiddenDevOnly(admin, true)).toBe(false);
  expect(hiddenDevOnly({ ...admin, enabled: true }, false)).toBe(false);
  expect(hiddenDevOnly({ id: "habits", optional: true, enabled: false }, false)).toBe(false);
});
