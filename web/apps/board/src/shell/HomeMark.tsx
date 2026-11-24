import { cn } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { useEffect } from "react";
import { homeQuery } from "../data/queries";
import { useSites } from "../data/sites";
import { t } from "../i18n/zh-TW";

// 一眼看出是哪個家（2026-11-01）：標題旁的家標籤（amy-home、amy-home@dev；只有一個家的正式家不畫，DEV-156 U-07），
// 開發家（env = dev）另外在畫面最上面一條橘色橫條、分頁標題前加「開發｜」、瀏覽器的 theme-color 換橘色。
// 資料來自 system.get_mode 的 env／label（伺服器算好，前端只顯示，FEN-N3）；讀不到就當正式家、不畫標籤。

export function useHome() {
  const { data } = useQuery(homeQuery());
  return { dev: data?.env === "dev", label: data?.label ?? "", userName: data?.userName ?? "" };
}

/** 分頁標題：開發家前面加「開發｜」。 */
export function homeTitle(dev: boolean, title: string): string {
  return dev ? `${t("shell.home.devTitle")}${title}` : title;
}

/**
 * 標題旁的家標籤：正式家中性灰底，開發家橘色底。
 * 只有一個家的正式家不畫（主人只有一個家時，家名是雜訊，DEV-156 U-07）；看板看兩個家以上、或這是開發家才畫。
 */
export function HomeLabel({ className }: { className?: string }) {
  const { dev, label } = useHome();
  const many = useSites().length > 1;
  if (!label || (!dev && !many)) return null;
  return (
    <span
      data-testid="home-label"
      data-env={dev ? "dev" : "prod"}
      title={dev ? t("shell.home.dev") : t("shell.home.label", { label })}
      className={cn(
        "inline-flex h-5 max-w-full shrink-0 items-center truncate rounded-full px-2 text-[11px] leading-none font-semibold",
        dev ? "bg-attention-soft text-attention-fg" : "bg-hover text-muted",
        className,
      )}
    >
      {label}
    </span>
  );
}

/** 開發家：畫面最上面一條橘色橫條（手機、桌機都有）。正式家不畫。 */
export function DevStrip() {
  const { dev } = useHome();
  if (!dev) return null;
  return (
    <div
      data-testid="dev-strip"
      role="note"
      aria-label={t("shell.home.dev")}
      className="h-1 w-full shrink-0 bg-attention"
    />
  );
}

/** 開發家把 theme-color（手機的狀態列、分頁列顏色）換成橘色；離開開發家（不會發生，保險）就還原。 */
export function useDevThemeColor(dev: boolean) {
  useEffect(() => {
    if (!dev) return;
    const metas = [...document.querySelectorAll<HTMLMetaElement>('meta[name="theme-color"]')];
    const before = metas.map((m) => m.content);
    const amber =
      getComputedStyle(document.documentElement).getPropertyValue("--attention").trim() ||
      "#f59e0b";
    for (const m of metas) m.content = amber;
    return () => {
      metas.forEach((m, i) => {
        m.content = before[i] ?? m.content;
      });
    };
  }, [dev]);
}
