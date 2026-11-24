import { EmptyState, LoadFailed, Spinner } from "@evergenie/ui";
import {
  ownedView,
  type Person,
  personaFromSearch,
  pickerPeople,
  SHOWROOM_HREF,
  T,
  wornImage,
} from "./lib/closet";
import {
  card,
  EmptyCloset,
  OutfitImage,
  OutfitTile,
  PersonPicker,
  RotateSwitch,
  useLoad,
  usePickedPersona,
  useRotation,
  useWear,
  VariantSwitches,
  WearControl,
} from "./lib/parts";
import { loadPeople } from "./lib/store";

const POLL_MS = 3000;

// 衣櫃（CLO-14，作者 2026-11-03）：只放已經擁有的衣服。上面一排角色頭像，一次只看一位（DEV-143；網址 ?persona=、
// 記得上次看誰）：現穿的圖、人名、擁有的衣服格；每格一顆「現在穿這套」（正在穿的寫「現在穿著」，CLO-30）與次要的輪流穿開關。沒擁有的、點數與兌換在「展示間」，設計中的在開發家角色頁的「修改」分頁（CLO-37），這頁都不出現。
// 頁面不算擁有也不算點數，只讀套件的程式算好的 person（REQ-125）。
// 有設定 button 的隱藏版，頂端畫它的開關（CLO-17，跟展示間同一排）；按了由程式換圖，這頁跟著 person 的新網址重畫。
export default function Wardrobe({ search = "" }: { search?: string }) {
  const people = useLoad(loadPeople, POLL_MS);
  const list = pickerPeople((people.data ?? []).map(ownedView));
  const [current, pick] = usePickedPersona(list, search);
  const person = list.find((p) => p.persona === current);
  const empty = people.data !== undefined && list.length === 0;
  const hasAny = (people.data ?? []).some((p) => p.outfits.length > 0);

  return (
    <section data-testid="closet" className="flex flex-col gap-4">
      <a
        href={SHOWROOM_HREF}
        data-testid="closet-to-showroom"
        className="inline-flex min-h-11 items-center self-start font-medium text-accent no-underline"
      >
        {T.toShowroom}
      </a>
      <VariantSwitches onChanged={people.reload} />
      {people.error ? (
        <LoadFailed
          what={T.loadWhat}
          auto
          onRetry={people.reload}
          variant={people.data === undefined ? "block" : "inline"}
          className={people.data === undefined ? "py-8" : undefined}
        />
      ) : null}
      {people.data === undefined ? (
        people.error ? null : (
          <div className="flex justify-center py-8">
            <Spinner label={T.loading} />
          </div>
        )
      ) : empty && hasAny ? (
        // 有衣服、只是一套都還沒擁有：到展示間兌換。
        <EmptyState title={T.emptyTitle} hint={T.emptyHint} />
      ) : empty ? (
        // 這個家一套衣服都沒有：照角色包的狀態說明（CLO-25）。
        <EmptyCloset
          wanted={personaFromSearch(search)}
          fallback={{ title: T.emptyTitle, hint: T.emptyHint }}
          onSynced={people.reload}
        />
      ) : (
        <>
          <PersonPicker people={list} current={current} onPick={pick} />
          {person ? (
            <PersonSection key={person.persona} person={person} onChanged={people.reload} />
          ) : null}
        </>
      )}
    </section>
  );
}

function PersonSection({ person, onChanged }: { person: Person; onChanged: () => void }) {
  const rot = useRotation(person, onChanged);
  const wear = useWear(person, onChanged);
  // 剛按了「現在穿這套」、頁面還沒重讀到時，上面的圖與名字先照剛換的那套畫（CLO-30）。
  const shown = wear.worn === person.worn ? person : { ...person, worn: wear.worn, wornPhoto: 1 };
  const worn = shown.outfits.find((o) => o.outfit === shown.worn);
  const img = wornImage(shown);

  return (
    <div data-testid="person-section" data-persona={person.persona} className={card}>
      <div className="flex items-center gap-3">
        <div className="size-16 shrink-0">
          <OutfitImage url={img} real testId="worn-img" round />
        </div>
        <div className="flex min-w-0 flex-col">
          <h2 className="truncate text-lg font-semibold">{person.name}</h2>
          <p data-testid="worn-name" className="truncate text-sm text-muted">
            {worn ? `${T.nowWearing}${worn.name}` : T.nobodyWearing}
          </p>
        </div>
      </div>
      {rot.error ? (
        <p role="alert" className="text-sm text-danger">
          {rot.error}
        </p>
      ) : null}
      <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3">
        {person.outfits.map((o) => (
          <OutfitTile key={o.record} outfit={o}>
            <WearControl outfit={o} wear={wear} />
            <RotateSwitch
              on={rot.current.includes(o.outfit)}
              disabled={rot.busy}
              label={`${T.rotate} ${o.name}`}
              onChange={(on) => rot.toggle(o.outfit, on)}
            />
          </OutfitTile>
        ))}
      </ul>
      <p data-testid="rotate-hint" className="text-xs text-muted">
        {T.rotateHint}
      </p>
    </div>
  );
}
