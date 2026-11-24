// 手機上指令面板的結果清單：頂端 12px＋搜尋列 50px＋清單上下內距 16px＋底下留 12px。
const CHROME = 12 + 50 + 16 + 12;
// 最少露出約三列（每列 44px）。
const MIN_LIST = 132;
/** 面板離看得到那塊頂端的距離（跟 CSS 的 top-3 一樣 12px）。 */
const TOP_GAP = 12;

/**
 * 手機上結果清單最高多高（WEB-124）：不超過畫面 60%，也不超過鍵盤上面看得到的那塊，
 * 最後幾個結果才不會躲在鍵盤後面捲不到。量不到（0）回 undefined，交給 CSS 的 60%（--hg-visible-h，WEB-128）。
 */
export function paletteListMaxHeight(visible: number): number | undefined {
  if (!(visible > 0)) return undefined;
  return Math.max(MIN_LIST, Math.min(Math.round(visible * 0.6), visible - CHROME));
}

/**
 * 手機上面板的 top（px，WEB-128）：iOS 打開鍵盤會把畫面往上推（visualViewport.offsetTop > 0），
 * `position: fixed; top: 12px` 的面板頂端就跑到看不到的地方；這時改成 offsetTop＋12。沒推（0）回 undefined，交給 CSS。
 */
export function paletteTop(offsetTop: number): number | undefined {
  if (!(offsetTop > 0)) return undefined;
  return Math.round(offsetTop) + TOP_GAP;
}
