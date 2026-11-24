import { describe, expect, test, vi } from "vitest";
import {
  createAndStart,
  deployBody,
  fill,
  homeDetail,
  nameAfterCJK,
  nightlyBody,
  noSafeHint,
  pendingNote,
  runningTask,
  safeLabel,
  stableBody,
  T,
  toDevHome,
  toRelease,
  trackOpened,
  versionProblem,
  waitRequest,
  whenText,
} from "./release";

describe("開發工坊頁面的純函式", () => {
  test("TestAdminToReleaseReadsProgramRecord：admin 程式寫的欄位轉成畫面用的形狀，缺的欄位不炸", () => {
    const r = toRelease({
      id: "current",
      fields: {
        version: "0.2.0-3-gabc1234",
        main: { commit: "abcdef0123", subject: "修東西", at: "2026-10-31T00:00:00Z" },
        pending: [{ commit: "abcdef0123", short: "abcdef0", subject: "修東西" }, "壞的"],
        pending_more: true,
        nightly: { commit: "1234567890", ok: true, steps: [{ name: "ci", ok: true, seconds: 12 }] },
        next: { version: "0.2.0", notes: "說明" },
        computed_at: "2026-10-31T01:00:00Z",
      },
    });
    expect(r?.version).toBe("0.2.0-3-gabc1234");
    expect(r).not.toHaveProperty("sites");
    expect(r?.main?.short).toBe("abcdef0");
    expect(r?.pending).toHaveLength(1);
    expect(r?.pendingMore).toBe(true);
    expect(r?.latest).toBeNull();
    expect(r?.nightly?.short).toBe("1234567");
    expect(r?.nightly?.steps[0]).toEqual({ name: "ci", ok: true, seconds: 12 });
    expect(r?.next).toEqual({ version: "0.2.0", notes: "說明", safeTag: "", draft: false });
    expect(toRelease(null)).toBeNull();
  });

  test("TestAdminToReleaseLegacySites：跨台時代的舊資料只有 sites 時取第一個有版本的那台，沒有版本不炸", () => {
    const old = toRelease({
      fields: {
        sites: [
          { site: "office", role: "node", online: false },
          { site: "mini", role: "hub", online: true, version: "abc1234" },
        ],
      },
    });
    expect(old?.version).toBe("abc1234");
    expect(old).not.toHaveProperty("sites");
    expect(toRelease({ fields: { sites: "壞的" } })?.version).toBe("");
    // 新舊都有時以 version 為準
    expect(
      toRelease({ fields: { version: "0.3.0", sites: [{ site: "mini", version: "0.1.0" }] } })
        ?.version,
    ).toBe("0.3.0");
  });

  test("TestAdminToDevHomeUnknownStateAndBadURL：不認得的狀態當沒設定、不是 http(s) 的網址不顯示", () => {
    expect(toDevHome({ fields: { state: "weird", url: "javascript:alert(1)" } })).toMatchObject({
      state: "unset",
      url: "",
    });
    expect(
      toDevHome({ fields: { state: "running", board_port: 18870, url: "https://x.invalid:1" } }),
    ).toMatchObject({
      state: "running",
      boardPort: 18870,
      url: "https://x.invalid:1",
    });
    // 跨台時代留下的舊資料（offline、site）：當沒設定，不帶 site。
    const old = toDevHome({ fields: { state: "offline", site: "office" } });
    expect(old).toMatchObject({ state: "unset" });
    expect(old).not.toHaveProperty("site");
  });

  test("TestAdminDeployBodyListsVersionAndChanges：部署單寫要部署的版本、做法、帶上的改動", () => {
    const main = { commit: "abcdef0123", short: "abcdef0", subject: "最新", at: "" };
    const body = deployBody(main, [
      main,
      { commit: "9", short: "9999999", subject: "前一個", at: "" },
    ]);
    expect(body).toContain("abcdef0123（最新）");
    expect(body).toContain("scripts/deploy.sh all");
    expect(body).toContain("  - 9999999 前一個");
    expect(body).not.toContain("dev submit");
  });

  test("TestAdminVersionProblem：版號要 x.y.z 而且比上一個正式版大，沒填不報錯", () => {
    expect(versionProblem("", "0.1.0")).toBe("");
    expect(versionProblem("0.2", "0.1.0")).toBe(T.versionBad);
    expect(versionProblem("0.1.0", "0.1.0")).toBe(fill(T.versionSmall, { latest: "0.1.0" }));
    expect(versionProblem("0.10.0", "0.9.9")).toBe("");
    expect(versionProblem("1.0.0", "")).toBe("");
  });

  test("TestAdminStableAndNightlyBody：發正式版單帶版號、safe 標記與說明；測試單帶做法", () => {
    const body = stableBody("0.2.0", "safe/2026-10-31", "abc", "  新功能\n");
    expect(body).toContain("- 版號：0.2.0");
    expect(body).toContain("safe/2026-10-31（abc）");
    expect(body).toContain("\n新功能\n");
    expect(body).toContain("--version 0.2.0");
    expect(nightlyBody()).toContain("scripts/release.sh nightly");
    expect(safeLabel("safe/2026-10-31.2")).toBe("2026-10-31.2");
  });

  test("TestAdminRunningTaskOnlyOpenOnes：同一種單還沒結束才算在跑", () => {
    const tasks = [
      { id: "SEC-1", title: "部署 abc 到 mini、office", status: "done" },
      { id: "SEC-2", title: "發版測試（每晚測試）", status: "doing" },
      { id: "SEC-3", title: "部署 def 到 mini、office", status: "waiting_user" },
    ];
    expect(runningTask(tasks, "部署 ")?.id).toBe("SEC-3");
    expect(runningTask(tasks, "發版測試")?.id).toBe("SEC-2");
    expect(runningTask(tasks, "發正式版 ")).toBeUndefined();
  });

  // 「跑發版測試」鈕排在這句提示的上面（release.tsx 的 Stable）：提示要指對方向、用按鈕上的字。
  test("TestAdminNoSafeHintPointsToButtonAbove：還沒有測過的版本時，提示指向上面的「跑發版測試」", () => {
    expect(T.noSafe).toContain(`上面的「${T.nightlyRun}」`);
    expect(T.noSafe).not.toContain("下面");
  });

  // 上面真的有「跑發版測試」鈕（有 admin 秘書）才叫人按它；名冊還沒讀到、或沒有秘書時不說；
  // 也不說「每晚會自己跑」（沒裝排程，DEV-113）。
  test("TestAdminHintsOnlyPromiseWhatExists：沒有按鈕時不叫人按上面的鈕、不說每晚自己跑", () => {
    expect(noSafeHint({ name: "輝夜" })).toBe(T.noSafe);
    expect(noSafeHint({ name: "" })).toBe("");
    expect(noSafeHint(undefined)).toBe("");
    expect(T.nightlyHint).not.toContain("每晚也會");
    expect(T.stableSub).not.toContain("每晚");
  });

  // 頁面要分得出說明是不是還是原樣草稿，表單才會提示「admin 會改寫成白話」（DEV-113）。
  test("TestAdminReleaseReadsNextDraft：next 帶 safe_tag 與 draft", () => {
    const r = toRelease({
      fields: { next: { version: "0.2.0", notes: "x", safe_tag: "safe/2026-11-04", draft: true } },
    });
    expect(r?.next).toEqual({
      version: "0.2.0",
      notes: "x",
      safeTag: "safe/2026-11-04",
      draft: true,
    });
    const plain = toRelease({ fields: { next: { version: "0.2.0", notes: "x" } } });
    expect(plain?.next?.draft).toBe(false);
  });

  // 測試單請 admin 秘書把原樣 commit 的草稿改寫成白話，再用 release.sh notes 寫回（DEV-113）。
  test("TestAdminNightlyBodyAsksPlainNotes：測試單要她改寫成白話並用 notes 寫回", () => {
    expect(nightlyBody()).toContain("白話");
    expect(nightlyBody()).toContain("scripts/release.sh notes --notes");
  });

  test("TestAdminWaitRequestTimesOut：程式一直沒處理重算請求時回 timeout，不默默當成好了", async () => {
    const wait = () => Promise.resolve();
    let reads = 0;
    const pending = async () => {
      reads++;
      return { state: "pending", reason: "" };
    };
    expect(await waitRequest(pending, wait, 3)).toEqual({ end: "timeout", reason: "" });
    expect(reads).toBe(3);
    expect(await waitRequest(async () => undefined, wait, 2)).toEqual({
      end: "timeout",
      reason: "",
    });
    const seq = ["pending", "done"];
    expect(
      await waitRequest(async () => ({ state: seq.shift() ?? "", reason: "" }), wait, 5),
    ).toEqual({ end: "done", reason: "" });
    expect(
      await waitRequest(async () => ({ state: "rejected", reason: "看不懂" }), wait, 5),
    ).toEqual({ end: "rejected", reason: "看不懂" });
    expect(T.refreshTimeout).toContain("沒有回應");
  });

  test("TestAdminOpenedTaskReleasedAfterItEnds：剛開的單在清單列到前照樣算在跑，列過之後單結束了就放掉、按鈕回來", () => {
    const o = { id: "SEC-9", title: "部署 abc 到 mini、office", seen: false };
    // 清單還沒列到（重讀還沒回來）：照樣留著，不讓按鈕閃回來
    expect(trackOpened(o, [])).toEqual({ ...o, misses: 1 });
    // 一直沒列到（單在列到前就結束了）：等幾次就放掉，不會永遠卡在「在跑」
    let x: typeof o | null = o;
    for (let i = 0; i < 3; i++) x = trackOpened(x, []);
    expect(x).toBeNull();
    expect(trackOpened({ id: "", title: "t", seen: false }, [])).toBeNull();
    // 清單列到了：記下 seen
    const seen = trackOpened(o, [{ id: "SEC-9", status: "doing" }]);
    expect(seen).toEqual({ ...o, seen: true });
    // 單結束了（不在沒結束的清單、或狀態變 done）：放掉
    expect(trackOpened(seen, [])).toBeNull();
    expect(trackOpened(seen, [{ id: "SEC-9", status: "done" }])).toBeNull();
    expect(trackOpened(null, [])).toBeNull();
  });

  test("TestAdminWhenTextPlain：程式寫的 RFC3339 時間顯示成「11 月 3 日 02:10」，不是今年的多寫年，讀不懂的照原樣", () => {
    const now = new Date(2026, 10, 3, 9, 0);
    expect(whenText(new Date(2026, 10, 3, 2, 10).toISOString(), now)).toBe("11 月 3 日 02:10");
    expect(whenText(new Date(2025, 0, 9, 23, 5).toISOString(), now)).toBe(
      "2025 年 1 月 9 日 23:05",
    );
    expect(whenText("", now)).toBe("");
    expect(whenText("不是時間", now)).toBe("不是時間");
  });

  test("TestAdminPendingNoteNoRepeat：讀不到 main 時「還沒部署的改動」說原因在上面，不重複「讀不到 main 的最新版本」", () => {
    const main = { commit: "a", short: "a", subject: "最新", at: "" };
    expect(pendingNote({ main: null, pending: [], detail: "找不到 repo" })).toBe(T.pendingUnknown);
    expect(pendingNote({ main: null, pending: [], detail: "找不到 repo" })).not.toBe(T.mainNone);
    expect(pendingNote({ main, pending: [], detail: "" })).toBe(T.pendingNone);
    expect(pendingNote({ main, pending: [], detail: "本機的版本認不出" })).toBe(T.pendingUnknown);
    expect(pendingNote({ main, pending: [main], detail: "抓不到最新的 main" })).toBe("");
  });

  test("TestAdminConfirmNameSpacingAndPlain：確認句的英文名字前補空白、中文名字不補；確認句不出現程式碼的反引號", () => {
    expect(fill(T.deployConfirm, { name: nameAfterCJK("Helper") })).toContain("給 Helper，");
    expect(fill(T.deployConfirm, { name: nameAfterCJK("阿媞米斯") })).toContain("給阿媞米斯，");
    for (const s of [T.deployConfirm, T.nightlyConfirm, T.stableConfirm]) {
      expect(s).not.toContain("`");
    }
  });

  test("TestAdminHomeDetailHiddenWhenRunning：開發家開著時不再多一列「說明：開發家開著」，沒開時照樣說原因", () => {
    expect(homeDetail({ state: "running", detail: "開發家開著" })).toBe("");
    expect(homeDetail({ state: "stopped", detail: "開發家沒有開" })).toBe("開發家沒有開");
  });
});

// 開發家卡片的標題不能跟第一列「狀態」同一個字（ADM-33）。
test("TestDevHomeCardTitleIsNotState", () => {
  expect(T.homeCard).not.toBe(T.homeState);
});

describe("按鈕開單並開工（DEV-124）", () => {
  const req = { title: "部署 abc 到 mini、office", body: "內文", agent: "admin" };

  // 新開就是進行中的單不會開工作 session，要從待做改成進行中才真的開工。
  test("先開成待做，再改進行中", async () => {
    const tasks = {
      create: vi.fn().mockResolvedValue({ task: { id: "DEV-1" } }),
      update: vi.fn().mockResolvedValue({}),
    };
    await expect(createAndStart(tasks, req)).resolves.toEqual({ id: "DEV-1" });
    expect(tasks.create).toHaveBeenCalledWith({ ...req, status: "idea" });
    expect(tasks.update).toHaveBeenCalledWith({ id: "DEV-1", status: "doing" });
  });

  // 單已經開好：不能當成沒開成（再按會開出重複單），要回單號並說沒開工。
  test("開工失敗：回單號與原因", async () => {
    const tasks = {
      create: vi.fn().mockResolvedValue({ task: { id: "DEV-2" } }),
      update: vi.fn().mockRejectedValue(new Error("連不上")),
    };
    await expect(createAndStart(tasks, req)).resolves.toEqual({
      id: "DEV-2",
      notStarted: "連不上",
    });
  });

  test("開單失敗：照樣丟出去，不去改狀態", async () => {
    const tasks = {
      create: vi.fn().mockRejectedValue(new Error("不行")),
      update: vi.fn(),
    };
    await expect(createAndStart(tasks, req)).rejects.toThrow("不行");
    expect(tasks.update).not.toHaveBeenCalled();
  });
});
