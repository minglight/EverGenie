// 套件頁面的網址只在這裡組（SEC-302 S4、REQ-351）：標準網址固定是 /p/<套件 id>/<頁面 id>（核心 internal/feature/menu 的 pageRoute 同一條規則），
// 其他地方要跳轉或比對都呼叫這兩個，不手寫 "/p/" 字串（routes.spec.ts 掃原始碼守著）。
export const PACKAGE_ROUTE = "/p/$pkg/$page";

export function packagePageRoute(pkg: string, page: string): string {
  return `/p/${pkg}/${page}`;
}
