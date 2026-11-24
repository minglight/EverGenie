import { Badge, cn, Sheet, StatusDot } from "@evergenie/ui";
import { Ellipsis } from "lucide-react";
import { useState } from "react";
import { t } from "../i18n/zh-TW";
import { iconFor } from "../lib/icons";
import { REPLY_BADGES } from "../lib/statusTone";
import { type FlatItem, isActiveItem, sectionsOf, splitBar } from "./flatten";
import { NavLink } from "./NavLink";

export interface MobileBarProps {
  items: FlatItem[];
  pathname: string;
}

const tabClass =
  "relative flex flex-1 flex-col items-center justify-center gap-0.5 text-[10px] font-medium focus-visible:outline-2 focus-visible:outline-offset-[-4px] focus-visible:outline-focus";

function TabBadge({ item }: { item: FlatItem }) {
  if (item.badge <= 0) return null;
  // 聊天有等你回的串時只顯示橘點；總覽（等你處理）用橘底，其他用主色底（WEB-70：橘只給等你處理）。
  if (item.id === "core.chat") {
    return <StatusDot className="absolute top-1 left-1/2 ml-2" size={9} />;
  }
  return (
    <Badge
      tone={REPLY_BADGES.has(item.id) ? "attention" : "accent"}
      className="absolute top-0.5 left-1/2 ml-1.5"
    >
      {item.badge}
    </Badge>
  );
}

// 手機（< 768px）的預設選單：把樹攤平，總數 ≤ 6 全放，否則前 5 個＋「更多」。
// 選到的那顆：圖示後面一塊淡紫色塊＋字加粗（沿用舊看板的底部分頁列）。
// 「更多」裡是一格一格的圖示磚（一列四個），比長清單好掃。
export function MobileBar({ items, pathname }: MobileBarProps) {
  const [moreOpen, setMoreOpen] = useState(false);
  const { bar, rest } = splitBar(items);
  const restBadge = rest.reduce((sum, i) => sum + i.badge, 0);
  const restCurrent = rest.some((i) => isActiveItem(i, pathname));

  return (
    <>
      <nav
        aria-label={t("app.nav")}
        className="box-border flex h-[calc(50px+env(safe-area-inset-bottom,0px))] shrink-0 border-t border-border bg-surface px-1.5 pt-0.5 pb-[env(safe-area-inset-bottom,0px)]"
      >
        {bar.map((item) => {
          const Icon = iconFor(item.icon);
          const current = isActiveItem(item, pathname);
          return (
            <NavLink
              key={item.id}
              route={item.route}
              aria-current={current ? "page" : undefined}
              className={cn(tabClass, current ? "font-semibold text-accent" : "text-muted")}
            >
              <span
                className={cn(
                  "flex h-[26px] w-12 items-center justify-center rounded-full transition-colors",
                  current && "bg-accent-soft",
                )}
              >
                <Icon className="size-[22px]" strokeWidth={1.8} aria-hidden="true" />
              </span>
              {item.title}
              <TabBadge item={item} />
            </NavLink>
          );
        })}
        {rest.length > 0 ? (
          <button
            type="button"
            onClick={() => setMoreOpen(true)}
            aria-haspopup="dialog"
            aria-current={restCurrent ? "page" : undefined}
            className={cn(tabClass, restCurrent ? "font-semibold text-accent" : "text-muted")}
          >
            <span
              className={cn(
                "flex h-[26px] w-12 items-center justify-center rounded-full transition-colors",
                restCurrent && "bg-accent-soft",
              )}
            >
              <Ellipsis className="size-[22px]" strokeWidth={1.8} aria-hidden="true" />
            </span>
            {t("app.more")}
            {restBadge > 0 ? (
              <Badge tone="accent" className="absolute top-0.5 left-1/2 ml-1.5">
                {restBadge}
              </Badge>
            ) : null}
          </button>
        ) : null}
      </nav>
      {rest.length > 0 ? (
        <Sheet open={moreOpen} onOpenChange={setMoreOpen} title={t("app.moreTitle")}>
          <div className="flex flex-col gap-1 px-3 pb-2">
            {sectionsOf(rest).map((section, i) => (
              <div
                key={section.title ?? `top-${i}`}
                data-testid="more-section"
                data-separated={i > 0 && !section.title ? "true" : undefined}
                className={cn(
                  "flex flex-col",
                  // 沒有標題的頂層項目（設定…）接在群組後面時畫一條分隔線，不然看起來像在上一個群組裡（WEB-106）。
                  i > 0 && !section.title && "mt-1 border-t border-border pt-2",
                )}
              >
                {section.title ? (
                  <div className="px-2 pt-2 pb-1 text-xs font-semibold text-muted">
                    {section.title}
                  </div>
                ) : null}
                <div className="grid grid-cols-4 gap-x-1 gap-y-2 pb-1">
                  {section.items.map((item) => {
                    const Icon = iconFor(item.icon);
                    const current = isActiveItem(item, pathname);
                    return (
                      <NavLink
                        key={item.id}
                        route={item.route}
                        onClick={() => setMoreOpen(false)}
                        aria-current={current ? "page" : undefined}
                        className="relative flex min-h-[76px] flex-col items-center gap-1.5 rounded-xl px-1 pt-1.5 text-center text-[13px] font-medium text-fg focus-visible:outline-2 focus-visible:outline-focus"
                      >
                        <span
                          className={cn(
                            "flex size-12 items-center justify-center rounded-2xl",
                            current ? "bg-accent text-accent-fg" : "bg-accent-soft text-accent",
                          )}
                        >
                          <Icon className="size-[22px]" strokeWidth={1.8} aria-hidden="true" />
                        </span>
                        <span className="w-full truncate">{item.title}</span>
                        {item.badge > 0 ? (
                          <Badge tone="accent" className="absolute top-0 right-2">
                            {item.badge}
                          </Badge>
                        ) : null}
                      </NavLink>
                    );
                  })}
                </div>
              </div>
            ))}
          </div>
        </Sheet>
      ) : null}
    </>
  );
}
