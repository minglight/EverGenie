import type { PageInfo } from "@evergenie/sdk-web";
import { CompactEmpty, LoadFailedGroup } from "@evergenie/ui";
import { useQuery } from "@tanstack/react-query";
import { pagesQuery } from "../data/queries";
import { type PackageComponent, useBundle } from "./loadBundle";
import { PackageBoundary } from "./PackageBoundary";

/** 掛在這個 slot、而且編譯成功的套件頁面：宣告 slot_front 的排最前面，其他依 id 排。 */
export function slotPages(pages: readonly PageInfo[] | undefined, id: string): PageInfo[] {
  return (pages ?? [])
    .filter((p) => p.slot === id && !p.error && p.bundle_url)
    .sort(
      (a, b) =>
        Number(b.slot_front ?? false) - Number(a.slot_front ?? false) ||
        (a.id ?? "").localeCompare(b.id ?? ""),
    );
}

/** 掛在這個 slot 的頁面（跟 Slot 同一份 web.get_pages 快取）；還沒讀到是 undefined。 */
export function useSlotPages(id: string): PageInfo[] | undefined {
  const pages = useQuery(pagesQuery());
  // 讀頁面清單失敗就當作沒有套件掛上來（只剩核心自己的東西），不要一直停在載入中。
  if (pages.isError) return [];
  return pages.data === undefined ? undefined : slotPages(pages.data, id);
}

/**
 * 頁面留給別的套件掛東西的位置：找 `web.get_pages` 裡 `slot === id` 而且沒有錯誤的頁面，
 * 宣告 slot_front 的排最前面，其他依 id 排，各自載入、各包一層 PackageBoundary（一個壞掉不影響別的）。沒有就什麼都不畫。
 * props 原樣交給每個套件元件（例：角色頁的分頁給 `persona`，DEV-143），套件自己決定用不用。
 * 載不進來的（程式沒抓到、畫面壞掉、套件自己用 `LoadFailed` 說讀不到資料）合成最上面一條「〈甲〉、〈乙〉暫時載不進來　再試一次」，
 * 不一張一條佔滿畫面（DEV-156）。
 * `compactEmpty`：宣告自己現在是空的卡片（用 `@evergenie/ui` 的 `EmptyLine` 包起來的）縮成一行；沒宣告的照舊畫整張
 * （今天頁的手機版用，DEV-156 U-04）。
 */
export function Slot({
  id,
  className,
  props,
  compactEmpty = false,
}: {
  id: string;
  className?: string;
  props?: Record<string, unknown>;
  compactEmpty?: boolean;
}) {
  const hosted = useSlotPages(id) ?? [];
  if (hosted.length === 0) return null;
  const pages = hosted.map((p) => <SlotPage key={p.id} info={p} props={props} />);
  return (
    <div data-slot={id} className={className}>
      <LoadFailedGroup>
        {compactEmpty ? <CompactEmpty>{pages}</CompactEmpty> : pages}
      </LoadFailedGroup>
    </div>
  );
}

/** 只畫一個掛在 slot 的套件頁面（例：角色頁一次只顯示選中的那個分頁）；外面包一層 PackageBoundary。 */
export function SlotPage({ info, props }: { info: PageInfo; props?: Record<string, unknown> }) {
  return (
    <PackageBoundary title={info.title ?? info.id ?? ""} variant="inline">
      <SlotItem
        pkg={info.package ?? ""}
        pageId={info.id ?? ""}
        url={info.bundle_url ?? ""}
        props={props}
      />
    </PackageBoundary>
  );
}

function SlotItem({
  pkg,
  pageId,
  url,
  props,
}: {
  pkg: string;
  pageId: string;
  url: string;
  props?: Record<string, unknown>;
}) {
  const { Component, error } = useBundle(url);
  if (error) throw error; // 交給外面的 PackageBoundary
  if (!Component) return null;
  return (
    <Mounted Component={Component} pkg={pkg} page={pageId.slice(pkg.length + 1)} props={props} />
  );
}

function Mounted({
  Component,
  pkg,
  page,
  props,
}: {
  Component: PackageComponent;
  pkg: string;
  page: string;
  props?: Record<string, unknown>;
}) {
  return <Component {...props} pkg={pkg} page={page} search={window.location.search} />;
}
