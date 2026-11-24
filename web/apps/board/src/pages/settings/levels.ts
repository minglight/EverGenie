import type { PermissionLevel } from "@evergenie/sdk-web";
import type { TextKey } from "../../i18n/zh-TW";

export const levelOrder: PermissionLevel[] = ["full", "standard", "readonly"];

/** 等級的白話全名（「整個家的預設：…」用）。 */
export const levelName = {
  full: "settings.level.full",
  standard: "settings.level.standard",
  readonly: "settings.level.readonly",
} as const satisfies Record<PermissionLevel, TextKey>;

/** 分段按鈕上的短名。 */
export const levelShort = {
  full: "settings.level.short.full",
  standard: "settings.level.short.standard",
  readonly: "settings.level.short.readonly",
} as const satisfies Record<PermissionLevel, TextKey>;

/** 設定精靈的三個選項，順序與文字同 internal/cli/trust.go 的 wizardChoices。 */
export const wizardChoices: { level: PermissionLevel; title: TextKey; explain: TextKey }[] = [
  {
    level: "full",
    title: "settings.setup.choice.full.title",
    explain: "settings.setup.choice.full.explain",
  },
  {
    level: "standard",
    title: "settings.setup.choice.standard.title",
    explain: "settings.setup.choice.standard.explain",
  },
  {
    level: "readonly",
    title: "settings.setup.choice.readonly.title",
    explain: "settings.setup.choice.readonly.explain",
  },
];

/** 跟預設＝拿掉這位的單獨設定，送給伺服器是空字串（型別沒有這個值，這裡集中轉一次）。 */
export const INHERIT = "" as PermissionLevel;

/** 這位秘書的等級選項：有單獨設定（source = agent）就是那個等級，否則是「跟預設」。 */
export function ownLevel(
  source: string | undefined,
  level: PermissionLevel | undefined,
): PermissionLevel | "inherit" {
  return source === "agent" && level ? level : "inherit";
}
