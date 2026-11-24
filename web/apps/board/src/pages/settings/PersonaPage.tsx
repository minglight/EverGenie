import type { PersonaCard } from "@evergenie/sdk-web";
import { api } from "@evergenie/sdk-web";
import { Field } from "@evergenie/ui";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useBlocker, useNavigate, useParams, useSearch } from "@tanstack/react-router";
import { Download } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { PageTabs } from "../../components/PageTabs";
import { LoadingBlock, NotFoundBlock } from "../../components/StateViews";
import { t } from "../../i18n/zh-TW";
import { useIsDesktop } from "../../lib/media";
import { reasonOf } from "../../lib/reason";
import { SlotPage, useSlotPages } from "../../pkg/Slot";
import { seatName } from "./functions";
import { PersonaAvatar, PersonaIntro, Tags } from "./PersonasSettings";
import { ExportDialog } from "./PersonaTransfer";
import { assignablePersonas } from "./personas";
import {
  currentPersonaTab,
  INTRO_TAB,
  itemSearch,
  personaTabs,
  SLOT_PERSONA_TAB,
  tabSearch,
} from "./personaTabs";
import { agentSettingsQuery, invalidatePersonas, personaQuery } from "./queries";
import { exportable } from "./transfer";

const inputClass =
  "min-h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus";
const cardClass =
  "flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4 shadow-[var(--shadow-card)]";

// 一位角色一頁（DEV-143，作者 2026-11-08）：/personas/<代號>，上面是頭像、名字、標籤，下面用分頁切換、不捲過別的角色。
// 「介紹」是核心的（角色卡、說話測試、擔任哪個職位、匯出、複製…）；其他分頁由套件掛在 slot core.persona.tab，
// 標題由套件給、元件拿到 `persona`（角色代號）。分頁記在 ?tab=，換分頁用 replace，上一頁直接回清單。
// 套件分頁裡「現在打開哪一項」記在 ?item=（核心不解讀）：元件拿到 `item` 與 `onItem(id)`（空字串＝關掉），
// 重新整理回同一個畫面（UXC-3）；一樣用 replace，換分頁時拿掉。
export function PersonaPage() {
  const { slug } = useParams({ strict: false }) as { slug: string };
  const { tab, item } = useSearch({ strict: false }) as { tab?: string; item?: string };
  const desktop = useIsDesktop();
  const query = useQuery(personaQuery(slug));
  const pages = useSlotPages(SLOT_PERSONA_TAB);
  const navigate = useNavigate();
  const setItem = useCallback(
    (id: string) =>
      void navigate({
        to: "/personas/$slug",
        params: { slug },
        search: itemSearch(tab, id),
        replace: true,
      }),
    [navigate, slug, tab],
  );
  // 介紹分頁有還沒存的角色卡編輯時，換分頁、離開都先在畫面裡問「要放棄嗎」（FEN-23；不用瀏覽器 confirm，FEN-N6）。
  const [dirty, setDirty] = useState(false);
  const dirtyRef = useRef(false);
  dirtyRef.current = dirty;
  const blocker = useBlocker({
    shouldBlockFn: () => dirtyRef.current,
    enableBeforeUnload: () => dirtyRef.current,
    withResolver: true,
  });
  // ?tab= 指的分頁已經不在（套件停用或網址打錯）：頁面清單載完後用 replace 拿掉，網址跟畫面一致。
  const staleTab =
    tab !== undefined &&
    pages !== undefined &&
    !pages.some((p) => p.id === tab) &&
    tab !== INTRO_TAB;
  useEffect(() => {
    if (staleTab)
      void navigate({ to: "/personas/$slug", params: { slug }, search: {}, replace: true });
  }, [staleTab, slug, navigate]);

  let content: ReactNode;
  if (query.data === undefined) {
    content = query.isError ? (
      <NotFoundBlock
        title={t("settings.personas.notFound")}
        hint={t("settings.personas.notFound.hint")}
        backLabel={t("ui.notFound.back", { where: t("page.personas") })}
        to="/personas"
      />
    ) : (
      <LoadingBlock />
    );
  } else if (!query.data.persona) {
    content = <LoadingBlock />;
  } else {
    const card = query.data.persona;
    const tabs = personaTabs(t("settings.personas.tab.intro"), pages ?? []);
    const current = currentPersonaTab(tabs, tab, pages !== undefined);
    const page = (pages ?? []).find((p) => p.id === current);
    content = (
      <div className="flex flex-col gap-4" data-testid="persona-detail" data-slug={slug}>
        <div className="flex items-center gap-3">
          <PersonaAvatar card={card} size={64} />
          <div className="flex min-w-0 flex-1 flex-col gap-1.5">
            {desktop ? (
              <h1 className="text-2xl leading-snug font-bold break-words">{card.name}</h1>
            ) : (
              <h2 className="text-lg leading-snug font-bold break-words">{card.name}</h2>
            )}
            {card.aka && card.aka.length > 0 ? (
              <p className="text-[13px] leading-snug break-words text-muted">
                {t("settings.personas.aka", { names: card.aka.join("、") })}
              </p>
            ) : null}
            <Tags card={card} />
          </div>
        </div>
        {tabs.length > 1 ? (
          <PageTabs
            label={t("settings.personas.tabs", { name: card.name ?? slug })}
            tabs={tabs.map((x) => ({
              key: x.key,
              to: `/personas/${encodeURIComponent(slug)}`,
              search: tabSearch(x.key),
              replace: true,
              label: x.label,
              current: x.key === current,
            }))}
          />
        ) : null}
        {current === null ? (
          <LoadingBlock />
        ) : current === INTRO_TAB ? (
          <div key={slug} className="flex flex-col gap-4" data-testid="persona-tab-intro">
            <PersonaIntro card={card} body={query.data.body ?? ""} onDirty={setDirty} />
            <PersonaSeats card={card} />
            <ExportOne card={card} />
          </div>
        ) : page ? (
          <div data-testid="persona-tab" data-tab={page.id}>
            {/* key：換角色或換分頁時整個重掛，套件元件從新的角色開始。 */}
            <SlotPage
              key={`${slug}:${page.id}`}
              info={page}
              props={{ persona: slug, item: item ?? "", onItem: setItem }}
            />
          </div>
        ) : null}
      </div>
    );
  }
  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 px-4 pt-3 pb-7">
      {content}
      <ConfirmDialog
        open={blocker.status === "blocked"}
        title={t("settings.personas.leave.title")}
        body={t("settings.personas.leave.body")}
        confirmLabel={t("settings.personas.leave.yes")}
        cancelLabel={t("settings.personas.leave.no")}
        onConfirm={() => {
          dirtyRef.current = false;
          blocker.proceed?.();
        }}
        onCancel={() => blocker.reset?.()}
      />
    </div>
  );
}

/**
 * 擔任哪個職位（指派，DEV-143 從套件的上傳頁搬到核心）：列出她擔任的職位，選一個職位、再按「換」才呼叫 `agents.assign`
 * （跟「職位與秘書」同一個 API），原本擔任那個職位的角色就空出來。設計中、被蓋住、暫時不能指派的卡不給選（D-31）。
 * 職位寫名字（職責第一段，WEB-107），不寫 id。
 */
function PersonaSeats({ card }: { card: PersonaCard }) {
  const client = useQueryClient();
  const agents = useQuery(agentSettingsQuery).data?.agents;
  const slug = card.slug ?? "";
  const seats = card.seats ?? [];
  const canAssign = assignablePersonas([card]).length > 0;
  // web.get_agents 列名冊全部（含停用）；停用的職位不能換角色，不放進選單。
  const choices = (agents ?? []).filter((a) => a.id && a.enabled && !seats.includes(a.id));
  const [pick, setPick] = useState("");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const chosen = choices.find((a) => a.id === pick);
  const seatTitle = (id: string) => seatName(agents, id);

  const confirm = async () => {
    if (!chosen?.id) return;
    setBusy(true);
    setNote(null);
    try {
      await api.agents.assign({ id: chosen.id, persona: slug });
      setNote({ ok: true, text: t("settings.personas.seat.done", { seat: seatTitle(chosen.id) }) });
      setPick("");
      await invalidatePersonas(client);
    } catch (err) {
      setNote({ ok: false, text: t("settings.personas.seat.failed", { reason: reasonOf(err) }) });
    } finally {
      setBusy(false);
    }
  };

  return (
    <section data-testid="persona-seat" aria-labelledby="persona-seat-title" className={cardClass}>
      <div>
        <h3 id="persona-seat-title" className="text-[15px] font-semibold">
          {t("settings.personas.seat.title")}
        </h3>
        <p data-testid="persona-seat-now" className="text-[13px] leading-snug text-muted">
          {seats.length > 0
            ? t("settings.personas.card.seats", { names: seats.map(seatTitle).join("、") })
            : t("settings.personas.card.noSeat")}
        </p>
      </div>
      {canAssign ? (
        <>
          <Field label={t("settings.personas.seat.pick")} hint={t("settings.personas.seat.hint")}>
            {(control) => (
              <select
                {...control}
                data-testid="persona-seat-pick"
                value={pick}
                disabled={busy || agents === undefined}
                onChange={(e) => {
                  setPick(e.target.value);
                  setNote(null);
                }}
                className={inputClass}
              >
                <option value="">{t("settings.personas.seat.choose")}</option>
                {choices.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name
                      ? t("settings.personas.seat.option", {
                          seat: seatTitle(a.id ?? ""),
                          name: a.name,
                        })
                      : seatTitle(a.id ?? "")}
                  </option>
                ))}
              </select>
            )}
          </Field>
          {chosen ? (
            <div className="flex flex-col gap-2 rounded-xl bg-surface-2 p-3">
              <p className="text-[13px] leading-snug">
                {t("settings.personas.seat.ask", {
                  seat: seatTitle(chosen.id ?? ""),
                  from: chosen.name || t("settings.personas.seat.empty"),
                  to: card.name ?? slug,
                })}
              </p>
              <div className="flex flex-wrap gap-2">
                <ActionButton
                  tone="primary"
                  size="card"
                  busy={busy}
                  onClick={() => void confirm()}
                  data-testid="persona-seat-yes"
                >
                  {t("settings.personas.seat.yes")}
                </ActionButton>
                <ActionButton tone="quiet" size="card" disabled={busy} onClick={() => setPick("")}>
                  {t("ui.cancel")}
                </ActionButton>
              </div>
            </div>
          ) : null}
        </>
      ) : (
        <p className="text-[13px] leading-snug text-muted">{t("settings.personas.seat.cannot")}</p>
      )}
      {note ? (
        <p
          role={note.ok ? "status" : "alert"}
          className={note.ok ? "text-[13px] font-medium text-ok" : "text-[13px] text-danger"}
        >
          {note.text}
        </p>
      ) : null}
    </section>
  );
}

/** 「匯出這位」：跟清單的匯出同一個對話框，一打開就勾好她；套件帶來的、被蓋住的不能單獨匯出，不畫。 */
function ExportOne({ card }: { card: PersonaCard }) {
  const [open, setOpen] = useState(false);
  if (exportable([card]).length === 0) return null;
  return (
    <div>
      <ActionButton tone="outline" onClick={() => setOpen(true)} data-testid="persona-export-one">
        <Download aria-hidden="true" className="size-4" />
        {t("settings.personas.export.one")}
      </ActionButton>
      <ExportDialog
        cards={[card]}
        preset={[card.slug ?? ""]}
        open={open}
        onClose={() => setOpen(false)}
      />
    </div>
  );
}
