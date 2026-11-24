import type { Menu } from "@evergenie/sdk-web";
import { flattenTree, menuPathFor } from "./flatten";
import { MobileBar } from "./MobileBar";
import { Sidebar } from "./Sidebar";

export interface DefaultMenuProps {
  menu: Menu;
  desktop: boolean;
  pathname: string;
  onSearch: () => void;
}

// 預設的選單呈現元件（WEB 卡的約定）：≥ 768px 左側欄，< 768px 底部列。
// 使用者自訂的呈現元件（renderer）上線後才有；P4 一律用這個。
// 套件裡沒放進選單的頁（例「飲食趨勢」）算在同一個套件的入口上（menuPathFor，WEB-100）。
export function DefaultMenu({ menu, desktop, pathname, onSearch }: DefaultMenuProps) {
  const tree = menu.tree ?? [];
  const items = flattenTree(tree);
  const at = menuPathFor(items, pathname);
  if (desktop) return <Sidebar tree={tree} pathname={at} onSearch={onSearch} />;
  return <MobileBar items={items} pathname={at} />;
}
