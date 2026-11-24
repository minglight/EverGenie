import type { Menu, MenuEntry } from "@evergenie/sdk-web";
import { t } from "../i18n/zh-TW";

// 基本選單（WEB-175）：選單（web.get_menu）等太久或讀不到時，外殼先用這份，主人照樣進得去每個核心頁。
// 只放「每個家都有、不看權限與套件」的核心頁，順序同伺服器的預設樹（internal/feature/inbox/menu 的 defaultPrefs）；
// 套件的頁面、主人自己排的樹、徽章的數字都要等真的選單。收在設定裡的「權限申請」不放（預設就是收起來的）。

/** 選單等多久還沒到就先用基本選單（正常幾百毫秒就到；等這段是為了不先畫一份再換，UXC-4）。 */
export const MENU_GRACE_MS = 3_000;

/** 選單讀不到之後，隔多久自己再試一次（不用等主人按）。 */
export const MENU_RETRY_MS = 15_000;

const entry = (id: string, title: string, icon: string, route: string): MenuEntry => ({
  id,
  title,
  icon,
  route,
  badge: 0,
});

export function basicMenu(): Menu {
  const tree = [
    entry("core.overview", t("page.overview"), "house", "/"),
    entry("core.chat", t("page.chat"), "message-circle", "/chat"),
    entry("core.reminders", t("page.reminders"), "alarm-clock", "/reminders"),
    entry("core.messages", t("page.messages"), "bell", "/messages"),
    entry("core.tasks", t("page.tasks"), "clipboard-list", "/tasks"),
    entry("core.settings", t("page.settings"), "settings", "/settings"),
  ];
  return { tree, pool: tree.map(({ children: _children, ...item }) => item) };
}
