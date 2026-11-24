import { type ChildProcess, spawn } from "node:child_process";
import {
  copyFileSync,
  cpSync,
  mkdirSync,
  mkdtempSync,
  openSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import http from "node:http";
import net from "node:net";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { test as base, expect, type Page } from "@playwright/test";
import { type ServeProxy, startServeProxy, TEST_OWNER_LOGIN } from "./serve-proxy";

export type { ServeProxy };
export { expect, TEST_OWNER_LOGIN };

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../../../..");

export interface Seed {
  /** 透過核心的 unix socket 直接呼叫 API（造資料用；看板自己不開 notify.send）。 */
  api: <T = unknown>(route: string, body: unknown) => Promise<T>;
  /** 造一則「要你同意」的通知，回它的編號；title 要每個測試獨一無二。 */
  approve: (title: string, agent?: string) => Promise<string>;
  /** 造一張「等你驗收」的任務單，回它的編號。 */
  reviewTask: (title: string, agent?: string) => Promise<string>;
  /** 造一張「進行中」的任務單，回它的編號。 */
  doingTask: (title: string, agent?: string) => Promise<string>;
  /** 造一張指定狀態（idea 待做、waiting_external 等外部…）的任務單，回它的編號。 */
  statusTask: (
    title: string,
    status: "idea" | "doing" | "waiting_external",
    agent?: string,
  ) => Promise<string>;
  /** 造一張「等你決定」的任務單（秘書有問題要問你），回它的編號。 */
  askTask: (title: string, ask: string, agent?: string) => Promise<string>;
  /** 造一個待辦；沒給時間就不送 `at`（伺服器套預設），回它的編號。 */
  reminder: (content: string, at?: Date) => Promise<string>;
  /** 目前伺服器算的 waiting_total。 */
  waitingTotal: () => Promise<number>;
  /**
   * 把這個家所有綁單號的工作 session 收乾淨：開著的關掉、排隊中的也取消，回來時保證沒有任何一個開著或排隊。
   * 同一個 worker 共用一個家，前面的測試留下的工作 session 會佔 work_max（預設 3）的名額，
   * 後面要開工作 session 的測試（例如「開專屬對話」）就被排隊、第一句單號永遠不會送出；要開工作 session 的測試開頭與收尾都呼叫它。
   */
  closeAllWork: () => Promise<void>;
}

function unixPost<T>(socket: string, route: string, body: unknown): Promise<T> {
  return new Promise((resolve, reject) => {
    const data = JSON.stringify(body);
    const req = http.request(
      {
        socketPath: socket,
        path: route,
        method: "POST",
        headers: { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(data) },
      },
      (res) => {
        let text = "";
        res.on("data", (c) => {
          text += c;
        });
        res.on("end", () => {
          if ((res.statusCode ?? 500) >= 300)
            reject(new Error(`${route} → ${res.statusCode} ${text}`));
          else resolve(JSON.parse(text) as T);
        });
      },
    );
    req.on("error", reject);
    req.end(data);
  });
}

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once("error", reject);
    srv.listen(0, "127.0.0.1", () => {
      const { port } = srv.address() as net.AddressInfo;
      srv.close(() => resolve(port));
    });
  });
}

function replaceOnce(text: string, from: string, to: string): string {
  if (!text.includes(from)) throw new Error(`測試家的 home.toml 找不到「${from}」`);
  return text.replace(from, to);
}

async function post(
  base: string,
  route: string,
  body: unknown,
): Promise<{ status: number; json: unknown }> {
  const res = await fetch(`${base}${route}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return { status: res.status, json: await res.json().catch(() => null) };
}

async function waitFor(
  what: string,
  timeoutMs: number,
  probe: () => Promise<boolean>,
  log: string,
) {
  const deadline = Date.now() + timeoutMs;
  let last: unknown;
  while (Date.now() < deadline) {
    try {
      if (await probe()) return;
    } catch (err) {
      last = err;
    }
    await new Promise((r) => setTimeout(r, 300));
  }
  const tail = readFileSync(log, "utf8").split("\n").slice(-40).join("\n");
  throw new Error(
    `等不到${what}（${timeoutMs / 1000} 秒）：${String(last ?? "")}\nserve.log 最後幾行：\n${tail}`,
  );
}

/** 每個 worker 自己的服務：暫存家、空的 port、`evergenie serve`。 */
export interface HgServer {
  base: string;
  home: string;
  tmp: string;
  log: string;
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

// 起一組 e2e 用的服務：fixture 家複製到 /tmp/hg-e2e-<亂數>/home，帳號換成假引擎、常駐 session 自動啟動、
// web.port 換成空的 port；不碰 8850（舊看板）。port 是先找再用，所以服務立刻結束時換一個 port 重試。
// editToml 給兩個家的 e2e 改設定（[runtime] 身分、[web] 的多台設定），在換好 port 等之後套用。
// prepare 在服務起來之前改暫存的家（例：拿掉名冊，做出「還沒有任何秘書」的新家）；residents 是要等幾位常駐秘書閒下來（空的家是 0）。
async function startServer(
  editToml: (text: string) => string = (t) => t,
  prepare: (home: string) => void = () => {},
  residents = 2,
): Promise<{ server: HgServer; stop: () => Promise<void> }> {
  const bin = path.join(repo, "bin", "evergenie");
  const tmp = mkdtempSync("/tmp/hg-e2e-");
  const home = path.join(tmp, "home");
  cpSync(path.join(repo, "cmd", "evergenie", "testdata", "home"), home, { recursive: true });
  mkdirSync(path.join(home, "runtime"), { recursive: true });

  // 設定頁的「權限」分頁讀 settings/trust.toml：fixture 家沒有這個檔，這裡寫一份假的（只在暫存的家）。
  mkdirSync(path.join(home, "settings"), { recursive: true });
  writeFileSync(
    path.join(home, "settings", "trust.toml"),
    [
      "schema = 1",
      "[[repo]]",
      'path = "/work/e2e-demo"',
      'remote = "git@example.test:demo/e2e-demo.git"',
      "push = false",
      'writers = ["code", "life"]',
      "[[repo]]",
      'path = "/work/e2e-docs"',
      "push = true",
      'writers = ["life"]',
      "[approve]",
      'actions = ["commit", "run_tests"]',
      "[deny]",
      'actions = ["delete", "spend", "send_external"]',
      "",
    ].join("\n"),
  );

  // 角色的圖與影片（REQ-156）：code（builder）有圖也有影片，life（helper）只有圖（驗「沒有影片就退回圖」）。
  // 素材在 e2e/fixtures/，是 ffmpeg 產生的測試圖樣，不是真的角色。
  const fixtures = path.join(here, "fixtures");
  const avatars = path.join(home, "personas", "avatars");
  mkdirSync(avatars, { recursive: true });
  copyFileSync(path.join(fixtures, "persona.png"), path.join(avatars, "builder.png"));
  copyFileSync(path.join(fixtures, "persona.mp4"), path.join(avatars, "builder.mp4"));
  copyFileSync(path.join(fixtures, "persona.png"), path.join(avatars, "helper.png"));

  // 套件頁面（P6-2a）：示範套件放進 packages/mine/demo/，核心啟動時掃到、看板編譯它的頁面（REQ-106、114、140）。
  cpSync(path.join(fixtures, "packages", "demo"), path.join(home, "packages", "mine", "demo"), {
    recursive: true,
  });

  prepare(home);

  const toml = path.join(home, "home.toml");
  const original = readFileSync(toml, "utf8");
  const claudeConfig = path.join(tmp, "claude-config");
  const log = path.join(tmp, "serve.log");
  const fd = openSync(log, "a");

  let child: ChildProcess | undefined;
  let base = "";
  for (let attempt = 1; attempt <= 3; attempt++) {
    const port = await freePort();
    let text = replaceOnce(original, "port = 18850", `port = ${port}`);
    // 帳號資料夾明確指到暫存區（DEV-157）：測試家不碰這台電腦真的 Claude 設定（環境裡的 CLAUDE_CONFIG_DIR 也換掉，見下面）。
    text = replaceOnce(
      text,
      'engine = "test-engine"',
      `engine = "fake"\n[accounts.main.engine_config]\nconfig_dir = ${JSON.stringify(claudeConfig)}`,
    );
    text = replaceOnce(text, "autostart = false", "autostart = true");
    // 介面模式（P6-6）：fixture 家沒寫 mode（預設 simple）；e2e 的家用完整模式，既有的設定頁測試照舊，
    // 簡單模式的測試自己切換、結束時切回。對話的顯示方式同理：e2e 的家用工程師模式，預設模式的測試自己切（WEB-160）。
    text = editToml(`mode = "full"\nchat_display = "engineer"\n${text}`);
    writeFileSync(toml, text);
    child = spawn(bin, ["serve", "--home", home], {
      env: {
        ...process.env,
        EVERGENIE_ENGINE_BIN_DIR: path.join(repo, "bin"),
        EVERGENIE_HOME: home,
        EVERGENIE_API_SOCKET: path.join(home, "runtime/sockets/api.sock"),
        EVERGENIE_AGENT: "",
        // 從 AI 的 session 跑 e2e 時，環境裡是這台電腦真的 Claude 設定：換成暫存區的（DEV-157）。
        CLAUDE_CONFIG_DIR: claudeConfig,
        // 測試家不碰這台電腦真的 Tailscale（DEV-153）：只認暫存區裡這個路徑，平常沒有檔＝當成沒裝；
        // phone.spec 在這裡放一支假的 tailscale。
        EVERGENIE_TAILSCALE_BIN: path.join(tmp, "tailscale", "tailscale"),
      },
      stdio: ["ignore", fd, fd],
    });
    if (!child.pid) throw new Error("serve 沒有起來");
    base = `http://127.0.0.1:${port}`;
    // 剛起來就結束（多半是 port 被別的 worker 搶走）：換一個 port 再試。
    await new Promise((r) => setTimeout(r, 400));
    if (alive(child.pid)) break;
    if (attempt === 3) throw new Error(`serve 連續結束\n${readFileSync(log, "utf8").slice(-2000)}`);
  }
  const pid = (child as ChildProcess).pid as number;

  const stop = async () => {
    if (alive(pid)) {
      process.kill(pid, "SIGINT");
      const deadline = Date.now() + 20_000;
      while (alive(pid) && Date.now() < deadline) await new Promise((r) => setTimeout(r, 200));
      if (alive(pid)) process.kill(pid, "SIGKILL");
    }
    // EVERGENIE_E2E_KEEP=1 時保留暫存家，方便看 serve.log。
    if (!process.env.EVERGENIE_E2E_KEEP) rmSync(tmp, { recursive: true, force: true });
  };

  try {
    await waitFor(
      "看板服務",
      30_000,
      async () => (await post(base, "/api/web/get_health", {})).status === 200,
      log,
    );
    await waitFor(
      "兩位常駐秘書都閒著",
      30_000,
      async () => {
        const { json } = await post(base, "/api/session/list", {});
        const sessions = (
          (json as { sessions?: { kind: string; state: string }[] })?.sessions ?? []
        ).filter((s) => s.kind === "resident");
        return sessions.length >= residents && sessions.every((s) => s.state === "idle");
      },
      log,
    );
  } catch (err) {
    await stop();
    throw err;
  }
  return { server: { base, home, tmp, log }, stop };
}

// makeSeed 是一個家的造資料工具（走那個家的 unix socket；waitingTotal 直接打後端 http）。
function makeSeed(server: HgServer): Seed {
  const socket = path.join(server.home, "runtime", "sockets", "api.sock");
  const api = <T>(route: string, body: unknown) => unixPost<T>(socket, route, body);
  return {
    api,
    approve: async (title, agent = "code") => {
      const res = await api<{ notification: { id: string } }>("/api/notify/send", {
        agent,
        category: "approve",
        title,
        context: `${title} 的說明`,
        todo: "要不要同意這件事？",
        options: ["同意", "不要"],
      });
      return res.notification.id;
    },
    reviewTask: async (title, agent = "code") => {
      const created = await api<{ task: { id: string } }>("/api/tasks/create", {
        criteria: "測試驗收條件",
        title,
        agent,
        by: { name: agent },
        body: `${title} 的內容`,
      });
      const id = created.task.id;
      await api("/api/tasks/submit", {
        id,
        result: `${title} 做完了`,
        verify: "verified",
        verify_how: "跑過測試",
        proposed: "done",
        by: { name: agent },
      });
      return id;
    },
    doingTask: async (title, agent = "code") => {
      const created = await api<{ task: { id: string } }>("/api/tasks/create", {
        criteria: "測試驗收條件",
        title,
        agent,
        by: { name: agent },
        body: `${title} 的內容`,
      });
      return created.task.id;
    },
    statusTask: async (title, status, agent = "code") => {
      const created = await api<{ task: { id: string } }>("/api/tasks/create", {
        criteria: "測試驗收條件",
        title,
        agent,
        status,
        by: { name: agent },
        body: `${title} 的內容`,
      });
      return created.task.id;
    },
    askTask: async (title, ask, agent = "code") => {
      const created = await api<{ task: { id: string } }>("/api/tasks/create", {
        criteria: "測試驗收條件",
        title,
        agent,
        ask,
        status: "waiting_user",
        by: { name: agent },
        body: `${title} 的內容`,
      });
      return created.task.id;
    },
    reminder: async (content, at) => {
      const created = await api<{ reminder: { id: string } }>("/api/reminders/create", {
        content,
        owner: "life",
        follow: at ? "remind" : "none",
        ...(at ? { at: at.toISOString() } : {}),
        by: { name: "owner(web)" },
      });
      return created.reminder.id;
    },
    waitingTotal: async () => {
      const { json } = await post(server.base, "/api/web/get_overview", {});
      return (json as { waiting_total: number }).waiting_total;
    },
    closeAllWork: async () => {
      interface Work {
        name: string;
        kind: string;
        task?: string;
        state: string;
        queued_at?: string;
      }
      const list = async () =>
        ((await api<{ sessions?: Work[] }>("/api/session/list", {})).sessions ?? []).filter(
          (s) => s.kind === "work" && s.task,
        );
      const queued = (s: Work) => !!s.queued_at && !s.queued_at.startsWith("0001-");
      const leftover = (all: Work[]) => all.filter((s) => s.state !== "stopped" || queued(s));
      // 收一輪：先取消排隊的、再關開著的（先關開著的會空出名額，核心對帳時就把排隊的開起來）。
      // 關掉的工作 session 被標成「手動關掉」，單還是 doing 也不會自己接回。回剩下的名單（收之前看到的）。
      const sweep = async () => {
        const left = leftover(await list());
        for (const s of left.filter(queued)) await api("/api/session/close", { name: s.name });
        for (const s of left.filter((x) => !queued(x) && x.state !== "stopped"))
          await api("/api/session/close", { name: s.name });
        return left.map((s) => s.name);
      };
      // 收到連續兩次看到都是空的才算數：核心對帳或排隊接回正在開一個 session 的當下（記錄還是 stopped、已不算排隊）
      // 看起來是空的，下一刻它才開起來；隔一小段再看一次，開起來的就會被下一輪收掉。
      await expect
        .poll(
          async () => {
            if ((await sweep()).length > 0) return ["swept"];
            await new Promise((r) => setTimeout(r, 400));
            return await sweep();
          },
          { timeout: 20_000 },
        )
        .toEqual([]);
    },
  };
}

export const test = base.extend<{ seed: Seed }, { hgServer: HgServer }>({
  // 每個 worker 一個自己的家與服務（資料不會跟別的 worker 互相干擾）；worker 結束時 SIGINT 並刪暫存家。
  hgServer: [
    // biome-ignore lint/correctness/noEmptyPattern: Playwright 要求 fixture 參數使用解構。
    async ({}, use) => {
      const { server, stop } = await startServer();
      try {
        await use(server);
      } finally {
        await stop();
      }
    },
    { scope: "worker", timeout: 120_000 },
  ],
  // 輕量無頭 Chromium（headless shell）的 Notification.permission 預設是 "denied"（完整版 Chrome 是 "default"），
  // 設定頁會變成「這個瀏覽器封鎖了通知」而不是「推播金鑰還沒設定」。這裡把 "denied" 當一般瀏覽器的預設 "default"；
  // 需要別的狀態的測試（例如 TestDevicesEnableTestDisable）自己再 defineProperty 蓋掉。
  context: async ({ context }, use) => {
    await context.addInitScript(() => {
      if (typeof Notification !== "undefined" && Notification.permission === "denied") {
        Object.defineProperty(Notification, "permission", {
          get: () => "default",
          configurable: true,
        });
      }
    });
    await use(context);
  },
  // baseURL 由這個 worker 的服務提供。
  baseURL: async ({ hgServer }, use) => {
    await use(hgServer.base);
  },
  seed: async ({ hgServer }, use) => {
    await use(makeSeed(hgServer));
  },
});

// ---- 開發家（[runtime] env = "dev"）----------------------------------------------------------------
// 只有 import devTest 的 spec 才起：worker fixture 跟 test 不同，Playwright 給它自己的 worker（跟兩個家的做法一樣），
// 其他 spec 的家照舊是正式家。給「只在開發家出現」的功能用（例：衣服的「升成官方」，CMI-16）。
export const devTest = test.extend<object, { hgServer: HgServer }>({
  hgServer: [
    // biome-ignore lint/correctness/noEmptyPattern: Playwright 要求 fixture 參數使用解構。
    async ({}, use) => {
      const { server, stop } = await startServer((text) => {
        if (/^\[runtime\]$/m.test(text))
          throw new Error("測試家的 home.toml 已經有 [runtime]，開發家的設定要改寫法");
        return `${text}\n[runtime]\nenv = "dev"\n`;
      });
      try {
        await use(server);
      } finally {
        await stop();
      }
    },
    { scope: "worker", timeout: 120_000 },
  ],
});

// ---- 沒人用過的新家 ------------------------------------------------------------------------------------
// 跟 test 一樣的正式家，但自己一個 worker：別的 spec 不會先在裡面約習慣、放衣服。
// 給「每個家只發生一次」的事用（例：家裡第一個習慣的起手禮，HAB-20）。同一支 spec 裡的測試照順序共用這個家。
export const freshHomeTest = test.extend<object, { hgServer: HgServer }>({
  hgServer: [
    // biome-ignore lint/correctness/noEmptyPattern: Playwright 要求 fixture 參數使用解構。
    async ({}, use) => {
      const { server, stop } = await startServer();
      try {
        await use(server);
      } finally {
        await stop();
      }
    },
    { scope: "worker", timeout: 120_000 },
  ],
});

// ---- 還沒有任何秘書的新家 ------------------------------------------------------------------------------
// 跟 test 一樣的正式家，但名冊是空的（剛裝好、還沒請任何秘書）：第一次打開看板會先到歡迎頁（WEB-176）。
// 自己一個 worker；同一支 spec 裡的測試照順序共用這個家（請了秘書之後就不再是空的）。
export const emptyHomeTest = test.extend<object, { hgServer: HgServer }>({
  hgServer: [
    // biome-ignore lint/correctness/noEmptyPattern: Playwright 要求 fixture 參數使用解構。
    async ({}, use) => {
      const { server, stop } = await startServer(
        undefined,
        (home) => rmSync(path.join(home, "vault", "agents"), { recursive: true, force: true }),
        0,
      );
      try {
        await use(server);
      } finally {
        await stop();
      }
    },
    { scope: "worker", timeout: 120_000 },
  ],
});

// ---- 兩個家（S3 看板連多台）----------------------------------------------------------------------
// 只有 import twoHomesTest 的 spec 才起第二個家：它的 worker fixture 跟 test 不同，Playwright 會給它自己的 worker，
// 其他 spec 的 worker（hgServer 一個家）照舊、速度不變。

/** 兩個家的其中一個：後端（server）＋前面模擬 tailscale serve 的 https proxy；看板網址是 url（proxy 的 origin）。 */
export interface E2EHome {
  /** get_mode 回的 label（[runtime] owner-machine）。 */
  label: string;
  /** 看板的 https origin（經過 proxy），也是這個家的 web.public_url。 */
  url: string;
  proxy: ServeProxy;
  server: HgServer;
  /** 在這個家造資料（走它自己的 unix socket，不經過 proxy、不經過瀏覽器）。 */
  seed: Seed;
}

export interface TwoHomes {
  /** 家 A：瀏覽器開的看板；[[web.sites]] 放 B 的網址。 */
  a: E2EHome;
  /** 家 B：被 A 的看板呼叫；allow_boards 有 A 的 origin、owner_login＝TEST_OWNER_LOGIN。 */
  b: E2EHome;
}

const E2E_OWNER = "amy";

// 兩個家的設定：[web] 的鍵接在 [web] 標頭那行後面（不管 fixture 的 [web] 之後還有哪些鍵）；
// [[web.sites]] 與 [runtime] 身分（不同 machine → 不同 label）接在檔尾。fixture 家已經有 [runtime] 就直接報錯，免得寫成重複的表。
function multiHomeToml(
  machine: string,
  web: string[],
  sites: string[] = [],
): (text: string) => string {
  return (text) => {
    if (/^\[runtime\]$/m.test(text))
      throw new Error("測試家的 home.toml 已經有 [runtime]，兩個家的設定要改寫法");
    let out = replaceOnce(text, "[web]\n", `[web]\n${web.join("\n")}\n`);
    for (const url of sites) out += `\n[[web.sites]]\nurl = "${url}"\n`;
    return `${out}\n[runtime]\nowner = "${E2E_OWNER}"\nmachine = "${machine}"\n`;
  };
}

async function startTwoHomes(): Promise<{ homes: TwoHomes; stop: () => Promise<void> }> {
  const certs = mkdtempSync("/tmp/hg-e2e-proxy-");
  const stops: (() => Promise<void>)[] = [];
  const stop = async () => {
    for (const s of stops.reverse()) await s().catch(() => {});
    rmSync(certs, { recursive: true, force: true });
  };
  try {
    // proxy 先起：兩個家的設定要互相寫對方的網址。後端還沒起來前 proxy 不轉（直接斷線）。
    const proxyA = await startServeProxy(path.join(certs, "a"));
    stops.push(proxyA.close);
    const proxyB = await startServeProxy(path.join(certs, "b"));
    stops.push(proxyB.close);
    // 兩個家一起起（省時間）；其中一個失敗時，起好的那個也要收掉，不留 serve 與暫存家。
    const [ra, rb] = await Promise.allSettled([
      startServer(multiHomeToml("e2e-a", [`public_url = "${proxyA.url}"`], [proxyB.url])),
      startServer(
        multiHomeToml("e2e-b", [
          `public_url = "${proxyB.url}"`,
          `owner_login = "${TEST_OWNER_LOGIN}"`,
          `allow_boards = ["${proxyA.url}"]`,
        ]),
      ),
    ]);
    for (const r of [ra, rb]) if (r.status === "fulfilled") stops.push(r.value.stop);
    if (ra.status === "rejected") throw ra.reason;
    if (rb.status === "rejected") throw rb.reason;
    const [a, b] = [ra.value, rb.value];
    proxyA.upstream = a.server.base;
    proxyB.upstream = b.server.base;
    const home = (label: string, proxy: ServeProxy, server: HgServer): E2EHome => ({
      label,
      url: proxy.url,
      proxy,
      server,
      seed: makeSeed(server),
    });
    return {
      homes: {
        a: home(`${E2E_OWNER}-e2e-a`, proxyA, a.server),
        b: home(`${E2E_OWNER}-e2e-b`, proxyB, b.server),
      },
      stop,
    };
  } catch (err) {
    await stop();
    throw err;
  }
}

/**
 * 兩個家的 e2e（S3）：`import { twoHomesTest as test } from "./fixtures"`。
 * baseURL 是家 A 經過 proxy 的 https 網址、`seed` 造的是 A 的資料；B 用 `homes.b.seed`。
 * 每個測試開始時兩個 proxy 都回到預設（照常轉、補 TEST_OWNER_LOGIN），測試裡可以改 `homes.b.proxy.login`／`mode`
 * （mode 切成 bad-gateway／reset 時，開著的連線——串流、即時更新——也一起斷掉）。
 */
export const twoHomesTest = test.extend<{ homes: TwoHomes }, { twoHomes: TwoHomes }>({
  twoHomes: [
    // biome-ignore lint/correctness/noEmptyPattern: Playwright 要求 fixture 參數使用解構。
    async ({}, use) => {
      const { homes, stop } = await startTwoHomes();
      try {
        await use(homes);
      } finally {
        await stop();
      }
    },
    { scope: "worker", timeout: 180_000 },
  ],
  // auto：每個測試（就算只用 page）開始前都把兩個 proxy 還原，前一個測試切成 reset 不會留到下一個。
  homes: [
    async ({ twoHomes }, use) => {
      for (const h of [twoHomes.a, twoHomes.b]) {
        h.proxy.login = TEST_OWNER_LOGIN;
        h.proxy.mode = "up";
        h.proxy.forwarded.length = 0;
      }
      await use(twoHomes);
    },
    { auto: true },
  ],
  // 自簽憑證：只有這組測試接受憑證錯誤（模擬的 serve 是 https）。
  ignoreHTTPSErrors: [true, { option: true }],
  baseURL: async ({ twoHomes }, use) => {
    await use(twoHomes.a.url);
  },
  seed: async ({ twoHomes }, use) => {
    await use(twoHomes.a.seed);
  },
});

/** 截圖存到 $EVERGENIE_SHOTS_DIR（預設 /tmp/hg-shots），不進 git。 */
export const shotsDir = () => process.env.EVERGENIE_SHOTS_DIR || "/tmp/hg-shots";

export async function shot(page: Page, name: string) {
  mkdirSync(shotsDir(), { recursive: true });
  await page.screenshot({ path: path.join(shotsDir(), `${name}.png`) });
}
