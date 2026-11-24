// 衣服分頁的總覽（DEV-143，作者 2026-11-08「一個角色可以知道每套衣服，每個照片的狀態」，不要一直捲）：
// 一套一列（縮圖、名字、狀態小標）；點一套換成那套的面板（每張照片一列，lib/photos.tsx）。
// 小標來自衣櫃程式算好的 closet.person（CLO-21 的 status），頁面不自己掃檔案；沒兌換的衣服沒有 status（開發家例外，CLO-27）。
import { type HiddenVersion, type MineOutfit, statusChips, T, tagsOf, thumbOf } from "./mine";
import { Thumb } from "./parts";

const chipOk = "rounded-full bg-accent-soft px-2 py-0.5 text-xs font-medium text-accent";
const chipMiss =
  "rounded-full border border-dashed border-border-strong px-2 py-0.5 text-xs text-muted";

/** 總覽的一列：整列是按鈕，按了打開那套的面板。 */
export function OutfitRow({
  outfit,
  hidden,
  edit = false,
  onOpen,
}: {
  outfit: MineOutfit;
  /** 要列小標的隱藏版（自己的隱藏版開關關著時給空的：一般畫面不出現隱藏版）。 */
  hidden: HiddenVersion[];
  /** 這個家能修改衣服（開發家）：標籤多設計中、下架（tagsOf）。 */
  edit?: boolean;
  onOpen: () => void;
}) {
  const chips = statusChips(outfit, hidden);
  return (
    <li>
      <button
        type="button"
        data-testid={`mine-row-${outfit.record}`}
        onClick={onOpen}
        className="flex w-full min-w-0 items-center gap-3 rounded-xl bg-surface-2 p-2 text-left hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
      >
        <div className="w-14 shrink-0">
          <Thumb url={thumbOf(outfit)} />
        </div>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <p className="flex min-w-0 flex-wrap items-baseline gap-x-2">
            <span className="break-words font-semibold">{outfit.name}</span>
            <span className="text-xs text-muted">{tagsOf(outfit, edit).join(" · ")}</span>
          </p>
          {chips.length > 0 ? (
            <ul className="flex flex-wrap gap-1" aria-label={T.partsLabel}>
              {chips.map((c) => (
                <li
                  key={c.key}
                  data-chip={c.key}
                  data-ok={c.ok}
                  className={c.ok ? chipOk : chipMiss}
                >
                  {c.label}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-xs text-muted">{T.lockedNote}</p>
          )}
        </div>
        <span aria-hidden="true" className="shrink-0 text-muted">
          ›
        </span>
      </button>
    </li>
  );
}
