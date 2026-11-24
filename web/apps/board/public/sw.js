// 看板的 service worker（手寫、不加套件）：App 推播，加上連不上時的離線頁（WEB-185）；不做任何快取。
// 推播的 data 是整個 sdk.Notification 的 JSON（internal/notify 送出的內容）。
// 多台（S3-8）：一支手機的看板 PWA 收所有台的推播（共用 VAPID）；payload 帶 home（送出那台的 label，
// 開發家是 <owner>-<machine>@dev），點了開帶 ?home=<那台> 的網址，看板照 home 決定是哪一台（data/sites.ts）。
const DEFAULT_ICON = "/icons/icon-192.png";
const FALLBACK_TITLE = "EverGenie 看板";
const FALLBACK_BODY = "有一則新通知";

self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      // 導覽預先載入：worker 開機的同時就去拿頁面，不因為多一層 worker 變慢（不支援就算了）。
      try {
        await self.registration.navigationPreload?.enable();
      } catch {
        // 沒有也照常
      }
      await self.clients.claim();
    })(),
  );
});

// ---- 離線頁（DEV-156 U-21、WEB-185）----
// 只管「打開一頁」（導覽）：照常走網路，跟沒有 worker 一樣（不用 Cache Storage、不改快取標頭，版本更新後馬上是新的）；
// 網路失敗，或中間的轉發回 502／503／504（看板服務沒開時 Tailscale 回 502），才回下面這張內建的頁。
// API、圖、程式檔都不經過這裡（不呼叫 respondWith，瀏覽器自己去拿）。
// 離線頁每幾秒問一次同一個網址，連上就重新載入回原來那頁；也聽 online、切回前景。
// 頁上寫白話的下一步（DEV-153、WEB-192）：會自己重開、等多久、還不行就到那台 Mac 開「evergenie 修復」（名字跟 platform.RescueName 一致，由測試守）。
const OFFLINE_STATUS = new Set([502, 503, 504]);
const OFFLINE_HTML = `<!doctype html>
<html lang="zh-Hant-TW">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="color-scheme" content="light dark">
<title>連不上看板</title>
<style>
:root { --bg: #f7f6f3; --fg: #1f1d1a; --muted: #6b665e; --line: #e2ded6; --btn: #1f1d1a; --btn-fg: #fff; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #171614; --fg: #ece9e3; --muted: #a39d93; --line: #34312c; --btn: #ece9e3; --btn-fg: #171614; }
}
* { box-sizing: border-box; }
html, body { margin: 0; height: 100%; background: var(--bg); color: var(--fg); }
body { display: flex; align-items: center; justify-content: center; padding: 24px 16px;
  font: 16px/1.6 -apple-system, BlinkMacSystemFont, "PingFang TC", "Noto Sans TC", sans-serif; }
main { max-width: 360px; width: 100%; text-align: center; display: flex; flex-direction: column; gap: 12px; align-items: center; }
h1 { margin: 0; font-size: 20px; }
p { margin: 0; color: var(--muted); }
button { min-height: 44px; min-width: 44px; padding: 0 20px; border-radius: 999px; border: 0;
  background: var(--btn); color: var(--btn-fg); font: inherit; font-weight: 600; cursor: pointer; }
.state { font-size: 14px; }
.next { margin-top: 12px; padding-top: 16px; border-top: 1px solid var(--line); text-align: left; font-size: 14px; color: var(--muted); }
.next ol { margin: 4px 0 0; padding-left: 1.4em; }
.next li + li { margin-top: 4px; }
</style>
</head>
<body>
<main data-testid="offline-page">
<h1>連不上看板</h1>
<p>看板的程式停掉時會自己重開，通常一分鐘內就好；好了這一頁會自己回去。</p>
<p class="state" id="state" aria-live="polite">每幾秒會再試一次</p>
<button type="button" id="retry">現在再試</button>
<div class="next" data-testid="offline-next">
<p>等了兩三分鐘還是這樣：</p>
<ol>
<li>看一下裝看板的那台 Mac 是不是開著、有沒有連上網路。</li>
<li>在那台 Mac 上按 ⌘＋空白鍵，搜尋「evergenie 修復」打開它。它會檢查、重開，再把結果開給你看。</li>
</ol>
</div>
</main>
<script>
(function () {
  var busy = false;
  var state = document.getElementById("state");
  function check() {
    if (busy) return;
    busy = true;
    state.textContent = "正在試…";
    fetch(location.href, { method: "HEAD", cache: "no-store" })
      .then(function (res) {
        if (res.status < 500) { location.reload(); return; }
        state.textContent = "還連不上，每幾秒會再試一次";
      })
      .catch(function () { state.textContent = "還連不上，每幾秒會再試一次"; })
      .then(function () { busy = false; });
  }
  document.getElementById("retry").addEventListener("click", check);
  window.addEventListener("online", check);
  document.addEventListener("visibilitychange", function () { if (!document.hidden) check(); });
  setInterval(function () { if (!document.hidden) check(); }, 5000);
})();
</script>
</body>
</html>`;

function offlinePage() {
  return new Response(OFFLINE_HTML, {
    status: 503,
    headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store" },
  });
}

async function navigateOrOffline(event) {
  try {
    const preloaded = event.preloadResponse ? await event.preloadResponse : undefined;
    const res = preloaded || (await self.fetch(event.request));
    return OFFLINE_STATUS.has(res.status) ? offlinePage() : res;
  } catch {
    return offlinePage();
  }
}

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.mode !== "navigate" || req.method !== "GET") return;
  const url = new URL(req.url);
  // 別的網站、API（例：下載檔案的連結）都不管。
  if (url.origin !== self.location.origin || url.pathname.startsWith("/api/")) return;
  event.respondWith(navigateOrOffline(event));
});

// 只接受同源的絕對路徑（以單一個 / 開頭）；其他一律用預設圖示。
function iconFor(image) {
  if (typeof image === "string" && image.startsWith("/") && !/^\/[/\\]/.test(image)) {
    return image;
  }
  return DEFAULT_ICON;
}

// 內文：要主人做的事（todo）優先，沒有就用背景（context）。todo 是「無」（不用做事，notify 預設會補）當作沒有，
// 不然只寫背景的通知（例：新角色來打招呼的自我介紹）推出去只看到「無」（NTF-66）。
function bodyFor(n) {
  const todo = typeof n.todo === "string" && n.todo.trim() !== "無" ? n.todo : "";
  const text = todo || n.context || "";
  return text ? Array.from(text).slice(0, 120).join("") : FALLBACK_BODY;
}

// 工作 session 的「還在等你」提醒（P7-6、D-34）：不是訊息，payload 多一個 nudge { tag }。
// 同一件事永遠同一個 tag：新的取代舊的，不疊好幾則；舊的還在通知列時新的也要響，所以 renotify。
function nudgeOf(n) {
  const x = n.nudge;
  return x && typeof x === "object" && typeof x.tag === "string" && x.tag !== "" ? x : null;
}

// 提醒點下去開那個 session 的聊天頁：只接受同源的站內路徑，其他退回首頁。
// 先照字面擋 `//`、`/\`，再照瀏覽器的解析確認還是同源（`/\t/evil.com` 這種會被解析成別的網站）。
function nudgeUrl(u) {
  if (typeof u !== "string" || !u.startsWith("/") || /^\/[/\\]/.test(u)) return "/";
  try {
    const origin = self.location.origin;
    const parsed = new URL(u, origin);
    return parsed.origin === origin ? parsed.pathname + parsed.search + parsed.hash : "/";
  } catch {
    return "/";
  }
}

// 送出這則的是哪一台（payload 的 home，照原字串比、不改寫）；沒帶（S3 之前的版本）是 ""。
function homeOf(n) {
  return typeof n.home === "string" ? n.home : "";
}

// 這個看板自己是哪一台（同源 get_mode 的 label，跟推播的 home 同一個來源）：記在 worker 裡，拿不到下次再問。
let ownHome = null;
function readOwnHome() {
  if (!ownHome) {
    ownHome = (async () => {
      try {
        const timeout =
          typeof AbortSignal !== "undefined" && typeof AbortSignal.timeout === "function"
            ? AbortSignal.timeout(3000)
            : undefined;
        const res = await self.fetch("/api/system/get_mode", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: "{}",
          signal: timeout,
        });
        const mode = res.ok ? await res.json() : null;
        if (!mode || typeof mode.label !== "string" || mode.label === "")
          throw new Error("no label");
        return mode.label;
      } catch {
        ownHome = null;
        return "";
      }
    })();
  }
  return ownHome;
}

// 這則是別台送來的嗎：有 home、而且不是這個看板自己（自己是誰讀不到時，有 home 就當別台，網址帶 ?home 一樣開得對）。
// 自己這台的推播跟 S3 之前完全一樣（網址不帶 ?home、tag 就是編號、用 payload 的圖示）。
async function remoteHome(n) {
  const home = homeOf(n);
  if (!home) return "";
  return home === (await readOwnHome()) ? "" : home;
}

// 站內路徑接上 ?home=<那台>（已經有的查詢字串保留、home 換成這一台）；不是別台就原樣。
function withHome(path, home) {
  if (!home) return path;
  const u = new URL(path, self.location.origin);
  u.searchParams.set("home", home);
  return u.pathname + u.search + u.hash;
}

// 通知的 tag：兩台的編號會撞（兩台都有 M-7、都有 nudge-code-sec-12），別台的加上台名分開。
function tagFor(tag, home) {
  return home ? `${home}|${tag}` : tag;
}

async function showNudge(n, nudge) {
  const home = await remoteHome(n);
  const tag = tagFor(nudge.tag, home);
  await self.registration.showNotification(n.title || FALLBACK_TITLE, {
    body: bodyFor(n),
    icon: DEFAULT_ICON,
    tag,
    renotify: true,
    data: { url: withHome(nudgeUrl(n.url), home) },
  });
}

// 點了開哪裡：測試通知開裝置頁，其他開那一則訊息（別台的帶 ?home=）；沒有編號就開首頁。
// 一般通知的 url 不讀（伺服器沒擋完整網址，NTF-53），用編號自己組站內路徑。
function urlFor(n, home) {
  if (n.id === "test") return "/settings/devices";
  if (typeof n.id === "string" && n.id !== "")
    return withHome(`/messages/${encodeURIComponent(n.id)}`, home);
  return "/";
}

// 一般通知：別台的圖示是那台的相對路徑，在這個看板開會變成別張圖，改用預設圖示。
async function showMessage(n) {
  const home = await remoteHome(n);
  await self.registration.showNotification(n.title || FALLBACK_TITLE, {
    body: bodyFor(n),
    icon: home ? DEFAULT_ICON : iconFor(n.image),
    tag: typeof n.id === "string" && n.id !== "" ? tagFor(n.id, home) : undefined,
    data: { url: urlFor(n, home) },
  });
}

self.addEventListener("push", (event) => {
  let n = null;
  try {
    n = event.data ? event.data.json() : null;
  } catch {
    n = null;
  }
  if (!n || typeof n !== "object") {
    event.waitUntil(
      self.registration.showNotification(FALLBACK_TITLE, {
        body: FALLBACK_BODY,
        icon: DEFAULT_ICON,
        data: { url: "/" },
      }),
    );
    return;
  }
  const nudge = nudgeOf(n);
  if (nudge) {
    event.waitUntil(showNudge(n, nudge));
    return;
  }
  event.waitUntil(showMessage(n));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = event.notification.data?.url || "/";
  event.waitUntil(
    (async () => {
      const wins = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
      const open = wins.find((w) => new URL(w.url).origin === self.location.origin);
      if (open) {
        await open.focus();
        try {
          await open.navigate(url);
          return;
        } catch {
          // navigate 在少數瀏覽器不能用：退回開新視窗。
        }
      }
      await self.clients.openWindow(url);
    })(),
  );
});
