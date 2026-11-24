import type { Menu, MenuEntry, MenuItem } from "@evergenie/sdk-web";
import { useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";
import { useBundle } from "../pkg/loadBundle";
import { PackageBoundary } from "../pkg/PackageBoundary";
import { DefaultMenu } from "./DefaultMenu";

/** 自訂選單元件（`shell.menu` 的套件頁面）收到的 props：唯讀；要換頁只能呼叫 navigate。 */
export interface MenuRendererProps {
  pool: MenuItem[];
  tree: MenuEntry[];
  /** 目前的網址路徑。 */
  current: string;
  navigate: (route: string) => void;
  layout: "desktop" | "mobile";
}

export interface NavHostProps {
  menu: Menu;
  desktop: boolean;
  pathname: string;
  onSearch: () => void;
  /** 網址有 ?menu=default：一律用預設選單，不載入自訂元件。 */
  forceDefault: boolean;
  /** 自訂選單載入或執行失敗（已經換回預設選單）；reason 是白話原因。 */
  onFailed: (reason: string) => void;
}

// 導覽區（桌機側欄、手機底部列）：預設用 DefaultMenu；menu.toml 指定了自訂選單元件（REQ-140）就先畫預設，
// 載入成功後換成自訂的（取代整個導覽區）；載入失敗或執行時丟錯就換回預設，並通知外殼顯示提醒。
export function NavHost({
  menu,
  desktop,
  pathname,
  onSearch,
  forceDefault,
  onFailed,
}: NavHostProps) {
  const navigate = useNavigate();
  const url = forceDefault || menu.renderer_error ? undefined : menu.renderer_bundle_url;
  const { Component, error } = useBundle(url);
  useEffect(() => {
    if (error) onFailed(error.message);
  }, [error, onFailed]);

  const fallback = (
    <DefaultMenu menu={menu} desktop={desktop} pathname={pathname} onSearch={onSearch} />
  );
  if (!url || !Component) return fallback;
  return (
    <PackageBoundary
      key={url}
      title={menu.renderer ?? ""}
      onError={(err) => onFailed(err.message)}
      fallback={() => fallback}
    >
      <Component
        pool={menu.pool ?? []}
        tree={menu.tree ?? []}
        current={pathname}
        navigate={(route: string) => void navigate({ to: route as "/" })}
        layout={desktop ? "desktop" : "mobile"}
      />
    </PackageBoundary>
  );
}
