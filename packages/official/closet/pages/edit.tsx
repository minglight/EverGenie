// 角色頁（/personas/<代號>）最後一個分頁「修改」（DEV-160 S6，CLO-35）：開發家專屬——一個地方看到一位角色所有衣服的每一張照片、
// 所有狀態，能改狀態（含放回開發中）、能評論、能升成官方（DEV-160 審查 S1：開發／設計用的只在這一頁）。這頁不判斷是哪種家：package.toml 的 show_when = "cap:closet.edit"，沒有這個能力核心就不列這個分頁。
// 篩選放在角色頁網址的 ?item=（onItem，重新整理還在）；上一頁由看板的外殼畫（FEN-29），這頁不畫返回。
import { onLiveChange } from "@evergenie/sdk-web";
import { EmptyState, LoadFailed, Spinner } from "@evergenie/ui";
import { useCallback, useEffect, useState } from "react";
import { actionsFor } from "./lib/caps";
import {
  buildView,
  filterActions,
  hasListableHidden,
  isOwnerHidden,
  parseFilters,
  photoUrl,
  promotableOf,
  T,
} from "./lib/edit";
import { FilterBar, OutfitCard } from "./lib/editParts";
import { useLoad } from "./lib/parts";
import { loadEditData } from "./lib/store";

export default function Edit({
  persona,
  item,
  onItem,
}: {
  persona: string;
  item?: string;
  onItem?: (id: string) => void;
}) {
  // 秘書或別的分頁改了資料，頁面跟著重讀（PKG-49）；另外每 5 秒再看一次（照片紀錄、檢視由程式算好才會更新）。
  const data = useLoad(loadEditData, 5000);
  const { reload } = data;
  // 要聽的種類前綴：衣櫃自己的，加上外掛動作的狀態紀錄所屬的套件（例 demo.）。
  const prefixes = [
    ...new Set([
      "closet.",
      ...(data.data?.caps.actions ?? []).flatMap((a) =>
        a.status ? [`${a.status.kind.split(".")[0]}.`] : [],
      ),
    ]),
  ].join(" ");
  // 部署前就開著的舊看板外殼沒有 onLiveChange（共用模組是外殼給的）：沒有就不接，靠定時重讀（PKG-49）。
  useEffect(() => {
    if (typeof onLiveChange !== "function") return undefined;
    const offs = prefixes.split(" ").map((p) => onLiveChange(p, reload));
    return () => {
      for (const off of offs) off();
    };
  }, [prefixes, reload]);
  const filters = parseFilters(item ?? "");
  const setItem = useCallback((next: string) => onItem?.(next), [onItem]);
  // 升成官方正式送出的結果（那一塊升了就換成官方的那筆，結果放在分頁最上面，不跟著那塊消失）。
  const [promoted, setPromoted] = useState<{ text: string; ok: boolean } | null>(null);

  if (data.data === undefined) {
    return data.error ? (
      <LoadFailed what={T.loadWhat} auto onRetry={reload} className="py-8" />
    ) : (
      <div className="flex justify-center py-6">
        <Spinner label={T.loading} />
      </div>
    );
  }
  const d = data.data;
  const sections = buildView({
    persona,
    filters,
    outfits: d.outfits,
    photos: d.photos,
    people: d.people,
    caps: d.caps,
    statuses: d.statuses,
  });
  const personaName = d.people.find((p) => p.persona === persona)?.name || persona;
  const photoActions = actionsFor(d.caps, "photo");
  const outfitActions = actionsFor(d.caps, "outfit");
  const url = (p: Parameters<typeof photoUrl>[0]) => photoUrl(p, d.people);
  const ownerHidden = (p: Parameters<typeof isOwnerHidden>[0]) => isOwnerHidden(p, d.people);
  const total = sections.reduce((n, s) => n + s.total, 0);
  const shown = sections.reduce((n, s) => n + s.blocks.length, 0);

  return (
    <section data-testid="closet-edit" className="flex min-w-0 flex-col gap-4">
      <FilterBar
        personaName={personaName}
        filters={filters}
        actions={filterActions(d.caps)}
        showHidden={hasListableHidden(d.photos, d.people, persona, filters.all)}
        onChange={setItem}
      />
      {data.error ? <LoadFailed what={T.loadWhat} auto onRetry={reload} variant="inline" /> : null}
      {promoted ? (
        <p
          role={promoted.ok ? "status" : "alert"}
          data-testid="edit-promote-done"
          data-ok={promoted.ok}
          className={`m-0 text-sm font-medium ${promoted.ok ? "text-ok" : "text-danger"}`}
        >
          {promoted.text}
        </p>
      ) : null}
      {total === 0 ? (
        <EmptyState
          tone="missing"
          title={filters.all ? T.emptyAllTitle : T.emptyTitle}
          hint={T.emptyHint}
        />
      ) : shown === 0 ? (
        <EmptyState title={T.noMatch} hint={T.noMatchHint} />
      ) : (
        sections.map((s) =>
          s.blocks.length === 0 ? null : (
            <div
              key={s.persona}
              data-testid="edit-persona"
              data-persona={s.persona}
              className="flex min-w-0 flex-col gap-3"
            >
              {filters.all ? <h3 className="m-0 text-base font-semibold">{s.name}</h3> : null}
              {s.blocks.map((b) => (
                <OutfitCard
                  key={b.outfit.record}
                  person={{ persona: s.persona, name: s.name }}
                  block={b}
                  photoUrl={url}
                  ownerHidden={ownerHidden}
                  promotable={d.mine && promotableOf(d.people, b.outfit)}
                  notes={d.notes}
                  outfitActions={outfitActions}
                  photoActions={photoActions}
                  statuses={d.statuses}
                  heading={filters.all ? "h4" : "h3"}
                  onChanged={reload}
                  onPromoted={(text, ok) => {
                    setPromoted({ text, ok });
                    reload();
                  }}
                />
              ))}
            </div>
          ),
        )
      )}
    </section>
  );
}
