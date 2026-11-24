import { api, type WebPhone } from "@evergenie/sdk-web";
import { type QueryClient, queryOptions } from "@tanstack/react-query";

// 手機連線（DEV-153）：設定頁的「手機連線」與外殼的「整個網路都連得到」警告共用同一份現況（FEN-26）。
// 只問主伺服器（這個看板自己的那台）；`local` 由看板伺服器照請求算，這裡送什麼都不算數。

export const PHONE_KEY = ["web.get_phone"] as const;

export const phoneQuery = queryOptions({
  queryKey: PHONE_KEY,
  queryFn: async () => (await api.web.getPhone({ local: false })).phone,
  staleTime: 5_000,
  refetchInterval: 60_000,
  retry: false,
});

function keep(client: QueryClient, phone: WebPhone | undefined) {
  if (phone) client.setQueryData(PHONE_KEY, phone);
}

/** 「讓手機連得到」：只有這台 Mac 本機開的看板按得動（別處伺服器回 403）。 */
export async function setupPhone(client: QueryClient) {
  const res = await api.web.phoneSetup({ local: false });
  keep(client, res.phone);
  return res;
}

/** 把看板對整個網路的公開（Funnel）關掉。 */
export async function closeFunnel(client: QueryClient) {
  const res = await api.web.phoneCloseFunnel({ local: false });
  keep(client, res.phone);
  return res;
}

/** 這個畫面是不是從看板的 Tailscale 網址打開的（是的話，手機那兩步已經做好了）。 */
export function openedViaTailnet(phone: Pick<WebPhone, "local" | "url">, origin: string): boolean {
  return !phone.local && Boolean(phone.url) && origin === phone.url;
}
