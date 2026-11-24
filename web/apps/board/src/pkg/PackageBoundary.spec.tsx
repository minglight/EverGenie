import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test, vi } from "vitest";
import { BundleLoadError, loadBundle } from "./loadBundle";
import { PackageBoundary } from "./PackageBoundary";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function Boom({ error }: { error: Error | null }) {
  if (error) throw error;
  return <p>套件的畫面</p>;
}

describe("PackageBoundary", () => {
  // 套件畫面壞掉：跟核心頁同一種白話提示，英文原文只寫 console，不上畫面（UXC-8）。
  test("畫面壞掉時說「〈標題〉暫時載不進來」＋「再試一次」，錯誤原文不上畫面", () => {
    const log = vi.spyOn(console, "error").mockImplementation(() => {});
    let error: Error | null = new Error("Minified React error #130; visit https://react.dev");
    function Page() {
      return <Boom error={error} />;
    }
    render(
      <PackageBoundary title="飲食">
        <Page />
      </PackageBoundary>,
    );
    const box = screen.getByRole("alert");
    expect(box.textContent).toContain("飲食暫時載不進來");
    expect(box.textContent).not.toContain("React");
    expect(box.textContent).not.toContain("http");
    expect(box.textContent).not.toContain("自動重試");
    expect(log.mock.calls.some((c) => c.some((x) => x instanceof Error))).toBe(true);
    // 按「再試一次」重畫；這次好了就看到畫面。
    error = null;
    fireEvent.click(screen.getByRole("button", { name: "再試一次" }));
    expect(screen.getByText("套件的畫面")).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  test("程式沒抓到（斷網）會自己再試：過幾秒、或連回網路時重畫", () => {
    vi.useFakeTimers();
    vi.spyOn(console, "error").mockImplementation(() => {});
    let error: Error | null = new BundleLoadError(
      new TypeError("Failed to fetch dynamically imported module: http://127.0.0.1/x.js"),
    );
    function Page() {
      return <Boom error={error} />;
    }
    render(
      <PackageBoundary title="今天的習慣" variant="inline">
        <Page />
      </PackageBoundary>,
    );
    expect(screen.getByRole("alert").textContent).toContain("今天的習慣暫時載不進來");
    expect(screen.getByRole("alert").textContent).not.toContain("Failed to fetch");
    // 還是壞的：再試之後提示還在。
    act(() => {
      vi.advanceTimersByTime(9000);
    });
    expect(screen.getByRole("alert")).toBeTruthy();
    error = null;
    act(() => {
      window.dispatchEvent(new Event("online"));
    });
    expect(screen.getByText("套件的畫面")).toBeTruthy();
  });
});

describe("loadBundle 的重試", () => {
  test("沒抓到的不留著：下次呼叫重新抓，而且換一個網址（瀏覽器不會拿記住的失敗來回）", async () => {
    const url = "/pkg-bundles/does-not-exist-for-retry.js";
    const first = loadBundle(url);
    await expect(first).rejects.toBeInstanceOf(BundleLoadError);
    const second = loadBundle(url);
    expect(second).not.toBe(first);
    await expect(second).rejects.toBeInstanceOf(BundleLoadError);
  });
});
