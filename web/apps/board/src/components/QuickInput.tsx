import { ApiError, type Cycle, type Difficulty, type Follow, upload } from "@evergenie/sdk-web";
import {
  attachmentLine,
  cn,
  Field,
  Segmented,
  type SegmentedOption,
  Sheet,
  Spinner,
  showToast,
} from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  AlarmClock,
  ArrowUp,
  ChevronDown,
  ClipboardList,
  FileText,
  MessageCircle,
  Paperclip,
  Plus,
  Sparkles,
  X,
} from "lucide-react";
import { type ClipboardEvent, type KeyboardEvent, useId, useMemo, useRef, useState } from "react";
import { useAgentFilter } from "../data/agentFilter";
import { agentKey, splitAgentKey, useAgents } from "../data/agents";
import { createReminder, sayToAgent } from "../data/quick";
import { homeSearch, type Site, siteUsable } from "../data/sites";
import { createTask } from "../data/taskCreate";
import { t } from "../i18n/zh-TW";
import { isImageName, namedFile } from "../lib/attachments";
import { useDraft } from "../lib/draft";
import { fullTime } from "../lib/format";
import { HIT_44 } from "../lib/hit";
import { type AgentRef, matchAgent, parseIntent } from "../lib/intent";
import { lastTaskAgentIn, writeLastTaskAgent } from "../lib/lastAgent";
import { useIsDesktop } from "../lib/media";
import { ActionButton } from "./ActionButton";
import { AppWhenField } from "./AppWhenField";
import { enabledAgentKeys, resolveAgentKey, SiteAgentPicker, useSiteTarget } from "./SitePick";
import { SitePickChip, SiteTag, siteName, useMultiSite } from "./SiteTag";

// 貼上的圖片（待辦頁「更多選項」也能選檔）：上傳到 /share/ 後，跟秘書說時接在話的後面、一張一行（跟聊天框一樣的附件行，DEV-20）；
// 記成待辦或開任務單時變成單的附件（RMD-23、TSK-28）。
interface Pic {
  id: string;
  name: string;
  /** 原檔：要送到別台時在那台再傳一次（附件存在每台自己的 /share/，S3）。 */
  file: File;
  url?: string;
  failed?: boolean;
}

export interface QuickInputProps {
  /** today：「今天」頁最上面；reminders：/reminders 頂端（多一個「更多選項」）。 */
  variant: "today" | "reminders";
  className?: string;
  /** 待辦記好之後（待辦頁用它把分段切到看得到新待辦的那一個）。`at` 是新待辦的時間，沒有時間是 null。 */
  onTodoCreated?: (at: Date | null) => void;
}

const DEFAULT_OWNER = "life";
const followKeys = ["none", "remind", "confirm", "check", "discuss"] as const satisfies Follow[];
const cycleKeys = ["once", "daily", "weekly", "biweekly", "monthly"] as const satisfies Cycle[];

const chipBase = "inline-flex items-center rounded-full px-2.5 py-1 text-[13px] leading-snug";
// 膠囊視覺上高 26～28，點擊範圍用 ::after 補到 44（FEN-7）。
const hitArea = `${HIT_44} focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus`;

function errorText(err: unknown): string {
  return err instanceof ApiError ? err.message : String(err);
}

// 萬用輸入框（補充契約 S5 第 2 點）：開頭 @ → 傳話給秘書；其他 → 新增待辦（時間從句子裡抓）。
// 預覽列顯示意圖、時間、內容、負責秘書的膠囊，可以點來改；Enter 或送出鈕就送。
export function QuickInput({ variant, className, onTodoCreated }: QuickInputProps) {
  const client = useQueryClient();
  const navigate = useNavigate();
  const desktop = useIsDesktop();
  const { view } = useAgents();
  const multi = useMultiSite();
  const inputId = useId();
  const inputRef = useRef<HTMLInputElement>(null);
  const [text, setText, clearText] = useDraft(`eg.draft.quick.${variant}`);
  const [ignoreWhen, setIgnoreWhen] = useState(false);
  const [override, setOverride] = useState<Date | null>(null);
  // 秘書篩選剛好選一位時，負責秘書預設帶那位（自己點過 chip 改的優先）。
  // 秘書用 agentKey（別台是「台名/職位 id」）：待辦建在負責秘書那台，沒選就是主伺服器（S3-7b）。
  const { picked, one } = useAgentFilter();
  const target = useSiteTarget(one || DEFAULT_OWNER);
  const owner = target.agentId || (target.site.primary ? DEFAULT_OWNER : "");
  const [follow, setFollow] = useState<Follow | null>(null);
  const [cycle, setCycle] = useState<Cycle>("once");
  const [difficulty, setDifficulty] = useState<Difficulty>("normal");
  const [check, setCheck] = useState("");
  // 待辦的描述（RMD-23）；跟進選「到時間找我聊」時就是想聊什麼。
  const [desc, setDesc] = useState("");
  const filePicker = useRef<HTMLInputElement>(null);
  const [panel, setPanel] = useState<null | "when" | "owner">(null);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [tried, setTried] = useState(false);
  const [fieldError, setFieldError] = useState<Record<string, string>>({});
  const [todoTip, setTodoTip] = useState(false);
  const [pics, setPics] = useState<Pic[]>([]);
  const uploading = pics.some((p) => !p.url && !p.failed);
  // 有檔案沒傳成：不默默少送一個（TSK-28），擋下來請他拿掉或重貼。
  const failedPics = pics.some((p) => p.failed);
  const blockFailed = () => {
    if (!failedPics) return false;
    showToast(t("quick.image.blocked"));
    return true;
  };

  // 「@秘書」對得到每台能用的秘書（S3 收尾）：id 是 agentKey（主伺服器就是職位 id，別台是「台名/職位 id」），
  // 兩台同名時打名字先對到前面那台（主伺服器在第一個），要別台那位就從建議清單點（填入「@台名/職位 id」）。
  const agents = useMemo<AgentRef[]>(
    () =>
      target.agents.flatMap(({ site, list }) =>
        list
          .filter((a) => a.enabled && a.id)
          .map((a) => ({ id: agentKey(site.key, a.id ?? ""), name: a.name ?? "" })),
      ),
    [target.agents],
  );
  const siteOfAgent = (key: string): Site | undefined =>
    target.agents.find((a) => a.site.key === splitAgentKey(key).site)?.site;
  const intent = useMemo(
    () => parseIntent(text, agents, { ignoreWhen }),
    [text, agents, ignoreWhen],
  );

  const at = intent.kind === "todo" ? (override ?? (ignoreWhen ? null : intent.at)) : null;
  const effFollow: Follow = follow ?? (at ? "remind" : "none");
  const ownerName = target.agentName || (owner ? view(owner).name : t("tasks.new.agent.pick"));
  // 「改開任務單」交給誰（agentKey）：自己點過的 > 篩選剛好一位 > 上一次開單給的那位 > 上面「負責」那位。
  // 只挑跟膠囊同一台的秘書（S3-7b）：畫面寫「建在 A」就不會開到 B。只有一台時就是原本的順序。
  const enabledKeys = useMemo(() => enabledAgentKeys(target.agents), [target.agents]);
  // 兩台都有她時 picked 是兩個 key：挑跟膠囊同一台的那個。
  const filtered = picked.find((k) => splitAgentKey(k).site === target.site.key) ?? "";
  const onTargetSite = (key: string) => key !== "" && splitAgentKey(key).site === target.site.key;
  const taskAgent =
    [
      target.picked ? target.agent : "",
      filtered,
      lastTaskAgentIn(enabledKeys.filter(onTargetSite)),
      target.agent,
      owner ? agentKey(target.site.key, owner) : "",
    ].find(onTargetSite) ?? "";
  const taskTarget = target.ready ? resolveAgentKey(target.agents, taskAgent) : undefined;

  const addFiles = (files: File[]) => {
    // 截圖（沒名字或 image.png）改名成 paste-<時間>：傳上去的檔名、單上的附件名才分得開。
    const added = files.map((raw, i): [Pic, File] => {
      const f = namedFile(raw);
      return [{ id: `${Date.now()}-${i}`, name: f.name, file: f }, f];
    });
    setPics((cur) => [...cur, ...added.map(([p]) => p)]);
    for (const [pic, file] of added) {
      upload(file)
        .then((res) =>
          setPics((cur) => cur.map((p) => (p.id === pic.id ? { ...p, url: res.url } : p))),
        )
        .catch((err) => {
          setPics((cur) => cur.map((p) => (p.id === pic.id ? { ...p, failed: true } : p)));
          showToast(t("quick.image.failed", { reason: errorText(err) }));
        });
    }
  };
  const onPaste = (e: ClipboardEvent<HTMLElement>) => {
    const files = Array.from(e.clipboardData.files).filter((f) => f.type.startsWith("image/"));
    if (files.length === 0) return;
    e.preventDefault();
    addFiles(files);
  };
  const picLines = pics
    .filter((p) => p.url)
    .map((p) => attachmentLine(p.name, p.url ?? "", isImageName(p.name)))
    .join("\n");
  // 送到別台：圖片在主伺服器的 /share/，那台的秘書讀不到，送之前在那台再傳一次（主伺服器照用已經傳好的）。
  const picUrlsFor = async (site: Site): Promise<string[]> => {
    const done = pics.filter((p) => p.url);
    if (site.primary) return done.map((p) => p.url ?? "");
    return (await Promise.all(done.map((p) => upload(p.file, site.base)))).map((r) => r.url);
  };
  const picLinesFor = async (site: Site): Promise<string> => {
    if (site.primary) return picLines;
    const done = pics.filter((p) => p.url);
    const urls = await picUrlsFor(site);
    return done
      .map((p, i) => attachmentLine(p.name, urls[i] ?? "", isImageName(p.name)))
      .join("\n");
  };

  const reset = () => {
    setPics([]);
    clearText();
    setIgnoreWhen(false);
    setOverride(null);
    setFollow(null);
    setCycle("once");
    setDifficulty("normal");
    setCheck("");
    setDesc("");
    setPanel(null);
    setTried(false);
    setFieldError({});
  };

  const checkMissing = intent.kind === "todo" && effFollow === "check" && check.trim() === "";
  const invalid =
    intent.kind === "empty" ||
    (intent.kind === "say" && (intent.agent === null || (intent.body === "" && !picLines))) ||
    (intent.kind === "todo" && intent.content === "");
  const blocked = invalid || (variant === "reminders" && checkMissing);

  const submit = async () => {
    if (busy || uploading || blockFailed()) return;
    setTried(true);
    if (intent.kind === "todo" && intent.task) {
      // 「交代 …」：開任務單（RMD-22），不看待辦的跟進欄位。
      if (intent.content !== "") await submitTask();
      return;
    }
    if (intent.kind === "empty" || blocked) return;
    setBusy(true);
    setFieldError({});
    try {
      if (intent.kind === "say" && intent.agent) {
        // 送到那位所在的台；那台剛好不能用就擋下，不改送主伺服器。
        const to = resolveAgentKey(target.agents, intent.agent.id);
        if (!to || !siteUsable(to.site)) {
          const site = siteOfAgent(intent.agent.id);
          showToast(t("site.create.blocked", { name: site ? siteName(site) : intent.agent.name }));
          return;
        }
        const lines = await picLinesFor(to.site);
        await sayToAgent(
          client,
          { id: to.id, name: to.name },
          lines ? `${intent.body}\n${lines}` : intent.body,
          (id) =>
            void navigate({
              to: "/chat/$session",
              params: { session: id },
              search: homeSearch(to.site),
            }),
          to.site,
        );
      } else if (intent.kind === "todo") {
        // 負責秘書那台用不了、或別台還沒選負責秘書：不送（不改建到主伺服器、不讓那台自己套預設秘書）。
        if (!target.ready || owner === "") {
          setPanel("owner");
          showToast(
            t(target.ready ? "site.create.needAgent" : "site.create.blocked", {
              name: siteName(target.site),
            }),
          );
          return;
        }
        const attachments = await picUrlsFor(target.site);
        await createReminder(
          client,
          {
            content: intent.content,
            owner,
            follow: variant === "reminders" ? effFollow : undefined,
            cycle: variant === "reminders" ? cycle : undefined,
            difficulty:
              variant === "reminders" && (effFollow === "confirm" || effFollow === "check")
                ? difficulty
                : undefined,
            check: effFollow === "check" ? check.trim() : undefined,
            body: desc.trim() || undefined,
            attachments,
            at,
          },
          target.site,
        );
        onTodoCreated?.(at);
      }
      reset();
    } catch (err) {
      const field = err instanceof ApiError ? err.field : undefined;
      if (field && ["content", "at", "check"].includes(field)) {
        setFieldError({ [field]: errorText(err) });
        if (field === "at") setPanel("when");
        if (field === "check") setMore(true);
      } else {
        showToast(t("quick.failed", { reason: errorText(err) }));
      }
    } finally {
      setBusy(false);
    }
  };

  // 同一句話改開成任務單（待辦和任務單都常用）：標題用整句，負責秘書見 taskAgent。
  const submitTask = async () => {
    if (busy || uploading || intent.kind !== "todo" || text.trim() === "") return;
    if (blockFailed()) return;
    if (!taskTarget) {
      setPanel("owner");
      showToast(
        target.ready
          ? t("quick.task.pickAgent")
          : t("site.create.blocked", { name: siteName(target.site) }),
      );
      return;
    }
    setBusy(true);
    try {
      const site = taskTarget.site;
      // 貼的圖跟著進單（TSK-28）：開在別台就在那台再傳一次。
      const attachments = await picUrlsFor(site);
      await createTask(
        client,
        {
          title: intent.task ? intent.content : text,
          agent: taskTarget.id,
          type: "task",
          // 待辦頁「更多選項」寫的描述跟著進單的說明，不默默丟掉。
          body: desc.trim() || undefined,
          attachments,
        },
        taskTarget.name,
        (id) => void navigate({ to: "/tasks/$id", params: { id }, search: homeSearch(site) }),
        site,
      );
      writeLastTaskAgent(taskAgent);
      reset();
    } catch (err) {
      showToast(t("tasks.new.failed", { reason: errorText(err) }));
    } finally {
      setBusy(false);
    }
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== "Enter") return;
    // 輸入法選字中的 Enter 是確認選字，不是送出。
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    e.preventDefault();
    void submit();
  };

  const placeholder = variant === "today" ? t("quick.placeholder") : t("quick.placeholder.todo");
  const SendIcon = busy ? Spinner : ArrowUp;
  const LeadIcon = variant === "today" ? Sparkles : Plus;

  const whenField = (
    <AppWhenField
      value={at}
      error={fieldError.at}
      autoFocus
      onChange={(d) => {
        setOverride(d);
        if (d === null) setIgnoreWhen(true);
      }}
      onSubmit={() => setPanel(null)}
    />
  );

  const showPreview = intent.kind !== "empty";
  const todoContent = intent.kind === "todo" ? intent.content : "";

  return (
    <div className={cn("flex flex-col", className)} data-unsaved={pics.length > 0 ? "" : undefined}>
      <div className="flex flex-col rounded-[18px] border border-border bg-surface shadow-[var(--shadow-card)]">
        <div className="flex items-center gap-1.5 pr-1.5 pl-3.5">
          <LeadIcon
            className="size-[18px] shrink-0 text-accent"
            strokeWidth={variant === "today" ? 1.8 : 2}
            aria-hidden="true"
          />
          <label htmlFor={inputId} className="sr-only">
            {variant === "today" ? t("quick.label") : t("quick.label.todo")}
          </label>
          <input
            id={inputId}
            ref={inputRef}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              if (e.target.value.trim() === "") {
                setIgnoreWhen(false);
                setOverride(null);
              }
            }}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
            placeholder={placeholder}
            enterKeyHint="send"
            autoComplete="off"
            autoCapitalize="off"
            spellCheck={false}
            className="h-[50px] min-w-0 flex-1 border-0 bg-transparent text-base text-fg outline-none placeholder:text-muted"
          />
          <button
            type="button"
            aria-label={t("quick.send")}
            disabled={intent.kind === "empty" || busy || uploading}
            onClick={() => void submit()}
            className={cn(
              "flex size-11 shrink-0 items-center justify-center rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus",
              intent.kind === "empty" ? "bg-surface-2 text-muted" : "bg-accent text-accent-fg",
            )}
          >
            <SendIcon className="size-5" aria-hidden="true" />
          </button>
        </div>

        {pics.length > 0 ? (
          <ul
            aria-label={t("quick.image.list")}
            className="m-0 flex list-none flex-wrap gap-2 border-t border-border px-3.5 py-2.5"
          >
            {pics.map((p) => (
              <li
                key={p.id}
                className={cn(
                  chipBase,
                  "max-w-full gap-1 bg-surface-2 text-fg",
                  p.failed && "text-danger",
                )}
              >
                {p.url && isImageName(p.name) ? (
                  <img src={p.url} alt={p.name} className="size-8 rounded-md object-cover" />
                ) : p.url ? (
                  <FileText className="size-4 text-muted" aria-hidden="true" />
                ) : p.failed ? null : (
                  <Spinner className="size-4" />
                )}
                <span className="truncate">
                  {p.failed
                    ? t("quick.image.failedChip")
                    : p.url
                      ? p.name
                      : t("quick.image.uploading")}
                </span>
                <button
                  type="button"
                  aria-label={t("quick.image.remove", { name: p.name })}
                  onClick={() => setPics((cur) => cur.filter((x) => x.id !== p.id))}
                  className={cn(
                    hitArea,
                    "flex size-6 items-center justify-center rounded-full after:-inset-x-1",
                  )}
                >
                  <X className="size-3.5" aria-hidden="true" />
                </button>
              </li>
            ))}
          </ul>
        ) : null}

        {showPreview ? (
          <div
            data-testid="quick-preview"
            className="hg-rise flex flex-wrap items-center gap-1.5 border-t border-border px-3.5 pt-2.5 pb-3"
          >
            {intent.kind === "say" ? (
              <>
                <span className="text-[13px] font-semibold text-accent">
                  {intent.agent
                    ? t("quick.intent.say", { name: intent.agent.name })
                    : t("quick.intent.say", { name: intent.query || "…" })}
                </span>
                {multi && intent.agent
                  ? (() => {
                      const site = siteOfAgent(intent.agent.id);
                      return site ? <SiteTag site={site} className="shrink-0" /> : null;
                    })()
                  : null}
                {intent.agent && intent.body ? (
                  <span className={cn(chipBase, "bg-surface-2 text-fg")}>{intent.body}</span>
                ) : null}
                <span className="ml-auto text-xs text-muted">{t("quick.enter.say")}</span>
              </>
            ) : intent.kind === "todo" && intent.task ? (
              <>
                <span className="text-[13px] font-semibold text-accent">
                  {t("quick.intent.task")}
                </span>
                <span className={cn(chipBase, "bg-surface-2 text-fg")}>
                  {todoContent || <span className="text-muted">{t("quick.content.empty")}</span>}
                </span>
                <span className={cn(chipBase, "bg-surface-2 text-muted")}>
                  {taskTarget
                    ? t("quick.task.to", { name: taskTarget.name })
                    : t("quick.task.pickAgent")}
                </span>
                <span className="ml-auto text-xs text-muted">{t("quick.enter.todo")}</span>
              </>
            ) : (
              <>
                <span className="text-[13px] font-semibold text-accent">
                  {t("quick.intent.todo")}
                </span>
                {at ? (
                  <span className={cn(chipBase, "gap-0.5 bg-accent-soft pr-1 text-accent")}>
                    <button
                      type="button"
                      aria-label={`${t("quick.when.edit")}：${fullTime(at.toISOString())}`}
                      aria-expanded={panel === "when"}
                      onClick={() => setPanel(panel === "when" ? null : "when")}
                      className={cn(hitArea, "rounded-full")}
                    >
                      {fullTime(at.toISOString())}
                    </button>
                    <button
                      type="button"
                      aria-label={t("quick.when.remove")}
                      onClick={() => {
                        setIgnoreWhen(true);
                        setOverride(null);
                        setPanel(null);
                      }}
                      className={cn(
                        hitArea,
                        "flex size-6 items-center justify-center rounded-full after:-inset-x-1",
                      )}
                    >
                      <X className="size-3.5" aria-hidden="true" />
                    </button>
                  </span>
                ) : (
                  <button
                    type="button"
                    aria-expanded={panel === "when"}
                    onClick={() => setPanel(panel === "when" ? null : "when")}
                    className={cn(
                      hitArea,
                      chipBase,
                      "border border-dashed border-border-strong text-muted",
                    )}
                  >
                    {t("quick.when.none")}
                  </button>
                )}
                <span className={cn(chipBase, "bg-surface-2 text-fg")}>
                  {todoContent || <span className="text-muted">{t("quick.content.empty")}</span>}
                </span>
                <button
                  type="button"
                  aria-label={t("quick.owner.edit")}
                  aria-expanded={panel === "owner"}
                  onClick={() => setPanel(panel === "owner" ? null : "owner")}
                  className={cn(hitArea, chipBase, "bg-surface-2 text-muted")}
                >
                  {t("quick.owner", { name: ownerName })}
                </button>
                <SitePickChip
                  sites={target.sites}
                  value={target.site.key}
                  onChange={target.setSite}
                />
                {variant === "today" ? (
                  <button
                    type="button"
                    data-testid="quick-as-task"
                    disabled={busy || todoContent === ""}
                    onClick={() => void submitTask()}
                    className={cn(
                      hitArea,
                      chipBase,
                      "gap-1 border border-border-strong bg-surface font-medium text-accent disabled:opacity-50",
                    )}
                  >
                    <ClipboardList className="size-3.5" aria-hidden="true" />
                    {taskTarget
                      ? t("quick.task.asTaskTo", { name: taskTarget.name })
                      : t("quick.task.asTask")}
                  </button>
                ) : null}
                <span className="ml-auto text-xs text-muted">{t("quick.enter.todo")}</span>
              </>
            )}
          </div>
        ) : null}

        {intent.kind === "say" && !intent.named ? (
          <ul
            aria-label={t("quick.suggest")}
            className="m-0 flex list-none flex-wrap gap-1.5 border-t border-border px-3.5 py-2.5"
          >
            {intent.suggestions.map((a) => {
              const site = multi ? siteOfAgent(a.id) : undefined;
              return (
                <li key={a.id}>
                  <button
                    type="button"
                    aria-label={
                      site ? t("chat.site.who", { name: a.name, site: siteName(site) }) : undefined
                    }
                    data-agent={a.id}
                    onClick={() => {
                      // 名字對得到她就填名字；兩台同名、名字會對到別人時填「@台名/職位 id」。
                      const token = matchAgent(a.name, agents)?.id === a.id ? a.name : a.id;
                      const next = `@${token} `;
                      setText(next);
                      // 選完秘書，焦點回到輸入框、游標在最後面，接著打要說的話。
                      requestAnimationFrame(() => {
                        const el = inputRef.current;
                        el?.focus();
                        el?.setSelectionRange(next.length, next.length);
                      });
                    }}
                    className="inline-flex min-h-11 items-center gap-1.5 rounded-full border border-border bg-bg px-3.5 text-[14px] text-fg focus-visible:outline-2 focus-visible:outline-focus"
                  >
                    {a.name}
                    {site ? <SiteTag site={site} /> : null}
                  </button>
                </li>
              );
            })}
          </ul>
        ) : null}
        {intent.kind === "say" && intent.named && intent.agent === null ? (
          <p role="alert" className="border-t border-border px-3.5 py-2.5 text-[13px] text-danger">
            {t("quick.agentNotFound", { name: intent.query })}
          </p>
        ) : null}
        {intent.kind === "say" &&
        intent.agent &&
        intent.named &&
        intent.body === "" &&
        !picLines &&
        tried ? (
          <p role="alert" className="border-t border-border px-3.5 py-2.5 text-[13px] text-danger">
            {t("quick.sayEmpty")}
          </p>
        ) : null}
        {fieldError.content ? (
          <p role="alert" className="border-t border-border px-3.5 py-2.5 text-[13px] text-danger">
            {fieldError.content}
          </p>
        ) : null}

        {panel === "when" && desktop && intent.kind === "todo" ? (
          <div className="hg-rise flex flex-col gap-3 border-t border-border px-3.5 py-3">
            {whenField}
          </div>
        ) : null}
        {panel === "owner" && intent.kind === "todo" ? (
          <div className="hg-rise flex flex-col gap-2 border-t border-border px-3.5 py-3">
            <SiteAgentPicker
              label={t("quick.owner.edit")}
              value={target.agent || owner}
              agents={target.agents}
              onChange={(key) => {
                target.setAgent(key);
                setPanel(null);
              }}
            />
          </div>
        ) : null}

        {variant === "reminders" ? (
          <div className="border-t border-border">
            <button
              type="button"
              aria-expanded={more}
              onClick={() => setMore((v) => !v)}
              className="flex min-h-11 w-full items-center gap-1 px-3.5 text-[13px] font-medium text-muted focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus"
            >
              {more ? t("quick.more.hide") : t("quick.more")}
              <ChevronDown
                className={cn("size-4 transition-transform", more && "rotate-180")}
                aria-hidden="true"
              />
            </button>
            {more ? (
              <div className="hg-rise flex flex-col gap-4 px-3.5 pt-1 pb-4">
                {/* 負責秘書也放在這裡（DEV-148）：還沒打字、預覽列的「負責：…」還沒出現時也選得到。 */}
                <div className="flex flex-col gap-1.5">
                  <span className="text-[13px] font-semibold" aria-hidden="true">
                    {t("reminder.field.owner")}
                  </span>
                  <SiteAgentPicker
                    label={t("reminder.field.owner")}
                    value={target.agent || owner}
                    agents={target.agents}
                    onChange={target.setAgent}
                  />
                </div>
                <div className="flex flex-col gap-1.5">
                  <span className="text-[13px] font-semibold" id={`${inputId}-follow`}>
                    {t("quick.follow")}
                  </span>
                  <Segmented
                    wrap
                    ariaLabel={t("quick.follow")}
                    value={effFollow}
                    onChange={setFollow}
                    options={followKeys.map<SegmentedOption<Follow>>((k) => ({
                      value: k,
                      label: t(`follow.opt.${k}`),
                    }))}
                  />
                </div>
                {/* 難度只對會集點的跟進方式有意義（RMD-26，FEN-16）。 */}
                {effFollow === "confirm" || effFollow === "check" ? (
                  <div className="flex flex-col gap-1.5">
                    <span className="text-[13px] font-semibold">
                      {t("reminder.field.difficulty")}
                    </span>
                    <Segmented
                      ariaLabel={t("reminder.field.difficulty")}
                      value={difficulty}
                      onChange={setDifficulty}
                      options={(["easy", "normal", "hard"] as const).map<
                        SegmentedOption<Difficulty>
                      >((k) => ({
                        value: k,
                        label: t(`difficulty.${k}`),
                      }))}
                    />
                  </div>
                ) : null}
                <div className="flex flex-col gap-1.5">
                  <span className="text-[13px] font-semibold">{t("quick.cycle")}</span>
                  <Segmented
                    ariaLabel={t("quick.cycle")}
                    value={cycle}
                    onChange={setCycle}
                    options={cycleKeys.map<SegmentedOption<Cycle>>((k) => ({
                      value: k,
                      label: t(`cycle.${k}`),
                    }))}
                  />
                  {cycle !== "once" && at === null ? (
                    <p role="status" className="text-xs text-attention-fg">
                      {t("quick.cycleNeedsTime")}
                    </p>
                  ) : null}
                </div>
                {effFollow === "check" ? (
                  <Field
                    label={t("quick.check.label")}
                    hint={t("quick.check.hint")}
                    error={
                      fieldError.check || (tried && checkMissing ? t("quick.check.required") : "")
                    }
                  >
                    {(control) => (
                      <input
                        {...control}
                        value={check}
                        onChange={(e) => setCheck(e.target.value)}
                        placeholder={t("quick.check.example")}
                        autoComplete="off"
                        className="h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus"
                      />
                    )}
                  </Field>
                ) : null}
                <Field
                  label={effFollow === "discuss" ? t("quick.discuss.label") : t("quick.desc.label")}
                  hint={effFollow === "discuss" ? t("quick.discuss.hint") : t("quick.desc.hint")}
                >
                  {(control) => (
                    <textarea
                      {...control}
                      value={desc}
                      onChange={(e) => setDesc(e.target.value)}
                      onPaste={onPaste}
                      rows={2}
                      placeholder={
                        effFollow === "discuss"
                          ? t("quick.discuss.example")
                          : t("quick.desc.example")
                      }
                      className="min-h-11 w-full resize-y rounded-xl border border-border-strong bg-bg px-3 py-2.5 text-base text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus"
                    />
                  )}
                </Field>
                <div className="flex flex-col items-start gap-1.5">
                  <span className="text-[13px] font-semibold">{t("quick.attach")}</span>
                  <input
                    ref={filePicker}
                    type="file"
                    multiple
                    hidden
                    data-testid="quick-file"
                    onChange={(e) => {
                      const picked = Array.from(e.target.files ?? []);
                      e.target.value = "";
                      addFiles(picked);
                    }}
                  />
                  <button
                    type="button"
                    onClick={() => filePicker.current?.click()}
                    className={cn(
                      hitArea,
                      chipBase,
                      "gap-1 border border-border-strong bg-surface font-medium text-accent",
                    )}
                  >
                    <Paperclip className="size-3.5" aria-hidden="true" />
                    {t("quick.attach.pick")}
                  </button>
                  <p className="text-xs text-muted">
                    {t(desktop ? "quick.attach.hint" : "quick.attach.hint.touch")}
                  </p>
                </div>
              </div>
            ) : null}
          </div>
        ) : null}
      </div>

      {variant === "today" && intent.kind === "empty" ? (
        <Starters
          tip={todoTip}
          onTodo={() => {
            setTodoTip(true);
            inputRef.current?.focus();
          }}
          onTask={() => void navigate({ to: "/tasks", search: { new: true } })}
          onAsk={() => {
            setText("@");
            requestAnimationFrame(() => {
              const el = inputRef.current;
              el?.focus();
              el?.setSelectionRange(1, 1);
            });
          }}
        />
      ) : null}

      {!desktop ? (
        <Sheet
          open={panel === "when" && intent.kind === "todo"}
          onOpenChange={(open) => !open && setPanel(null)}
          title={t("when.label")}
        >
          <div className="flex flex-col gap-3 px-5 pb-3">
            {whenField}
            <ActionButton size="card" tone="primary" onClick={() => setPanel(null)}>
              {t("when.apply")}
            </ActionButton>
          </div>
        </Sheet>
      ) : null}
    </div>
  );
}

// 輸入框底下的三個入口（REQ-234）：看得出這個框能做什麼。記待辦＝直接打（給一句例子）；
// 交代任務＝到任務單頁開單（輸入框已經聚焦）；問秘書＝先填「@」，跳出秘書清單。
function Starters({
  tip,
  onTodo,
  onTask,
  onAsk,
}: {
  tip: boolean;
  onTodo: () => void;
  onTask: () => void;
  onAsk: () => void;
}) {
  const items = [
    { key: "todo", icon: AlarmClock, label: t("quick.start.todo"), onClick: onTodo },
    { key: "task", icon: ClipboardList, label: t("quick.start.task"), onClick: onTask },
    { key: "ask", icon: MessageCircle, label: t("quick.start.ask"), onClick: onAsk },
  ];
  return (
    <div className="flex flex-col gap-1.5 pt-2.5">
      <fieldset
        data-testid="quick-starters"
        className="m-0 flex min-w-0 flex-wrap gap-2 border-0 p-0"
      >
        <legend className="sr-only">{t("quick.start.label")}</legend>
        {items.map(({ key, icon: Icon, label, onClick }) => (
          <button
            key={key}
            type="button"
            onClick={onClick}
            className="inline-flex min-h-11 items-center gap-1.5 rounded-full border border-border bg-surface px-3.5 text-[13px] font-medium text-fg hover:bg-hover focus-visible:outline-2 focus-visible:outline-focus"
          >
            <Icon className="size-4 text-accent" aria-hidden="true" />
            {label}
          </button>
        ))}
      </fieldset>
      {tip ? (
        <p role="status" className="px-1 text-xs text-muted">
          {t("quick.start.todo.tip")}
        </p>
      ) : null}
    </div>
  );
}
