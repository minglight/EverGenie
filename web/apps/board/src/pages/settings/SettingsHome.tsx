import { cn } from "@evergenie/ui";
import { Link, Navigate } from "@tanstack/react-router";
import { ChevronRight } from "lucide-react";
import { type TextKey, t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import { settingsCategories, tabLabel, useTucked } from "./SettingsLayout";

const rowCls = cn(
  "flex min-h-[52px] items-center justify-between gap-3 px-4 text-[15px] font-medium text-fg",
  "hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus",
);

function Row({ to, label }: { to: string; label: string }) {
  return (
    <li className="border-b border-border last:border-b-0">
      <Link to={to as "/"} className={rowCls}>
        <span className="min-w-0 truncate">{label}</span>
        <ChevronRight className="size-5 shrink-0 text-muted" aria-hidden="true" />
      </Link>
    </li>
  );
}

function Group({ label, children }: { label: TextKey; children: React.ReactNode }) {
  return (
    <section aria-label={t(label)} className="flex flex-col gap-1.5">
      <h2 className="px-1 text-[12px] font-semibold tracking-[0.06em] text-muted">{t(label)}</h2>
      <ul className="overflow-hidden rounded-[14px] border border-border bg-surface">{children}</ul>
    </section>
  );
}

/**
 * 設定首頁 `/settings`（DEV-156 U-12）：手機是一張清單，分常用／秘書／進階三組，點一列進那一頁；
 * 收到設定的頁面（MNU-10，例：權限申請）放在「進階」最後面，不在每一頁都出現。
 * 桌機有左邊的分頁清單，這裡直接轉到第一頁（跟以前一樣）。
 */
export function SettingsHome() {
  const desktop = useIsDesktop();
  const tucked = useTucked();
  if (desktop) return <Navigate to="/settings/notify" replace />;
  return (
    <div
      data-testid="settings-home"
      className="mx-auto flex max-w-[640px] flex-col gap-5 px-4 pt-1 pb-8"
    >
      {settingsCategories.map((cat) => (
        <Group key={cat.label} label={cat.label}>
          {cat.tabs.map((id) => (
            <Row key={id} to={`/settings/${id}`} label={t(tabLabel[id])} />
          ))}
          {cat.label === "settings.cat.advanced"
            ? tucked.map((it) => (
                <Row
                  key={it.id}
                  to={it.route ?? "/"}
                  label={(it.section && it.section_title) || it.title || it.id || ""}
                />
              ))
            : null}
        </Group>
      ))}
    </div>
  );
}
