// 部署新版後，開著的舊分頁還在跑舊的看板程式（SPA 不會自己換），新的訊息格式它看不懂（WEB-172）。
// 定時、以及切回分頁時看一下伺服器現在的 index.html 掛哪一支主程式，跟自己不一樣就是舊版；
// 主人沒在打字、沒有填到一半的東西、沒開著視窗時才自動重新整理，不然等下一次再看。
// 只存在畫面上的東西（選了還沒送的附件、還沒送到的話、答到一半的選擇題）由元件標 data-unsaved。

const ENTRY = /<script\b[^>]*\btype="module"[^>]*\bsrc="(\/assets\/index-[\w-]+\.js)"/;
const ENTRY_SRC = /^\/assets\/index-[\w-]+\.js$/;
// 有字就算填到一半的輸入框（勾選、按鈕這類不算）。
const TEXT_INPUTS =
  'input:not([type]), input[type="text"], input[type="search"], input[type="url"], input[type="email"], input[type="number"], input[type="tel"], input[type="password"], textarea';
const CHECK_MS = 60_000;

/** html 裡的主程式路徑（type="module" 那支 /assets/index-xxxx.js）；找不到回空字串。 */
export function entryOf(html: string): string {
  return html.match(ENTRY)?.[1] ?? "";
}

/** 現在可以重新整理嗎：沒有焦點在輸入框、沒有打到一半的字、沒有標了 data-unsaved 的東西、沒有開著的視窗（抽屜、確認框）。 */
export function safeToReload(doc: Document): boolean {
  const el = doc.activeElement as HTMLElement | null;
  if (el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable))
    return false;
  if (doc.querySelector('[role="dialog"], [role="alertdialog"], [data-unsaved]')) return false;
  for (const t of Array.from(doc.querySelectorAll<HTMLInputElement>(TEXT_INPUTS))) {
    if (t.value.trim() !== "") return false;
  }
  for (const t of Array.from(doc.querySelectorAll<HTMLElement>('[contenteditable="true"]'))) {
    if ((t.textContent ?? "").trim() !== "") return false;
  }
  return true;
}

/** 正式版才裝：開始定時檢查，回傳停止的函式。 */
export function installStaleReload(
  doc: Document = document,
  fetchHtml: () => Promise<string> = () =>
    fetch("/", { cache: "no-store", credentials: "same-origin" }).then((r) =>
      r.ok ? r.text() : "",
    ),
  reload: () => void = () => window.location.reload(),
): () => void {
  const mine = Array.from(doc.querySelectorAll<HTMLScriptElement>('script[type="module"][src]'))
    .map((s) => s.getAttribute("src") ?? "")
    .find((s) => ENTRY_SRC.test(s));
  if (!mine) return () => {};
  let stale = false;
  let busy = false;
  const check = async () => {
    if (doc.visibilityState !== "visible" || busy) return;
    busy = true;
    try {
      if (!stale) {
        const now = entryOf(await fetchHtml());
        stale = now !== "" && now !== mine;
      }
      if (stale && safeToReload(doc)) reload();
    } catch {
      // 連不到伺服器就下次再看。
    } finally {
      busy = false;
    }
  };
  const timer = window.setInterval(check, CHECK_MS);
  const onVisible = () => void check();
  doc.addEventListener("visibilitychange", onVisible);
  return () => {
    window.clearInterval(timer);
    doc.removeEventListener("visibilitychange", onVisible);
  };
}
