import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync } from "node:fs";
import http from "node:http";
import https from "node:https";
import type net from "node:net";
import path from "node:path";

// e2e 用的「假 tailscale serve」（S3-E，2026-11-02）：兩個家的 e2e 每個家前面一個，模擬正式環境裡 Tailscale 服務轉進看板的那一段。
// 產品程式不為測試放行 http（code 2026-11-01 定），所以 proxy 對外是 https（自簽憑證，啟動時產生在暫存資料夾，
// Playwright 用 ignoreHTTPSErrors 接受）；不同 port＝不同 origin。照 S3-0a 量到的 serve 行為：
//   - 刪掉用戶端自己帶的 Tailscale-User-*（頁面偽造不了身分）；
//   - 補上 Tailscale-User-Login／-Name（預設是主人的測試 login；測試可以改成不補或補別人）；
//   - 加 X-Forwarded-For／-Host／-Proto；Host 原樣轉給後端（照 tailscale 原始碼：serve 的 reverse proxy 保留 In.Host；
//     S3-0a 只量了 X-Forwarded-* 與 Tailscale-User-*，Host 留給 S3-12 真機補驗）。
// 串流照原樣一段一段轉，不緩衝。

/** proxy 怎麼回應：up＝照常轉；bad-gateway＝後端掛了（serve 回 502）；reset＝整台連不上（連線直接斷掉）。 */
export type ProxyMode = "up" | "bad-gateway" | "reset";

/** proxy 轉給後端的一個請求（測試拿來看後端實際收到哪些標頭）。 */
export interface Forwarded {
  method: string;
  path: string;
  headers: http.OutgoingHttpHeaders;
}

export interface ServeProxy {
  /** 對外的 https origin（例 https://127.0.0.1:54321），就是這個家的看板網址。 */
  url: string;
  port: number;
  /** 補給後端的 Tailscale-User-Login；null＝不補（模擬 tag 身分的機器，沒有使用者標頭）。 */
  login: string | null;
  /** 切成 bad-gateway／reset 時，已經開著的連線（串流、即時更新）也一起斷掉；之後的新請求照 mode 回。 */
  mode: ProxyMode;
  /** 轉給後端的請求（最新的在後面，最多留 200 筆）。 */
  forwarded: Forwarded[];
  /** 後端（http://127.0.0.1:<port>）；起 proxy 時可以還不知道，家起來後再設。 */
  upstream: string;
  close: () => Promise<void>;
}

/** 預設補上的測試 login（兩個家的 e2e 把它寫進 B 的 web.owner_login）。 */
export const TEST_OWNER_LOGIN = "amy@e2e.test";

const MAX_FORWARDED = 200;

// 自簽憑證：用系統的 openssl 產生（macOS 內建；Homebrew 版也可以）。Playwright 忽略憑證錯誤，所以不用進信任清單。
function selfSigned(dir: string): { key: Buffer; cert: Buffer } {
  mkdirSync(dir, { recursive: true });
  const key = path.join(dir, "key.pem");
  const cert = path.join(dir, "cert.pem");
  try {
    execFileSync(
      "openssl",
      [
        "req",
        "-x509",
        "-newkey",
        "rsa:2048",
        "-nodes",
        "-days",
        "2",
        "-subj",
        "/CN=127.0.0.1",
        "-keyout",
        key,
        "-out",
        cert,
      ],
      { stdio: "pipe" },
    );
  } catch (err) {
    throw new Error(`產生 e2e 的自簽憑證失敗（要有 openssl 指令）：${String(err)}`);
  }
  return { key: readFileSync(key), cert: readFileSync(cert) };
}

/** 起一個 https proxy（127.0.0.1、隨機 port）；憑證放在 certDir。 */
export async function startServeProxy(certDir: string): Promise<ServeProxy> {
  const sockets = new Set<net.Socket>();
  let mode: ProxyMode = "up";
  const proxy: ServeProxy = {
    url: "",
    port: 0,
    login: TEST_OWNER_LOGIN,
    get mode() {
      return mode;
    },
    set mode(next: ProxyMode) {
      mode = next;
      if (next !== "up") for (const s of sockets) s.destroy();
    },
    forwarded: [],
    upstream: "",
    close: async () => {},
  };

  const server = https.createServer(selfSigned(certDir), (req, res) => {
    if (proxy.mode === "reset" || !proxy.upstream) {
      req.socket.destroy();
      return;
    }
    if (proxy.mode === "bad-gateway") {
      res.writeHead(502, { "Content-Type": "text/plain; charset=utf-8" });
      res.end("bad gateway");
      return;
    }
    const headers: http.OutgoingHttpHeaders = {};
    for (const [name, value] of Object.entries(req.headers)) {
      if (value === undefined) continue;
      if (name.toLowerCase().startsWith("tailscale-user-")) continue;
      headers[name] = value;
    }
    if (proxy.login) {
      headers["tailscale-user-login"] = proxy.login;
      headers["tailscale-user-name"] = proxy.login === TEST_OWNER_LOGIN ? "Amy (e2e)" : proxy.login;
    }
    headers["x-forwarded-for"] = req.socket.remoteAddress ?? "127.0.0.1";
    headers["x-forwarded-host"] = req.headers.host ?? "";
    headers["x-forwarded-proto"] = "https";
    proxy.forwarded.push({ method: req.method ?? "GET", path: req.url ?? "/", headers });
    if (proxy.forwarded.length > MAX_FORWARDED) proxy.forwarded.shift();

    const target = new URL(req.url ?? "/", proxy.upstream);
    const out = http.request(
      {
        hostname: target.hostname,
        port: target.port,
        path: `${target.pathname}${target.search}`,
        method: req.method,
        headers,
      },
      (up) => {
        res.writeHead(up.statusCode ?? 502, up.headers);
        // 串流（聊天、即時更新）收到一段就送一段。
        res.flushHeaders();
        up.pipe(res);
      },
    );
    out.on("error", () => {
      if (!res.headersSent) {
        res.writeHead(502, { "Content-Type": "text/plain; charset=utf-8" });
        res.end("bad gateway");
      } else {
        res.destroy();
      }
    });
    // 用戶端先斷線（換頁、關分頁）：後端那條也收掉，不然串流會一直掛著。
    res.on("close", () => out.destroy());
    req.pipe(out);
  });
  server.on("connection", (s) => {
    sockets.add(s);
    s.on("close", () => sockets.delete(s));
  });

  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve());
  });
  const { port } = server.address() as net.AddressInfo;
  proxy.port = port;
  proxy.url = `https://127.0.0.1:${port}`;
  proxy.close = () =>
    new Promise<void>((resolve) => {
      for (const s of sockets) s.destroy();
      server.close(() => resolve());
    });
  return proxy;
}
