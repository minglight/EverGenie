import { Popover } from "@base-ui/react/popover";
import { Kbd } from "@evergenie/ui";
import { t } from "../../i18n/zh-TW";

// 桌機今天頁的快速鍵說明（DEV-156 U-32）：以前清單底下常駐一排「J K A R ⌘K」，對不用鍵盤的人是雜訊；
// 收進「等你處理」標題旁的「？」（44px），按了才看。快速鍵本身照舊能用（todayKeys）。
export function KeysHelp() {
  return (
    <Popover.Root>
      <Popover.Trigger
        aria-label={t("today.keys.help")}
        data-testid="today-keys-help"
        className="ml-auto flex size-11 shrink-0 items-center justify-center rounded-full text-[15px] font-semibold text-muted hover:bg-hover hover:text-fg focus-visible:outline-2 focus-visible:outline-focus"
      >
        ？
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner align="end" sideOffset={4} className="z-50">
          <Popover.Popup
            aria-label={t("today.keys.help")}
            className="hg-pop flex w-[240px] flex-col gap-2 rounded-2xl border border-border bg-surface p-3.5 text-[13px] text-fg shadow-[var(--shadow-pop)] outline-none"
          >
            <p className="m-0 text-xs font-semibold text-muted">{t("today.keys.help")}</p>
            <ul className="m-0 flex list-none flex-col gap-1.5 p-0">
              <li>
                <Kbd>J</Kbd> <Kbd>K</Kbd> {t("today.keys.updown")}
              </li>
              <li>
                <Kbd>A</Kbd> {t("today.keys.primary")}
              </li>
              <li>
                <Kbd>R</Kbd> {t("today.keys.secondary")}
              </li>
              <li>
                <Kbd>⌘K</Kbd> {t("today.keys.palette")}
              </li>
            </ul>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
