import { api } from "@evergenie/sdk-web";
import { describe, expect, test, vi } from "vitest";
import { loadBundle } from "./loadBundle";
import { host, installSharedRuntime, setHostNavigator } from "./runtime";

type Shared = Record<string, Record<string, unknown>>;

describe("套件頁面的共用模組", () => {
  // Guards: REQ-114
  test("TestSharedRuntimeInstalled：外殼在 render 之前把套件頁面會 import 的模組放進 globalThis", () => {
    installSharedRuntime();
    const shared = (globalThis as unknown as { __hgShared: Shared }).__hgShared;
    expect(Object.keys(shared).sort()).toEqual(
      [
        "@evergenie/host",
        "@evergenie/sdk-web",
        "@evergenie/ui",
        "react",
        "react-dom",
        "react-dom/client",
        "react/jsx-runtime",
      ].sort(),
    );
    expect(typeof shared.react?.useState).toBe("function");
    expect(typeof shared["react/jsx-runtime"]?.jsx).toBe("function");
    expect(shared["@evergenie/ui"]?.Button).toBeDefined();
    expect(shared["@evergenie/host"]).toBe(host);
    expect(host.client).toBe(api);
    expect(typeof host.Slot).toBe("function");
  });

  // Guards: REQ-114
  test("TestLoadBundleOncePerUrl：同一個網址只載一次，取 default export", async () => {
    const url = `data:text/javascript,${encodeURIComponent("export default function Hello() { return null; }")}`;
    const first = loadBundle(url);
    expect(loadBundle(url)).toBe(first);
    const Hello = await first;
    expect(typeof Hello).toBe("function");
  });

  // Guards: REQ-114
  test("TestLoadBundleRejectsMissingDefault：沒有 default export 的頁面載入失敗並說明", async () => {
    const url = `data:text/javascript,${encodeURIComponent("export const x = 1;")}`;
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    await expect(loadBundle(url)).rejects.toThrow("沒有 export default");
    spy.mockRestore();
  });

  // 套件轉址（例：舊的上傳頁轉到角色頁）要走前端路由、用 replace，站內歷史才不會歸零（FEN-29、DEV-143）。
  test("套件的 navigate 走外殼掛上的前端路由，replace 照傳；站外網址與伺服器檔案不交給路由", () => {
    const go = vi.fn();
    setHostNavigator(go);
    host.navigate("/personas/kaguya?tab=x", { replace: true });
    host.navigate("/personas");
    expect(go.mock.calls).toEqual([
      ["/personas/kaguya?tab=x", true],
      ["/personas", false],
    ]);
    const assign = vi.fn();
    const orig = window.location;
    Object.defineProperty(window, "location", { value: { ...orig, assign }, configurable: true });
    host.navigate("//elsewhere.example/x");
    // 伺服器的檔案（/share/、/api/）不是看板頁面，整頁換。
    host.navigate("/share/a.png");
    expect(go).toHaveBeenCalledTimes(2);
    expect(assign.mock.calls).toEqual([["//elsewhere.example/x"], ["/share/a.png"]]);
    Object.defineProperty(window, "location", { value: orig, configurable: true });
    setHostNavigator(null);
  });
});
