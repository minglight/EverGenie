import { api, type PersonaCard, type PersonaStyleLevel } from "@evergenie/sdk-web";
import { Avatar, ChipsInput, cn, EmptyState, Field, MoreMenu, Segmented } from "@evergenie/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { Check, ChevronRight, Download, Plus, Upload } from "lucide-react";
import { type FormEvent, useEffect, useRef, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { FormDialog } from "../../components/FormDialog";
import { Pill, type PillTone } from "../../components/Pill";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { useAgents } from "../../data/agents";
import { packageListQuery } from "../../data/queries";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { SlotPage, useSlotPages } from "../../pkg/Slot";
import { seatName } from "./functions";
import { TransferDialogs } from "./PersonaTransfer";
import {
  fieldError,
  groupPersonas,
  hasErrors,
  isDirty,
  type NewCardErrors,
  type PersonaTag,
  parseAka,
  saveFailure,
  styleDescKey,
  styleLevels,
  styleTitleKey,
  tagsOf,
  validateNewCard,
} from "./personas";
import { SLOT_PERSONAS_NEW } from "./personaTabs";
import {
  agentSettingsQuery,
  invalidatePersonas,
  personaStyleQuery,
  personasQuery,
} from "./queries";
import { ToneTest } from "./ToneTest";

const inputClass =
  "min-h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";

// ── 說話層強度 ────────────────────────────────────────────────────────────

function StyleOption({
  level,
  info,
  current,
  busy,
  onPick,
}: {
  level: (typeof styleLevels)[number];
  info: PersonaStyleLevel | undefined;
  current: boolean;
  busy: boolean;
  onPick: () => void;
}) {
  const [open, setOpen] = useState(false);
  const missing = level === "custom" && !info?.exists;
  return (
    <div className="px-4 py-2" data-testid="persona-style-option" data-level={level}>
      <button
        type="button"
        aria-pressed={current}
        disabled={missing || busy}
        onClick={onPick}
        className="flex min-h-11 w-full items-start gap-3 py-1.5 text-left focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-not-allowed"
      >
        <span
          aria-hidden="true"
          className={cn(
            "mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full border-2",
            current ? "border-accent bg-accent text-accent-fg" : "border-border-strong",
            missing && "opacity-50",
          )}
        >
          {current ? <Check className="size-3" strokeWidth={3} /> : null}
        </span>
        <span className="min-w-0 flex-1">
          {/* 選不了的那一項只把名稱與說明調淡；「為什麼選不了」照常的對比（UXC-6，P6 收尾）。 */}
          <span className={cn("block", missing && "opacity-60")}>
            <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[15px] leading-snug font-semibold">
              {t(styleTitleKey[level])}
              {current ? <Pill tone="accent">{t("settings.personas.style.current")}</Pill> : null}
            </span>
            <span className="mt-0.5 block text-[13px] leading-snug text-muted">
              {t(styleDescKey[level])}
            </span>
          </span>
          {missing ? (
            <span className="mt-0.5 block text-xs leading-snug text-attention-fg">
              {t("settings.personas.style.custom.missing")}
            </span>
          ) : null}
        </span>
      </button>
      {info?.text ? (
        <div className="pl-8">
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
            className="flex min-h-11 items-center gap-1 text-[13px] font-semibold text-accent focus-visible:outline-2 focus-visible:outline-focus"
          >
            <ChevronRight
              aria-hidden="true"
              className={cn("size-4 transition-transform", open && "rotate-90")}
            />
            {open ? t("settings.personas.style.hideFull") : t("settings.personas.style.showFull")}
          </button>
          {open ? (
            <pre
              data-testid="persona-style-text"
              className="mb-2 max-h-72 overflow-auto rounded-xl bg-surface-2 p-3 text-xs leading-relaxed break-words whitespace-pre-wrap"
            >
              {info.text}
            </pre>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

export function StyleSection() {
  const query = useQuery(personaStyleQuery);
  const client = useQueryClient();
  const [error, setError] = useState("");
  const set = useMutation({
    mutationFn: (level: string) => api.personas.setStyle({ level }),
    onMutate: () => setError(""),
    onError: (err) => setError(t("settings.personas.style.failed", { reason: reasonOf(err) })),
    onSettled: () => void client.invalidateQueries({ queryKey: ["personas", "style"] }),
  });

  if (query.data === undefined) {
    return query.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void query.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const levels = query.data.levels ?? [];
  // 送出的當下就換成新的選擇（樂觀）；失敗了 onSettled 重抓，會回到伺服器的設定。
  const shown = set.isPending ? set.variables : query.data.level;
  return (
    <section aria-labelledby="persona-style-title" className="flex flex-col gap-2">
      <div className="px-1">
        <h2 id="persona-style-title" className="text-[15px] font-semibold">
          {t("settings.personas.style.title")}
        </h2>
        <p className="text-[13px] leading-snug text-muted">{t("settings.personas.style.hint")}</p>
      </div>
      <fieldset className="m-0 border-0 p-0">
        <legend className="sr-only">{t("settings.personas.style.group")}</legend>
        <div className="divide-y divide-border overflow-hidden rounded-[18px] border border-border bg-surface shadow-[var(--shadow-card)]">
          {styleLevels.map((level) => (
            <StyleOption
              key={level}
              level={level}
              info={levels.find((l) => l.level === level)}
              current={shown === level}
              busy={set.isPending}
              onPick={() => {
                if (shown !== level) set.mutate(level);
              }}
            />
          ))}
        </div>
      </fieldset>
      {error ? (
        <p role="alert" className="px-1 text-xs leading-snug text-danger">
          {error}
        </p>
      ) : null}
    </section>
  );
}

// ── 標籤、清單 ─────────────────────────────────────────────────────────────

function TagPill({ tag }: { tag: PersonaTag }) {
  const agents = useQuery(agentSettingsQuery).data?.agents;
  let tone: PillTone = "muted";
  let label = "";
  switch (tag.kind) {
    case "official":
      label = t("settings.personas.tag.official");
      break;
    case "mine":
      tone = "accent";
      label = t("settings.personas.tag.mine");
      break;
    case "package":
      tone = "accent";
      label = t("settings.personas.tag.package", { id: tag.pkg });
      break;
    case "unassignable":
      tone = "attention";
      label = t("settings.personas.tag.unassignable", { reason: tag.reason });
      break;
    case "design":
      tone = "attention";
      label = t("settings.personas.tag.design");
      break;
    case "shadowed":
      tone = "danger";
      label = t("settings.personas.tag.shadowed");
      break;
    case "seat":
      tone = "ok";
      label = t("settings.personas.tag.seat", { name: seatName(agents, tag.agent) });
      break;
  }
  return <Pill tone={tone}>{label}</Pill>;
}

export function Tags({ card }: { card: PersonaCard }) {
  return (
    <span className="flex flex-wrap gap-1.5">
      {tagsOf(card).map((tag) => (
        <TagPill key={tag.kind === "seat" ? `seat-${tag.agent}` : tag.kind} tag={tag} />
      ))}
    </span>
  );
}

export function PersonaAvatar({ card, size }: { card: PersonaCard; size: number }) {
  const { view } = useAgents();
  const seat = view(card.seats?.[0]);
  return (
    <Avatar
      name={card.name || card.slug || "?"}
      src={card.avatar_url || undefined}
      color={seat.color}
      colorDark={seat.colorDark}
      size={size}
    />
  );
}

/** 小卡上「擔任 X、Y」：職位的名字（WEB-107），沒人擔任寫「還沒擔任職位」。 */
function SeatLine({ card }: { card: PersonaCard }) {
  const agents = useQuery(agentSettingsQuery).data?.agents;
  const seats = card.seats ?? [];
  return (
    <span data-testid="persona-seats" className="text-[13px] leading-snug break-words text-muted">
      {seats.length > 0
        ? t("settings.personas.card.seats", {
            names: seats.map((s) => seatName(agents, s)).join("、"),
          })
        : t("settings.personas.card.noSeat")}
    </span>
  );
}

/** 清單上的一張角色小卡（DEV-143）：頭像、名字、擔任的職位；設計中、被蓋住另標一個小標籤。點進去是 /personas/<代號>。 */
function PersonaTile({ card }: { card: PersonaCard }) {
  const slug = card.slug ?? "";
  const body = (
    <>
      <PersonaAvatar card={card} size={56} />
      <span className="flex min-w-0 flex-col items-center gap-1 text-center">
        <span className="max-w-full text-[15px] leading-snug font-semibold break-words">
          {card.name}
        </span>
        <SeatLine card={card} />
        {card.status === "design" || card.shadowed ? (
          <span className="flex flex-wrap justify-center gap-1">
            {card.status === "design" ? <TagPill tag={{ kind: "design" }} /> : null}
            {card.shadowed ? <TagPill tag={{ kind: "shadowed" }} /> : null}
          </span>
        ) : null}
      </span>
    </>
  );
  const cls =
    "flex h-full min-h-32 flex-col items-center gap-2 rounded-[18px] border border-border bg-surface px-2 py-3 shadow-[var(--shadow-card)]";
  // 被官方蓋住的我的卡：指派時用的是官方那張，點進去也只會看到官方的，所以只列出來、不給連結。
  if (card.shadowed) {
    return (
      <div
        data-testid="persona-row"
        data-slug={slug}
        data-source={card.source}
        className={cn(cls, "opacity-80")}
      >
        {body}
      </div>
    );
  }
  return (
    <Link
      to="/personas/$slug"
      params={{ slug }}
      aria-label={t("settings.personas.open", { name: card.name ?? slug })}
      data-testid="persona-row"
      data-slug={slug}
      data-source={card.source}
      className={cn(
        cls,
        "hover:border-border-strong focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
      )}
    >
      {body}
    </Link>
  );
}

// ── 新增、複製表單 ─────────────────────────────────────────────────────────

export function CardForm({
  from,
  onDone,
  onCancel,
}: {
  /** 有就是複製這張卡；沒有就是新增。 */
  from?: PersonaCard;
  onDone: (slug: string) => void;
  onCancel: () => void;
}) {
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [aka, setAka] = useState("");
  const [shown, setShown] = useState<NewCardErrors>({});
  const [server, setServer] = useState<{ field?: string; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const client = useQueryClient();
  const errs = validateNewCard({ slug, name });

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (hasErrors(errs)) {
      setShown(errs);
      return;
    }
    setBusy(true);
    setServer(null);
    try {
      const req = {
        slug: slug.trim(),
        name: name.trim(),
        aka: parseAka(aka),
      };
      const res = from
        ? await api.personas.copy({ ...req, from: from.slug })
        : await api.personas.create(req);
      await invalidatePersonas(client);
      // 自己的角色代號由伺服器加上 my_：用回傳的代號開她的頁
      onDone(res.persona?.slug ?? req.slug);
    } catch (err) {
      setServer(fieldError(err, ["slug", "name"]));
    } finally {
      setBusy(false);
    }
  };
  const slugText = shown.slug ? t(shown.slug) : server?.field === "slug" ? server.text : undefined;
  const nameText = shown.name ? t(shown.name) : server?.field === "name" ? server.text : undefined;
  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="flex flex-col gap-3 px-5 pb-4">
      <Field
        label={t("settings.personas.form.slug")}
        hint={t("settings.personas.form.slug.hint")}
        error={slugText}
      >
        {(control) => (
          <input
            {...control}
            value={slug}
            onChange={(e) => setSlug(e.target.value)}
            onBlur={() => setShown((s) => ({ ...s, slug: errs.slug }))}
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            autoComplete="off"
            enterKeyHint="next"
            className={inputClass}
          />
        )}
      </Field>
      <Field
        label={t("settings.personas.form.name")}
        hint={from ? t("settings.personas.copy.name.hint") : t("settings.personas.form.name.hint")}
        error={nameText}
      >
        {(control) => (
          <input
            {...control}
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={() => setShown((s) => ({ ...s, name: errs.name }))}
            autoComplete="off"
            enterKeyHint="next"
            className={inputClass}
          />
        )}
      </Field>
      <Field label={t("settings.personas.form.aka")} hint={t("settings.personas.form.aka.hint")}>
        {(control) => (
          <input
            {...control}
            value={aka}
            onChange={(e) => setAka(e.target.value)}
            autoComplete="off"
            enterKeyHint="done"
            className={inputClass}
          />
        )}
      </Field>
      {server && !server.field ? (
        <p role="alert" className="text-[13px] text-danger">
          {t("settings.personas.form.failed", { reason: server.text })}
        </p>
      ) : null}
      <div className="flex flex-wrap justify-end gap-2 pt-1">
        <ActionButton tone="outline" onClick={onCancel} disabled={busy}>
          {t("settings.personas.form.cancel")}
        </ActionButton>
        <ActionButton type="submit" tone="primary" busy={busy}>
          {t("settings.personas.form.create")}
        </ActionButton>
      </div>
    </form>
  );
}

/** 角色清單「新增角色」裡用核心表單的那一格（只寫角色卡）。 */
const CARD_ONLY = "card";

/**
 * 「新增角色」（DEV-143）：套件可以在 slot `core.personas.new` 掛別的新增方式（例：上傳半身照建角色），
 * 標題由套件給、排在前面；最後一格是核心的「只寫角色卡」。套件元件拿到 `onDone(代號)`（建好了，帶代號就開到她的頁）
 * 與 `onCancel()`；沒有套件掛時只有核心表單。
 */
function NewPersonaDialog({
  open,
  onClose,
  onDone,
}: {
  open: boolean;
  onClose: () => void;
  onDone: (slug: string) => void;
}) {
  const loaded = useSlotPages(SLOT_PERSONAS_NEW);
  const ways = loaded ?? [];
  const ready = loaded !== undefined;
  const bodyRef = useRef<HTMLDivElement>(null);
  const [picked, setPicked] = useState("");
  const current = ways.some((w) => w.id === picked)
    ? picked
    : picked === CARD_ONLY
      ? CARD_ONLY
      : (ways[0]?.id ?? CARD_ONLY);
  const way = ways.find((w) => w.id === current);
  // 套件清單讀到之前畫「載入中」，讀到後把第一次畫的那一格定下來：之後清單再變，也不把正在填的表單換掉。
  // 關掉就放開，下次打開照當時的清單重新挑。
  useEffect(() => {
    setPicked((p) => (!open ? "" : ready ? p || current : p));
  }, [open, ready, current]);
  // 表單在載入中之後才出現時，對話框開啟時的聚焦已經過了：補聚焦到第一格（FEN-24）。
  useEffect(() => {
    if (!open || !ready) return;
    const body = bodyRef.current;
    if (!body || body.contains(document.activeElement)) return;
    body.querySelector<HTMLElement>("input, select, textarea")?.focus();
  }, [open, ready]);
  return (
    <FormDialog
      open={open}
      title={t("settings.personas.create.title")}
      body={way ? undefined : t("settings.personas.create.body")}
      onClose={onClose}
    >
      {ways.length > 0 ? (
        <div className="px-5 pb-3">
          <Segmented
            ariaLabel={t("settings.personas.create.how")}
            wrap
            value={current}
            onChange={setPicked}
            options={[
              ...ways.map((w) => ({ value: w.id ?? "", label: w.title ?? w.id ?? "" })),
              { value: CARD_ONLY, label: t("settings.personas.create.cardOnly") },
            ]}
          />
        </div>
      ) : null}
      <div ref={bodyRef} className="contents">
        {!ready ? (
          <div className="px-5 pb-4">
            <LoadingBlock />
          </div>
        ) : way ? (
          <div className="px-5 pb-4" data-testid="persona-new-slot" data-page={way.id}>
            <SlotPage
              info={way}
              props={{ onDone: (slug?: string) => onDone(slug ?? ""), onCancel: onClose }}
            />
          </div>
        ) : (
          <CardForm onCancel={onClose} onDone={onDone} />
        )}
      </div>
    </FormDialog>
  );
}

function Library({ openNew }: { openNew: boolean }) {
  const query = useQuery(personasQuery);
  const navigate = useNavigate();
  const client = useQueryClient();
  const [creating, setCreating] = useState(openNew);
  const [transfer, setTransfer] = useState<"" | "export" | "import">("");
  const creatingRef = useRef(creating);
  creatingRef.current = creating;
  // 已經在清單頁時又連到 ?new=1（例：從別的頁面回來）：一樣打開。
  useEffect(() => {
    if (openNew) setCreating(true);
  }, [openNew]);

  if (query.data === undefined) {
    return query.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void query.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const cards = query.data;
  return (
    <section aria-labelledby="persona-list-title" className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 px-1">
        <h2 id="persona-list-title" className="text-[15px] font-semibold">
          {t("settings.personas.list.title")}
        </h2>
        {/* 新增、匯入、匯出收在右上的「⋯」（DEV-156 U-15）：這頁平常是來看角色的，不讓做角色的鈕搶走注意力。 */}
        <MoreMenu
          label={t("settings.personas.menu")}
          title={t("settings.personas.list.title")}
          testId="persona-list-menu"
          className="text-muted hover:bg-hover hover:text-fg"
          items={[
            {
              key: "new",
              label: t("settings.personas.new"),
              icon: <Plus aria-hidden="true" className="size-4" />,
              onSelect: () => setCreating(true),
              testId: "persona-new-open",
            },
            {
              key: "import",
              label: t("settings.personas.import.button"),
              icon: <Upload aria-hidden="true" className="size-4" />,
              onSelect: () => setTransfer("import"),
              testId: "persona-import-open",
            },
            {
              key: "export",
              label: t("settings.personas.export.button"),
              icon: <Download aria-hidden="true" className="size-4" />,
              onSelect: () => setTransfer("export"),
              testId: "persona-export-open",
            },
          ]}
        />
      </div>
      <TransferDialogs cards={cards} open={transfer} onClose={() => setTransfer("")} />
      {cards.length === 0 ? (
        <EmptyState
          title={t("settings.personas.list.empty")}
          hint={t("settings.personas.list.empty.hint")}
        />
      ) : (
        <section aria-label={t("settings.personas.list.aria")} className="flex flex-col gap-3">
          {groupPersonas(cards).map((g) => (
            <section key={g.key} data-testid="persona-group" data-group={g.key}>
              <h3 className="px-1 pb-1.5 text-xs font-semibold text-muted">
                {g.kind === "package"
                  ? t("settings.personas.group.package", { id: g.pkg })
                  : t(`settings.personas.group.${g.kind}`)}
              </h3>
              <ul className="m-0 grid list-none grid-cols-2 gap-2.5 p-0 sm:grid-cols-3 lg:grid-cols-4">
                {g.cards.map((card) => (
                  <li key={`${card.source}-${card.slug}`}>
                    <PersonaTile card={card} />
                  </li>
                ))}
              </ul>
            </section>
          ))}
        </section>
      )}
      <NewPersonaDialog
        open={creating}
        onClose={() => {
          setCreating(false);
          // ?new=1（別的頁面連來「新增角色」）只管一進來打開；關掉就拿掉，重新整理不會再跳出來。
          if (openNew) void navigate({ to: "/personas", search: {}, replace: true });
        }}
        onDone={(slug) => {
          // 視窗已經關掉才回來的結果（套件還在背景跑完）不再換頁。
          if (!creatingRef.current) return;
          setCreating(false);
          // 套件建的角色（例：上傳半身照）卡是程式寫的，清單要重讀；從 ?new=1 進來的用 replace，上一頁回原本那頁。
          void invalidatePersonas(client);
          if (slug) void navigate({ to: "/personas/$slug", params: { slug }, replace: openNew });
          else if (openNew) void navigate({ to: "/personas", search: {}, replace: true });
        }}
      />
    </section>
  );
}

// ── 一張卡的詳情 ───────────────────────────────────────────────────────────

/** 官方卡：唯讀的內文加「複製成我的」。 */
function OfficialView({ card, body }: { card: PersonaCard; body: string }) {
  const navigate = useNavigate();
  const [copying, setCopying] = useState(false);
  return (
    <>
      <p
        role="note"
        className="rounded-xl bg-surface-2 px-3 py-2.5 text-[13px] leading-snug text-muted"
      >
        {t("settings.personas.official.note")}
      </p>
      <div>
        <ActionButton tone="primary" onClick={() => setCopying(true)}>
          {t("settings.personas.copy.button")}
        </ActionButton>
      </div>
      <div className="flex flex-col gap-1.5">
        <h3 className="text-[13px] font-semibold">{t("settings.personas.detail.body")}</h3>
        <pre
          data-testid="persona-body-readonly"
          className="max-h-[60vh] overflow-auto rounded-xl border border-border bg-surface p-3 text-[13px] leading-relaxed break-words whitespace-pre-wrap"
        >
          {body}
        </pre>
      </div>
      <FormDialog
        open={copying}
        title={t("settings.personas.copy.title")}
        body={t("settings.personas.copy.body", { name: card.name ?? "" })}
        onClose={() => setCopying(false)}
      >
        <CardForm
          from={card}
          onCancel={() => setCopying(false)}
          onDone={(slug) => {
            setCopying(false);
            void navigate({ to: "/personas/$slug", params: { slug } });
          }}
        />
      </FormDialog>
    </>
  );
}

/** 套件裡的卡：唯讀內文；可複製成我的，自己套件（mine 層）裡的卡可以上線／移回設計中。 */
function PackageView({ card, body }: { card: PersonaCard; body: string }) {
  const navigate = useNavigate();
  const client = useQueryClient();
  const [copying, setCopying] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const packages = useQuery(packageListQuery());
  const own = (packages.data?.packages ?? []).some(
    (p) => p.id === card.package && p.tier === "mine",
  );
  const design = card.status === "design";
  const setStatus = async () => {
    setBusy(true);
    setError("");
    try {
      await api.personas.setStatus({ slug: card.slug ?? "", status: design ? "live" : "design" });
      await invalidatePersonas(client);
    } catch (err) {
      setError(t("settings.personas.status.failed", { reason: reasonOf(err) }));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <p
        role="note"
        className="rounded-xl bg-surface-2 px-3 py-2.5 text-[13px] leading-snug text-muted"
      >
        {t("settings.personas.package.note", { id: card.package ?? "" })}
      </p>
      <div className="flex flex-wrap gap-2">
        <ActionButton tone="primary" onClick={() => setCopying(true)}>
          {t("settings.personas.copy.button")}
        </ActionButton>
        {own ? (
          <ActionButton tone="outline" busy={busy} onClick={() => void setStatus()}>
            {design ? t("settings.personas.live.button") : t("settings.personas.design.button")}
          </ActionButton>
        ) : null}
      </div>
      {error ? (
        <p role="alert" className="text-[13px] whitespace-pre-line text-danger">
          {error}
        </p>
      ) : null}
      <div className="flex flex-col gap-1.5">
        <h3 className="text-[13px] font-semibold">{t("settings.personas.detail.body")}</h3>
        <pre
          data-testid="persona-body-readonly"
          className="max-h-[60vh] overflow-auto rounded-xl border border-border bg-surface p-3 text-[13px] leading-relaxed break-words whitespace-pre-wrap"
        >
          {body}
        </pre>
      </div>
      <FormDialog
        open={copying}
        title={t("settings.personas.copy.title")}
        body={t("settings.personas.copy.body", { name: card.name ?? "" })}
        onClose={() => setCopying(false)}
      >
        <CardForm
          from={card}
          onCancel={() => setCopying(false)}
          onDone={(slug) => {
            setCopying(false);
            void navigate({ to: "/personas/$slug", params: { slug } });
          }}
        />
      </FormDialog>
    </>
  );
}

type Confirm = "" | "live" | "design";

/** 我的卡：改名字、別名、內文（存檔帶 base_updated_at），檢查，上線／移回設計中。 */
function MineEditor({
  card,
  body,
  saved,
  onSaved,
  onReload,
  onDraft,
}: {
  card: PersonaCard;
  body: string;
  /** 剛存好（「已儲存」1.5 秒）；存完卡片會重新載入、編輯器重建，所以這個狀態放在外面。 */
  saved: boolean;
  onSaved: () => void;
  onReload: () => void;
  /** 編輯中還沒存的內容（沒有未存變更是 null），給「說話測試」帶去測。 */
  onDraft: (draft: { name: string; body: string } | null) => void;
}) {
  const client = useQueryClient();
  const seatAgents = useQuery(agentSettingsQuery).data?.agents;
  const [name, setName] = useState(card.name ?? "");
  const [aka, setAka] = useState(card.aka ?? []);
  const [text, setText] = useState(body);
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<{ kind: "conflict" | "other"; text: string } | null>(null);
  const [checking, setChecking] = useState(false);
  const [problems, setProblems] = useState<string[] | null>(null);
  const [checkError, setCheckError] = useState("");
  const [confirm, setConfirm] = useState<Confirm>("");
  const [switching, setSwitching] = useState(false);
  const [statusError, setStatusError] = useState("");

  const slug = card.slug ?? "";
  const draft = { name, aka, body: text };
  const dirty = isDirty(card, body, draft);
  const nameMissing = name.trim() === "";
  const seats = card.seats ?? [];
  const design = card.status === "design";

  useEffect(() => {
    onDraft(dirty ? { name, body: text } : null);
  }, [dirty, name, text, onDraft]);

  const save = async () => {
    if (nameMissing) return;
    setSaving(true);
    setFailure(null);
    try {
      // 只送有改的欄位：沒動名字別名就不會被撞名檢查擋住。
      await api.personas.update({
        slug,
        name: name !== card.name ? name.trim() : undefined,
        aka: aka.join("\n") !== (card.aka ?? []).join("\n") ? aka : undefined,
        body: text !== body ? text : undefined,
        base_updated_at: card.updated_at,
      });
      onSaved();
      setProblems(null);
      await invalidatePersonas(client);
    } catch (err) {
      setFailure(saveFailure(err));
    } finally {
      setSaving(false);
    }
  };

  const check = async () => {
    setChecking(true);
    setCheckError("");
    try {
      setProblems((await api.personas.check({ slug })).problems ?? []);
    } catch (err) {
      setCheckError(t("settings.personas.check.failed", { reason: reasonOf(err) }));
    } finally {
      setChecking(false);
    }
  };

  const setStatus = async (status: "live" | "design") => {
    setSwitching(true);
    setStatusError("");
    try {
      await api.personas.setStatus({ slug, status });
      setConfirm("");
      setProblems(null);
      await invalidatePersonas(client);
    } catch (err) {
      setStatusError(t("settings.personas.status.failed", { reason: reasonOf(err) }));
    } finally {
      setSwitching(false);
    }
  };

  return (
    <>
      <div className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4 shadow-[var(--shadow-card)]">
        <Field
          label={t("settings.personas.edit.name")}
          error={nameMissing ? t("settings.personas.edit.name.required") : undefined}
        >
          {(control) => (
            <input
              {...control}
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoComplete="off"
              className={inputClass}
            />
          )}
        </Field>
        <ChipsInput
          label={t("settings.personas.edit.aka")}
          hint={t("settings.personas.edit.aka.hint")}
          values={aka}
          onAdd={(v) => setAka((xs) => (xs.includes(v) ? xs : [...xs, v]))}
          onRemove={(v) => setAka((xs) => xs.filter((x) => x !== v))}
          removeLabel={(v) => t("settings.personas.edit.aka.remove", { name: v })}
        />
        <Field
          label={t("settings.personas.edit.body")}
          hint={t("settings.personas.edit.body.hint")}
        >
          {(control) => (
            <textarea
              {...control}
              value={text}
              onChange={(e) => setText(e.target.value)}
              rows={16}
              spellCheck={false}
              className={cn(inputClass, "min-h-72 py-2.5 font-mono text-[13px] leading-relaxed")}
            />
          )}
        </Field>
        {failure ? (
          <div role="alert" className="flex flex-col gap-2 text-[13px] text-danger">
            <p>
              {failure.kind === "conflict"
                ? t("settings.personas.edit.conflict")
                : t("settings.personas.edit.failed", { reason: failure.text })}
            </p>
            {failure.kind === "conflict" ? (
              <div>
                <ActionButton tone="outline" size="card" onClick={onReload}>
                  {t("settings.personas.edit.reload")}
                </ActionButton>
              </div>
            ) : null}
          </div>
        ) : null}
        <div className="flex flex-wrap items-center gap-3">
          <ActionButton
            tone="primary"
            busy={saving}
            disabled={!dirty || nameMissing}
            onClick={() => void save()}
          >
            {t("settings.personas.edit.save")}
          </ActionButton>
          <span role="status" aria-live="polite" className="text-[13px]">
            {saved ? (
              <span className="flex items-center gap-1 font-medium text-ok">
                <Check aria-hidden="true" className="size-4" strokeWidth={2.2} />
                {t("settings.personas.edit.saved")}
              </span>
            ) : dirty ? (
              <span className="text-muted">{t("settings.personas.edit.dirty")}</span>
            ) : null}
          </span>
        </div>
      </div>

      <div className="flex flex-col gap-3 rounded-[18px] border border-border bg-surface p-4 shadow-[var(--shadow-card)]">
        <div className="flex flex-wrap gap-2">
          <ActionButton tone="outline" busy={checking} onClick={() => void check()}>
            {t("settings.personas.check.button")}
          </ActionButton>
          {confirm === "" ? (
            design ? (
              <ActionButton
                tone="primary"
                disabled={dirty}
                onClick={() => setConfirm("live")}
                data-testid="persona-go-live"
              >
                {t("settings.personas.live.button")}
              </ActionButton>
            ) : (
              <ActionButton
                tone="outline"
                disabled={seats.length > 0}
                onClick={() => setConfirm("design")}
                data-testid="persona-go-design"
              >
                {t("settings.personas.design.button")}
              </ActionButton>
            )
          ) : null}
        </div>
        {design && dirty ? (
          <p className="text-[13px] leading-snug text-muted">
            {t("settings.personas.live.needSave")}
          </p>
        ) : null}
        {!design && seats.length > 0 ? (
          <p className="text-[13px] leading-snug text-muted">
            {t("settings.personas.design.seated", {
              names: seats.map((s) => seatName(seatAgents, s)).join("、"),
            })}
          </p>
        ) : null}
        {confirm !== "" ? (
          <div className="flex flex-col gap-2 border-t border-border pt-3">
            <p className="text-[13px] leading-snug text-muted">
              {t(
                confirm === "live"
                  ? "settings.personas.live.confirm"
                  : "settings.personas.design.confirm",
              )}
            </p>
            <div className="flex flex-wrap gap-2">
              <ActionButton
                tone={confirm === "live" ? "primary" : "danger"}
                size="card"
                busy={switching}
                onClick={() => void setStatus(confirm)}
                data-testid="persona-confirm-status"
              >
                {t(
                  confirm === "live"
                    ? "settings.personas.live.yes"
                    : "settings.personas.design.yes",
                )}
              </ActionButton>
              <ActionButton tone="quiet" size="card" onClick={() => setConfirm("")}>
                {t("ui.cancel")}
              </ActionButton>
            </div>
          </div>
        ) : null}
        {statusError ? (
          <p role="alert" className="text-[13px] whitespace-pre-line text-danger">
            {statusError}
          </p>
        ) : null}
        {checkError ? (
          <p role="alert" className="text-[13px] text-danger">
            {checkError}
          </p>
        ) : null}
        {problems ? (
          <div role="status" data-testid="persona-check-result" className="flex flex-col gap-1.5">
            {problems.length === 0 ? (
              <p className="flex items-center gap-1.5 text-[14px] font-medium text-ok">
                <Check aria-hidden="true" className="size-4" strokeWidth={2.2} />
                {t("settings.personas.check.ok")}
              </p>
            ) : (
              <>
                <p className="text-[14px] font-semibold">
                  {t("settings.personas.check.found", { n: problems.length })}
                </p>
                <ul className="m-0 flex list-disc flex-col gap-1 pl-5 text-[13px] leading-snug">
                  {problems.map((p) => (
                    <li key={p} className="break-words">
                      {p}
                    </li>
                  ))}
                </ul>
              </>
            )}
          </div>
        ) : null}
      </div>
    </>
  );
}

/**
 * 一位角色的「介紹」分頁（DEV-143，原 ?card= 的詳情）：官方卡唯讀＋複製、套件卡、我的卡的編輯與上線、說話測試。
 * 擔任哪個職位、匯出由角色頁（PersonaPage）放在這段後面。
 */
export function PersonaIntro({
  card,
  body,
  onDirty,
}: {
  card: PersonaCard;
  body: string;
  /** 有沒有還沒存的編輯（FEN-23）：角色頁拿來在換分頁或離開前提醒。 */
  onDirty?: (dirty: boolean) => void;
}) {
  const client = useQueryClient();
  const [saved, setSaved] = useState(false);
  const [draft, setDraft] = useState<{ name: string; body: string } | null>(null);
  const dirty = draft !== null;

  useEffect(() => {
    onDirty?.(dirty);
  }, [dirty, onDirty]);
  // 收起來（換角色、換分頁）就沒有未存的了。
  useEffect(() => () => onDirty?.(false), [onDirty]);

  // 存完 1.5 秒後收起「已儲存」（FEN-13）。
  useEffect(() => {
    if (!saved) return;
    const id = window.setTimeout(() => setSaved(false), 1500);
    return () => window.clearTimeout(id);
  }, [saved]);

  const reload = () => void client.invalidateQueries({ queryKey: ["personas"] });
  return (
    <>
      {card.shadowed ? (
        <p role="note" className="rounded-xl bg-danger-soft px-3 py-2.5 text-[13px] text-danger">
          {t("settings.personas.shadowed.note")}
        </p>
      ) : null}
      {card.source === "official" ? (
        <OfficialView card={card} body={body} />
      ) : card.source === "package" ? (
        <PackageView card={card} body={body} />
      ) : (
        // updated_at 變了（存檔、上線、重新載入）就整個重建，輸入框回到伺服器的內容。
        <MineEditor
          key={card.updated_at}
          card={card}
          body={body}
          saved={saved}
          onSaved={() => setSaved(true)}
          onReload={reload}
          onDraft={setDraft}
        />
      )}
      <ToneTest card={card} draft={card.source === "mine" ? draft : null} />
    </>
  );
}

// 「角色」頁的清單（REQ-130；DEV-117 從設定搬到 /personas；DEV-143 一位角色一頁）：角色小卡、新增、匯入匯出、說話層強度；
// 點小卡進 /personas/<代號>（PersonaPage）。角色清單放上面：說話層四個選項很高，放前面手機要捲一整頁才看得到角色（WEB-108）。
export function PersonasSettings() {
  const search = useSearch({ strict: false }) as { new?: boolean };
  return (
    <div className="flex flex-col gap-6">
      <Library openNew={search.new === true} />
      <StyleSection />
    </div>
  );
}
