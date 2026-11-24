// 看板自己的路由；/share、/api 這些是伺服器的檔案與接口，不能交給前端路由。
// 站內連結（shell/internalLinks.ts）與套件的 host.navigate（pkg/runtime.ts）共用這一份判斷。
const BOARD_PATH = /^\/(p\/|chat|tasks|reminders|messages|goals|settings|policy|personas|$)/;

/** 路徑（不含網域、可帶 ?query）是不是看板前端路由管的頁面。 */
export function isBoardPath(path: string): boolean {
  if (!path.startsWith("/") || path.startsWith("//")) return false;
  const pathname = path.split(/[?#]/, 1)[0] ?? "";
  return BOARD_PATH.test(pathname);
}
