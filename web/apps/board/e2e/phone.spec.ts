import { chmodSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import type { Page } from "@playwright/test";
import { expect, type HgServer, shotsDir, test } from "./fixtures";

// 設定 → 手機連線（DEV-153）：四步把看板接到手機。
// 服務只認暫存區裡的假 tailscale（fixtures.ts 的 EVERGENIE_TAILSCALE_BIN），不碰這台電腦真的 Tailscale。
// 這些測試會讓服務改家的 home.toml（public_url、推播金鑰的位置）：結束時還原，同一個 worker 的下一個 spec 不受影響。

const DNS = "e2e-mac.example.ts.net";
const URL_ = `https://${DNS}`;
const ENABLE = "https://login.tailscale.com/f/serve?node=nE2ECNTRL";

// 假的 tailscale：已登入；serve --bg 把看板掛上去（state/serve-blocked 在的時候像新帳號一樣被後台擋住）；
// funnel … off 把公開拿掉；其他一律失敗（絕不會「開」Funnel）。每次呼叫記進 calls.log。
const fakeTailscale = `#!/bin/sh
d="$(dirname "$0")/state"
echo "$*" >>"$d/calls.log"
hp="${DNS}:443"
case "$1 $2" in
"status --json") echo '{"BackendState":"Running","CertDomains":["${DNS}"],"Self":{"DNSName":"${DNS}.","UserID":7},"User":{"7":{"LoginName":"owner@example.com"}}}' ;;
"serve status") if [ -f "$d/serve.json" ]; then cat "$d/serve.json"; else echo "{}"; fi ;;
"serve get-config") echo '{"version":"0.0.1"}' ;;
"serve --bg")
  if [ -f "$d/serve-blocked" ]; then printf 'Serve is not enabled on your tailnet.\\nTo enable, visit:\\n\\n         ${ENABLE}\\n\\n'; exit 1; fi
  printf '{"TCP":{"443":{"HTTPS":true}},"Web":{"%s":{"Handlers":{"/":{"Proxy":"%s"}}}}}\\n' "$hp" "$4" >"$d/serve.json" ;;
"funnel --https=443")
  [ "$3" = "off" ] || { echo "假的 tailscale 不開 Funnel"; exit 1; }
  echo "{}" >"$d/serve.json" ;;
*) echo "假的 tailscale 不認得：$*"; exit 1 ;;
esac
`;

const dirOf = (server: HgServer) => path.join(server.tmp, "tailscale");
const stateOf = (server: HgServer, name: string) => path.join(dirOf(server), "state", name);

function install(server: HgServer) {
  mkdirSync(path.join(dirOf(server), "state"), { recursive: true });
  const bin = path.join(dirOf(server), "tailscale");
  writeFileSync(bin, fakeTailscale);
  chmodSync(bin, 0o755);
}

let homeToml = "";
test.beforeAll(({ hgServer: server }) => {
  homeToml = readFileSync(path.join(server.home, "home.toml"), "utf8");
});
test.afterAll(({ hgServer: server }) => {
  rmSync(dirOf(server), { recursive: true, force: true });
  writeFileSync(path.join(server.home, "home.toml"), homeToml);
});

const step = (page: Page, n: number) => page.getByTestId(`phone-step-${n}`);

async function noSideScroll(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  const wide = await page.evaluate(() =>
    Math.max(document.documentElement.scrollWidth, document.body.scrollWidth),
  );
  expect(wide, "不能左右捲").toBeLessThanOrEqual(page.viewportSize()?.width ?? 0);
}

// Guards: SET-10
test("TestPhoneTailscaleNotInstalled", async ({ page, hgServer: server }) => {
  rmSync(dirOf(server), { recursive: true, force: true });
  await page.goto("/settings/phone");
  await expect(step(page, 1)).toHaveAttribute("data-state", "todo");
  await expect(step(page, 1).getByRole("link", { name: "到 Tailscale 官網下載" })).toHaveAttribute(
    "href",
    "https://tailscale.com/download/mac",
  );
  await expect(page.getByRole("main")).not.toContainText("brew");
  await expect(page.getByTestId("phone-setup")).toHaveCount(0);
  await expect(page.getByTestId("phone-qr")).toHaveCount(0);
  await expect(page.getByTestId("phone-tailnet-note")).toHaveText(
    "跟你在同一個 Tailscale 網路裡的人，都能打開你的看板；不要把別人加進你的 Tailscale 網路。",
  );
  await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);
  await noSideScroll(page);
});

// 這台 Mac 本機開的看板：裝好登入 → 按鈕 → 後台還沒開 HTTPS 被接住 → 開好再按一次 → 網址與 QR code → 接到裝置推播。
// Guards: SET-11, SET-12
test("TestPhoneSetupOnThisMac @smoke", async ({
  page,
  hgServer: server,
  request,
  isMobile,
}, info) => {
  install(server);
  writeFileSync(stateOf(server, "serve-blocked"), "1");
  await page.goto("/settings/phone");
  await expect(step(page, 1)).toHaveAttribute("data-state", "done");
  await expect(step(page, 1)).toContainText("owner@example.com");
  await expect(step(page, 2)).toHaveAttribute("data-state", "todo");
  await expect(step(page, 3)).toHaveAttribute("data-state", "manual");

  // 新帳號：Tailscale 後台還沒開 HTTPS。白話說去哪按、給連結、按完再按一次。
  await page.getByTestId("phone-setup").click();
  const failed = page.getByTestId("phone-setup-failed");
  await expect(failed).toContainText("Tailscale 後台還沒打開 HTTPS");
  await expect(failed).toContainText("回來這裡再按一次");
  await expect(failed.getByRole("link", { name: "打開 Tailscale 的設定網頁" })).toHaveAttribute(
    "href",
    ENABLE,
  );
  await expect(step(page, 2)).toHaveAttribute("data-state", "todo");
  await noSideScroll(page);

  rmSync(stateOf(server, "serve-blocked"));
  await expect(page.getByTestId("phone-setup")).toHaveText("再做一次");
  await page.getByTestId("phone-setup").click();
  await expect(step(page, 2)).toHaveAttribute("data-state", "done");
  await expect(page.getByTestId("phone-url")).toHaveText(URL_);
  await expect(page.getByTestId("phone-qr")).toBeVisible();
  // 推播金鑰一起做好了：按鈕收起來。
  await expect(page.getByTestId("phone-setup")).toHaveCount(0);
  const { phone } = (await (
    await request.post(`${server.base}/api/web/get_phone`, { data: {} })
  ).json()) as { phone: { push_ready: boolean; mounted: boolean; url: string; local: boolean } };
  expect(phone).toMatchObject({ push_ready: true, mounted: true, url: URL_, local: true });
  // 絕不開 Funnel：假的 tailscale 收到的指令裡沒有 funnel。
  const calls = readFileSync(stateOf(server, "calls.log"), "utf8");
  expect(calls).toContain("serve --bg --https=443 http://127.0.0.1:");
  expect(calls).not.toContain("funnel");
  await noSideScroll(page);
  if (isMobile) {
    const box = await page.getByTestId("phone-next").boundingBox();
    expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
  }
  if (process.env.EVERGENIE_SHOTS_DIR) {
    mkdirSync(shotsDir(), { recursive: true });
    await page.screenshot({
      path: path.join(shotsDir(), `phone-${info.project.name}.png`),
      fullPage: true,
    });
  }

  // 重新整理還在；接到現有的「加到主畫面／開通知」那一頁。
  await page.reload();
  await expect(step(page, 2)).toHaveAttribute("data-state", "done");
  await page.getByTestId("phone-next").click();
  await expect(page).toHaveURL(/\/settings\/devices$/);

  // 設定精靈有一行連過來。
  await page.goto("/settings/setup");
  await page.getByTestId("setup-phone").getByRole("link", { name: "手機連線" }).click();
  await expect(page).toHaveURL(/\/settings\/phone$/);
});

// 看板被 Funnel 開到整個網路：每一頁上面都有紅色警告，手機連線頁有說明；本機按一下就關掉。
// Guards: SET-13
test("TestPhoneFunnelWarningAndClose", async ({ page, hgServer: server, seed }, info) => {
  install(server);
  const port = new URL(server.base).port;
  writeFileSync(
    stateOf(server, "serve.json"),
    JSON.stringify({
      TCP: { "443": { HTTPS: true } },
      Web: { [`${DNS}:443`]: { Handlers: { "/": { Proxy: `http://127.0.0.1:${port}` } } } },
      AllowFunnel: { [`${DNS}:443`]: true },
    }),
  );
  await page.goto("/");
  const banner = page.getByTestId("funnel-banner");
  await expect(banner).toContainText("你的看板現在整個網路都連得到");
  await expect(banner.getByRole("button", { name: "關掉對外公開" })).toBeVisible();
  await noSideScroll(page);
  await banner.getByRole("link", { name: "怎麼關掉" }).click();
  await expect(page).toHaveURL(/\/settings\/phone$/);
  const warn = page.getByTestId("phone-funnel");
  await expect(warn).toContainText("你的看板現在整個網路都連得到");
  await expect(warn).toContainText(URL_);
  await noSideScroll(page);
  if (process.env.EVERGENIE_SHOTS_DIR) {
    mkdirSync(shotsDir(), { recursive: true });
    await page.screenshot({
      path: path.join(shotsDir(), `phone-funnel-${info.project.name}.png`),
      fullPage: true,
    });
  }
  // 健檢（evergenie doctor）也是紅字。
  const doctor = JSON.stringify(await seed.api("/api/system/doctor", { groups: ["config"] }));
  expect(doctor).toContain("config.web_funnel");
  expect(doctor).toContain("你的看板現在整個網路都連得到");

  await page.goto("/settings/phone");
  await page.getByTestId("phone-close-funnel").click();
  await expect(page.getByTestId("phone-funnel")).toHaveCount(0);
  await expect(page.getByTestId("funnel-banner")).toHaveCount(0);
  expect(readFileSync(stateOf(server, "calls.log"), "utf8")).not.toMatch(/funnel(?!.* off)/);
  expect(
    JSON.stringify(await seed.api("/api/system/doctor", { groups: ["config"] })),
  ).not.toContain("config.web_funnel");
  // 關掉之後手機照樣連得到（看板還掛著）。
  await expect(step(page, 2)).toHaveAttribute("data-state", "done");
});

// 不是這台 Mac 本機開的看板（手機、別台：經 Tailscale 進來的請求一定帶 X-Forwarded-For）：只有狀態與說明，
// 沒有「讓手機連得到」的按鈕；直接打 API 也是 403，而且假的 tailscale 沒被叫去掛看板。
// Guards: WEB-187
test("TestPhoneSetupRefusedAwayFromThisMac", async ({ browser, hgServer: server, request }) => {
  install(server);
  rmSync(stateOf(server, "serve.json"), { force: true });
  rmSync(stateOf(server, "calls.log"), { force: true });
  const away = { "X-Forwarded-For": "192.0.2.10" };
  const context = await browser.newContext({ baseURL: server.base, extraHTTPHeaders: away });
  const page = await context.newPage();
  await page.goto("/settings/phone");
  await expect(step(page, 1)).toHaveAttribute("data-state", "done");
  await expect(page.getByTestId("phone-not-here")).toContainText("要在跑看板的那台 Mac 上按");
  await expect(page.getByTestId("phone-setup")).toHaveCount(0);
  await context.close();

  for (const headers of [
    away,
    { "X-Forwarded-For": "127.0.0.1", "X-Forwarded-Host": "127.0.0.1" },
    { "Tailscale-User-Login": "owner@example.com" },
    { Origin: "https://evil.example" },
    { Origin: "http://127.0.0.1:1" }, // 這台 Mac 上別的埠的網頁
  ]) {
    const res = await request.post(`${server.base}/api/web/phone_setup`, {
      data: { local: true, by: { name: "owner(web)" } },
      headers,
    });
    expect(res.status(), `phone_setup ${JSON.stringify(headers)}`).toBe(403);
  }
  // 「關掉對外公開」是變安全的動作，手機、別台也能關（SET-12）；別的網站跨站送來的照樣擋。
  const closeAway = await request.post(`${server.base}/api/web/phone_close_funnel`, {
    data: { local: true, by: { name: "owner(web)" } },
    headers: away,
  });
  expect(closeAway.status()).toBe(200);
  expect(((await closeAway.json()) as { phone: { local: boolean } }).phone.local).toBe(false);
  const closeCross = await request.post(`${server.base}/api/web/phone_close_funnel`, {
    data: { local: true, by: { name: "owner(web)" } },
    headers: { Origin: "https://evil.example" },
  });
  expect(closeCross.status()).toBe(403);
  const status = await request.post(`${server.base}/api/web/get_phone`, {
    data: { local: true },
    headers: away,
  });
  expect(((await status.json()) as { phone: { local: boolean } }).phone.local).toBe(false);
  const log = stateOf(server, "calls.log");
  const calls = existsSync(log) ? readFileSync(log, "utf8") : "";
  expect(calls).not.toContain("serve --bg");
  expect(calls).not.toMatch(/funnel(?!.* off)/); // 只可能有「關掉」，絕不會開
});
