import { defineConfig } from "@playwright/test";

// e2e 用 Playwright 內建的輕量無頭 Chromium（chromium headless shell）：不給 channel、headless（預設）時
// 1.59 會自己選 chromium-headless-shell（lib/server/chromium/chromium.js 的 getExecutableName）。
// 它沒有 GPU 行程、沒有 GoogleUpdater，記憶體比完整版 Google Chrome 小很多（T3，作者 2026-10-28）。
// 第一次要下載一次：`make e2e-install`（= playwright install --only-shell chromium，存在 ~/Library/Caches/ms-playwright）。
// 以前用完整版 Chrome 時要 e2e/chrome-quiet.sh 包住它，不然 GoogleUpdater 會抓著管線讓 worker 卡住；headless shell 沒有，已拿掉。
// 服務由 e2e/fixtures.ts 的 worker-scoped fixture 起：每個 worker 一個自己的暫存家＋假引擎＋隨機 port，
// baseURL 也由它提供（所以沒有 globalSetup／globalTeardown）。

const mobile = {
  viewport: { width: 390, height: 844 },
  isMobile: true,
  hasTouch: true,
  deviceScaleFactor: 2,
};
const desktop = { viewport: { width: 1280, height: 800 } };

// 手機 project 只跑這些 spec（T3，作者 2026-10-28「e2e 減量」）；其餘的 spec 只跑桌機（desktop project）。
// 判斷規則：spec 裡只要有「手機才有的行為或版面斷言」就列進來——
//   - 用 `isMobile` 分支走不同的操作或斷言（底部選單的「更多」抽屜、手機設定的兩層分頁、搜尋鈕、
//     卡片就地回答、角色區收合、手機的篩選小卡列、手機的頁首數字…）；
//   - 有 `info.skip(!isMobile, …)` 的手機專屬測試（不在清單裡這些測試會在桌機被跳過、等於消失）；
//   - 有用 viewportSize 對手機尺寸做斷言。
// 不列的：isMobile 只用來幫截圖檔命名（policy）、或只是換一個打開指令面板的方法而且手機的開法已在 palette.spec 驗過（reminders）。
// 新增 spec 時照同一條規則：寫了 isMobile 分支或手機專屬測試，就要加進這份清單，不然手機的斷言不會跑。
// 驗證清單沒漏：`pnpm exec playwright test --list --project=desktop` 裡 skip(!isMobile) 的測試所在的 spec 都要在清單裡。
const mobileSpecs = [
  "**/agent-filter.spec.ts",
  "**/back.spec.ts",
  "**/chat.spec.ts",
  "**/engine-stuck.spec.ts",
  "**/fitness.spec.ts",
  "**/goals.spec.ts",
  "**/home-stuck.spec.ts",
  "**/knowledge.spec.ts",
  "**/layout-stable.spec.ts",
  "**/messages.spec.ts",
  "**/offline.spec.ts",
  "**/overview.spec.ts",
  "**/packages.spec.ts",
  "**/palette.spec.ts",
  "**/persona.spec.ts",
  "**/phone.spec.ts",
  "**/rules.spec.ts",
  "**/settings-tabs.spec.ts",
  "**/settings.spec.ts",
  "**/shell.spec.ts",
  "**/tasks.spec.ts",
  "**/welcome.spec.ts",
];

// 下班時間（平日 18:30～隔天 08:00、整個週末，本機時間）機器比較閒，可以開到 3 個 worker（作者 2026-10-28）；
// 其他時間 2 個。EVERGENIE_E2E_WORKERS 可以直接指定（例：白天機器剛好沒人用）。
function e2eWorkers(now = new Date()): number {
  const fixed = Number(process.env.EVERGENIE_E2E_WORKERS);
  if (fixed > 0) return fixed;
  const day = now.getDay();
  const minutes = now.getHours() * 60 + now.getMinutes();
  const weekend = day === 0 || day === 6;
  const offHours = minutes >= 18 * 60 + 30 || minutes < 8 * 60;
  return weekend || offHours ? 3 : 2;
}

export default defineConfig({
  testDir: "./e2e",
  testMatch: "**/*.spec.ts",
  // 檔案之間平行（每個 worker 有自己的家）；同一檔內照順序跑，因為有些測試會依賴前一個造的資料。
  fullyParallel: false,
  // 上班時間最多 2 個 worker、下班時間 3 個（每個 worker 一組 serve＋假引擎＋瀏覽器，同時有好幾個 session 在跑時記憶體吃不消，T3）；
  // EVERGENIE_CPU（Makefile 設的並行數上限）比較小就用它（T2 第 1 條）。
  workers: Math.min(e2eWorkers(), Number(process.env.EVERGENIE_CPU) || Number.POSITIVE_INFINITY),
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: [["list"]],
  // service worker 預設擋掉（WEB-185）：worker 有 fetch 之後，page.route 攔不到經過它的請求；
  // 測 worker 本身的（離線頁、TestManifestAndServiceWorker）用 test.use({ serviceWorkers: "allow" }) 打開。
  use: { trace: "off", serviceWorkers: "block" },
  // 截圖有自己的 project（worker 不會跟其他 project 共用）：它的家是乾淨的，只有截圖自己造的資料。
  projects: [
    { name: "mobile-shots", testMatch: "**/shots*.spec.ts", use: mobile },
    { name: "desktop-shots", testMatch: "**/shots*.spec.ts", use: desktop },
    { name: "mobile", testMatch: mobileSpecs, use: mobile },
    { name: "desktop", testIgnore: ["**/shots*.spec.ts"], use: desktop },
  ],
});
