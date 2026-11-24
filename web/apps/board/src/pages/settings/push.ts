// 這台裝置的 App 推播（Web Push）：瀏覽器端的訂閱與退訂。伺服器端的記錄由 notify.subscribe_push 管。

/** 沒有 serviceWorker、PushManager 或 Notification 就收不了推播（iOS 要先加入主畫面）。 */
export function pushSupported(): boolean {
  return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

/** 伺服器給的公開金鑰是 base64url；pushManager.subscribe 要的是位元組。 */
export function keyToBytes(base64url: string): Uint8Array<ArrayBuffer> {
  const padded = base64url.replaceAll("-", "+").replaceAll("_", "/");
  const raw = atob(padded + "=".repeat((4 - (padded.length % 4)) % 4));
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

/** 看板的 service worker（main.tsx 在正式版會註冊；沒註冊就補註冊），等它準備好。 */
export async function readyRegistration(): Promise<ServiceWorkerRegistration> {
  const existing = await navigator.serviceWorker.getRegistration();
  if (!existing) await navigator.serviceWorker.register("/sw.js", { scope: "/" });
  return navigator.serviceWorker.ready;
}

/** 這台裝置現在有沒有推播訂閱。 */
export async function localSubscription(): Promise<PushSubscription | null> {
  const reg = await navigator.serviceWorker.getRegistration();
  return reg ? reg.pushManager.getSubscription() : null;
}

export interface SubscriptionKeys {
  endpoint: string;
  p256dh: string;
  auth: string;
}

/** 訂閱的 endpoint 與兩把金鑰（notify.subscribe_push 要的三個欄位）。 */
export function keysOf(sub: PushSubscription): SubscriptionKeys {
  const json = sub.toJSON();
  return {
    endpoint: json.endpoint ?? sub.endpoint,
    p256dh: json.keys?.p256dh ?? "",
    auth: json.keys?.auth ?? "",
  };
}
