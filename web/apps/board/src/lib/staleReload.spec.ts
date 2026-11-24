// @vitest-environment jsdom
import { afterEach, describe, expect, test, vi } from "vitest";
import { entryOf, installStaleReload, safeToReload } from "./staleReload";

afterEach(() => {
  document.head.innerHTML = "";
  document.body.innerHTML = "";
  vi.useRealTimers();
});

function page(entry: string) {
  document.head.innerHTML = `<script type="module" crossorigin src="${entry}"></script>`;
}

describe("staleReload（WEB-172）", () => {
  test("從 index.html 找主程式", () => {
    expect(
      entryOf('<script type="module" crossorigin src="/assets/index-Cqvqms1z.js"></script>'),
    ).toBe("/assets/index-Cqvqms1z.js");
    expect(entryOf("<html></html>")).toBe("");
    // 預載的分包也叫 index 時不能抓錯：只認 type="module" 那支。
    expect(
      entryOf(
        '<link rel="modulepreload" href="/assets/index-LAZY.js"><script type="module" src="/assets/index-MAIN.js"></script>',
      ),
    ).toBe("/assets/index-MAIN.js");
  });

  test("在打字、有打到一半的字、開著視窗就不重整", () => {
    expect(safeToReload(document)).toBe(true);
    document.body.innerHTML = "<textarea></textarea>";
    const ta = document.querySelector("textarea") as HTMLTextAreaElement;
    ta.focus();
    expect(safeToReload(document)).toBe(false);
    ta.blur();
    expect(safeToReload(document)).toBe(true);
    ta.value = "打到一半";
    expect(safeToReload(document)).toBe(false);
    document.body.innerHTML = '<div role="dialog"></div>';
    expect(safeToReload(document)).toBe(false);
    document.body.innerHTML = '<input type="text" value="填一半">';
    expect(safeToReload(document)).toBe(false);
    document.body.innerHTML = '<input type="checkbox" value="on">';
    expect(safeToReload(document)).toBe(true);
    document.body.innerHTML = "<div data-unsaved>附件</div>";
    expect(safeToReload(document)).toBe(false);
  });

  async function tick() {
    await vi.advanceTimersByTimeAsync(60_000);
  }

  test("伺服器換版本才重整；同版本、抓不到都不動", async () => {
    vi.useFakeTimers();
    page("/assets/index-OLD.js");
    let html = '<script type="module" src="/assets/index-OLD.js"></script>';
    const reload = vi.fn();
    const stop = installStaleReload(document, async () => html, reload);
    await tick();
    expect(reload).not.toHaveBeenCalled();
    html = "";
    await tick();
    expect(reload).not.toHaveBeenCalled();
    html = '<script type="module" src="/assets/index-NEW.js"></script>';
    await tick();
    expect(reload).toHaveBeenCalledTimes(1);
    stop();
  });

  test("換版本時在打字：等打完（下一次檢查）才重整", async () => {
    vi.useFakeTimers();
    page("/assets/index-OLD.js");
    document.body.innerHTML = "<textarea></textarea>";
    const ta = document.querySelector("textarea") as HTMLTextAreaElement;
    ta.value = "還沒送";
    const reload = vi.fn();
    const stop = installStaleReload(
      document,
      async () => '<script type="module" src="/assets/index-NEW.js"></script>',
      reload,
    );
    await tick();
    expect(reload).not.toHaveBeenCalled();
    ta.value = "";
    await tick();
    expect(reload).toHaveBeenCalledTimes(1);
    stop();
  });

  test("開發模式（沒有打包的主程式）不裝", () => {
    document.head.innerHTML = '<script type="module" src="/src/main.tsx"></script>';
    const fetchHtml = vi.fn(async () => "");
    installStaleReload(document, fetchHtml, vi.fn())();
    expect(fetchHtml).not.toHaveBeenCalled();
  });
});
