import type { MenuItem } from "@evergenie/sdk-web";
import { useQuery } from "@tanstack/react-query";
import { menuQuery, packageListQuery } from "../data/queries";
import { t } from "../i18n/zh-TW";
import { PageTabs } from "./PageTabs";
import { SectionToggles, sectionPackages } from "./SectionToggles";

/**
 * 跟這一頁同一個 section 的選單項目，依 pool 的順序（伺服器排好：核心在前、再照套件的 order）；沒有 section 回空陣列
 * （REQ-235；核心頁也能加入 section，WEB-136）。
 */
export function sectionOf(pool: MenuItem[] | undefined, route: string): MenuItem[] {
  const self = (pool ?? []).find((it) => it.route === route);
  if (!self?.section) return [];
  return (pool ?? []).filter((it) => it.section === self.section);
}

/** 合成那一頁的名稱：同 section 裡第一個寫了 section_title 的（核心在前；套件可以不寫，跟著核心的名稱）。 */
export function sectionTitleOf(siblings: MenuItem[]): string | undefined {
  return siblings.find((it) => it.section_title)?.section_title || undefined;
}

/**
 * 每個帶 section 的項目都補上那個 section 的名稱（`sectionTitleOf`）：套件可以不寫 section_title、跟著核心的，
 * 選單編輯器拿哪一項當那一列的代表都叫同一個名字（存過選單、衣櫃排在介紹前面時也叫「角色」，WEB-136）。
 */
export function withSectionTitles(pool: MenuItem[]): MenuItem[] {
  const titles = new Map<string, string>();
  for (const it of pool) {
    if (it.section && it.section_title && !titles.has(it.section))
      titles.set(it.section, it.section_title);
  }
  return pool.map((it) => {
    const title = it.section ? titles.get(it.section) : undefined;
    return title && !it.section_title ? { ...it, section_title: title } : it;
  });
}

/** 核心的選單項目（`core.<名稱>`，menu-prefs 的項目 id 規則）；項目開關只列套件，核心分頁不能關（WEB-137）。 */
export function isCoreItem(it: Pick<MenuItem, "id">): boolean {
  return (it.id ?? "").startsWith("core.");
}

/** 同 section 裡的核心頁網址：關掉正在看的套件、又沒有別的套件分頁時回到這裡（例：關掉衣櫃回「介紹」）。 */
export function sectionHome(siblings: MenuItem[]): string | undefined {
  return siblings.find(isCoreItem)?.route || undefined;
}

/**
 * 核心頁（例「介紹」`/personas`）上方的分頁：同 section 的項目有兩個以上才畫分頁（衣櫃停用、只剩自己時不畫只有一格的分頁列，WEB-137），
 * 標題用 section_title；右邊是套件的「項目」開關。只剩自己、但這個 section 有套件（含停用的）時，只留開關，關掉的套件才開得回來。
 */
export function CoreSectionTabs({ route }: { route: string }) {
  const menu = useQuery(menuQuery());
  const packages = useQuery(packageListQuery());
  const siblings = sectionOf(menu.data?.pool, route);
  const section = siblings[0]?.section ?? "";
  if (!section) return null;
  const title = sectionTitleOf(siblings) || siblings[0]?.title || "";
  const toggles = <SectionToggles section={section} title={title} current="" home={route} />;
  if (siblings.length < 2) {
    if (sectionPackages(packages.data?.packages ?? [], section).length === 0) return null;
    return <div className="-mb-2 flex justify-end">{toggles}</div>;
  }
  return (
    <PageTabs
      label={t("pageTabs.label", { title })}
      tabs={siblings.map((it) => ({
        key: it.id ?? "",
        to: it.route ?? "",
        label: it.title ?? "",
        current: it.route === route,
      }))}
      trailing={toggles}
    />
  );
}
