import { Button, EmptyState, LoadFailed, Spinner } from "@evergenie/ui";
import { useState } from "react";
import { canEdit } from "./lib/caps";
import { AddOutfit, MineOutfitCard, OfficialOutfitCard } from "./lib/forms";
import { closetHref, deletedOf, liveOutfits, photoGroups, sortOutfits, T } from "./lib/mine";
import { card, ResultLine, useLoad, useSender } from "./lib/parts";
import { DELETED_AT, DeletedList } from "./lib/photos";
import { OutfitRow } from "./lib/status";
import { loadAdultOn, loadCharacters, loadRecords, loadSupports } from "./lib/store";

const POLL_MS = 3000;
const linkCls =
  "inline-flex min-h-11 items-center rounded-lg px-1 text-sm font-medium text-accent no-underline focus-visible:outline-2 focus-visible:outline-focus";

// 角色頁的「衣服」分頁（DEV-143，作者 2026-11-08）：掛在核心的 slot core.persona.tab，核心給 `persona`（角色代號）。
// 總覽一套一列（作者 2026-11-08：一位角色看得到每套衣服、每張照片的狀態，不要一直捲）；點一套換成那套的面板：
// 每張照片一列（換一張、加動畫、刪掉，lib/photos.tsx）、加照片與全身照、要不要用點數換、刪掉這套。「加一套衣服」按了才展開；
// 總覽最下面是「刪掉的」，可以拿回來。兌換與詳情在衣櫃（closet）的頁，這裡只連過去。只能從角色頁打開（掛在 slot 的頁面沒有自己的網址）。
// 一般家與開發家長得一樣（DEV-160 審查 S1，CMI-24）：開發／設計用的（採用、評論、整套的狀態、放回開發中、升成官方）
// 全在衣櫃的「修改」分頁（CLO-35），這裡不放。只差在這個家支援什麼（closet.capabilities 的 edit，不問是哪種家）：
// 開發家標籤多設計中、下架；一般家沒有下架，我的下架的那套列在「刪掉的」（拿回來＝改回上架）。隱藏版只在自己的隱藏版開關開著時出現。
// 現在打開哪一套記在角色頁網址的 ?item=<衣服編號>（核心給 `item` 與 `onItem`，board 卡 WEB-163）：重新整理回同一套（UXC-3），
// 衣櫃的衣服詳情頁也能直接連到某一套（CLO-27）。
export default function Outfits({
  persona = "",
  item = "",
  onItem,
}: {
  persona?: string;
  /** 網址 ?item= 的值：打開哪一套（closet.outfit 編號）；空字串＝總覽。 */
  item?: string;
  /** 換一套或回總覽（空字串）；核心把它寫進網址。 */
  onItem?: (id: string) => void;
}) {
  const code = persona;
  const chars = useLoad(loadCharacters, POLL_MS);
  // 照片紀錄與衣服紀錄（含刪掉的）：每張照片的鈕、「刪掉的」清單。
  const records = useLoad(() => loadRecords(code), POLL_MS);
  // 自己的隱藏版開關（衣櫃的 closet.adult）：開著才出現「放進自己的隱藏版」與隱藏版的照片，關著完全不出現。
  const adultLoad = useLoad(loadAdultOn, POLL_MS);
  // 這個家支援什麼（衣櫃的 closet.capabilities）：有 edit（開發家）才標設計中、下架；還沒有這筆（衣櫃還沒算好）、封存了當沒有。
  // 跟著輪詢，衣櫃剛寫好也跟得上。讀到之前整頁先轉圈、讀失敗顯示讀不到：不先照一般家畫（開發家下架的那套會先閃進「刪掉的」，FEN-N1）。
  const supports = useLoad(loadSupports, POLL_MS);
  const edit = canEdit(supports.data);
  const adult = adultLoad.data === true;
  const sender = useSender(() => {
    chars.reload();
    records.reload();
  });
  const current = chars.data?.find((c) => c.persona === code);
  const open = item;
  const setOpen = (id: string) => onItem?.(id);
  const [adding, setAdding] = useState(false);

  if (chars.data === undefined || supports.data === undefined) {
    const failed = chars.data === undefined ? chars : supports;
    return failed.error ? (
      <LoadFailed what={T.loadWhat} auto onRetry={failed.reload} className="py-8" />
    ) : (
      <div className="flex justify-center py-6">
        <Spinner label={T.loading} />
      </div>
    );
  }
  if (!current) {
    return (
      <section data-testid="mine-outfits" className="flex flex-col gap-4">
        <EmptyState tone="missing" title={T.notFound} hint={T.notFoundHint} />
        {code.startsWith("my") ? <AddOutfit sender={sender} persona={code} /> : null}
      </section>
    );
  }
  const outfits = sortOutfits(liveOutfits(current.outfits, records.data?.outfits, edit));
  const picked = outfits.find((o) => o.record === open);
  const hidden = adult ? current.hidden : [];
  const deleted = records.data
    ? deletedOf(records.data.outfits, records.data.photos, adult, edit)
    : [];
  return (
    <section
      data-testid="mine-outfits"
      data-persona={current.persona}
      data-edit={edit}
      className="flex flex-col gap-3"
    >
      <ResultLine sender={sender} />
      {picked ? (
        <div data-testid="mine-outfit-panel" className="flex flex-col gap-3">
          <button type="button" onClick={() => setOpen("")} className={`${linkCls} self-start`}>
            ‹ {T.backToAll}
          </button>
          {picked.source !== "mine" ? (
            <ul className="flex flex-col">
              <OfficialOutfitCard outfit={picked} edit={edit} />
            </ul>
          ) : records.data ? (
            <ul className="flex flex-col">
              <MineOutfitCard
                key={picked.record}
                persona={current.persona}
                outfit={picked}
                groups={photoGroups(records.data.photos, picked.record, current.photoUrls, adult)}
                sender={sender}
                adult={adult}
                edit={edit}
                onRemoved={() => setOpen("")}
              />
            </ul>
          ) : records.error ? (
            <LoadFailed what={T.loadWhat} auto onRetry={records.reload} className="py-6" />
          ) : (
            <div className="flex justify-center py-6">
              <Spinner label={T.loading} />
            </div>
          )}
        </div>
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
            <p className="min-w-0 text-sm text-muted">{T.outfitsHint}</p>
            <div className="flex flex-wrap gap-x-3">
              <a href={closetHref("wardrobe", current.persona)} className={linkCls}>
                {T.toWardrobe}
              </a>
              <a href={closetHref("showroom", current.persona)} className={linkCls}>
                {T.toShowroom}
              </a>
            </div>
          </div>
          {outfits.length === 0 ? (
            <p className={`${card} text-sm text-muted`}>{T.outfitsEmpty}</p>
          ) : (
            <ul data-testid="mine-rows" className="flex flex-col gap-1.5">
              {outfits.map((o) => (
                <OutfitRow
                  key={o.record}
                  outfit={o}
                  hidden={hidden}
                  edit={edit}
                  onOpen={() => setOpen(o.record)}
                />
              ))}
            </ul>
          )}
          {adding || outfits.length === 0 ? (
            <AddOutfit sender={sender} persona={current.persona} />
          ) : (
            <Button variant="secondary" className="self-start" onClick={() => setAdding(true)}>
              {T.addOutfitOpen}
            </Button>
          )}
          <DeletedList persona={current.persona} items={deleted} sender={sender} />
          <ResultLine sender={sender} at={DELETED_AT} />
        </>
      )}
    </section>
  );
}
