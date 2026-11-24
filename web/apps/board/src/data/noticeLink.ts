import type { Notification } from "@evergenie/sdk-web";

// 訊息上的連結按鈕與「說話的人」（NTF-66、WEB-150，DEV-115 新角色來打招呼）。
// 規則在核心（notify 只收站內路徑）；這裡只把它拆成路由用的 to／search，再擋一次不是站內的。

export interface NoticeLinkTarget {
  label: string;
  /** 路由路徑（例 /settings/personas）。 */
  to: string;
  /** 網址上的查詢參數（例 { card: "luna" }）。 */
  search: Record<string, string>;
}

/**
 * 這則有沒有連結按鈕：字和站內路徑都要有（/ 開頭、不是 // 或 /\ 開頭），否則不畫；
 * 指回這則訊息自己的（/messages/<這則的編號>）也不畫——已經在這頁了，按了沒反應（DEV-157）。
 */
export function noticeLinkOf(
  n: Pick<Notification, "link" | "id"> | undefined,
): NoticeLinkTarget | null {
  const label = n?.link?.label?.trim();
  const url = n?.link?.url?.trim();
  if (!label || !url || !url.startsWith("/") || url.startsWith("//") || url.startsWith("/\\")) {
    return null;
  }
  const base = "http://board.invalid";
  const parsed = new URL(url, base);
  // 解析（正規化）之後還要在同一站、路徑不是 // 開頭：/.//evil.com 會被正規化成 //evil.com。
  if (parsed.origin !== base || parsed.pathname.startsWith("//")) {
    return null;
  }
  if (n?.id && parsed.pathname.replace(/\/+$/, "") === `/messages/${n.id}`) {
    return null;
  }
  return { label, to: parsed.pathname, search: Object.fromEntries(parsed.searchParams) };
}

/** 寄件人顯示的名字：角色本人在說話（speaker）就寫她，否則寫那位秘書。 */
export function senderName(n: Pick<Notification, "speaker">, agentName: string): string {
  return n.speaker?.trim() || agentName;
}

/** 角色本人在說話時的頭像：image 是站內的頭像網址（/avatars/…）才算；不是就用秘書的。 */
export function speakerAvatar(n: Pick<Notification, "speaker" | "image">): string | undefined {
  return n.speaker?.trim() && n.image?.startsWith("/avatars/") ? n.image : undefined;
}
