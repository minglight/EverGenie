import { ApiError, api } from "@evergenie/sdk-web";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { t } from "../../i18n/zh-TW";
import { uniqueKeys } from "../../lib/keys";
import { reasonOf } from "../../lib/reason";
import { addTodo, NIGHT_ID, NIGHT_KIND, type NightEntry, removeTodo, viewOf } from "./nightText";
import { nightQueueQuery } from "./queries";
import { SettingGroup } from "./SettingRow";

const inputClass =
  "min-h-24 min-w-0 flex-1 rounded-[10px] border border-border bg-bg px-3 py-2 text-base text-fg focus-visible:outline-2 focus-visible:outline-focus";
const buttonClass =
  "min-h-11 rounded-xl px-4 text-[15px] font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus";

type Shown =
  | { body?: string; fields?: Record<string, unknown>; revision?: string }
  | null
  | undefined;
type Change = (body: string | undefined) => string | null;

const keysOf = (entries: NightEntry[]) =>
  uniqueKeys(entries.map((e) => `${e.text}|${e.meta.join("|")}`));

function Entry({ entry, children }: { entry: NightEntry; children?: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3 px-4 py-3">
      <div className="min-w-0">
        <p className="text-[15px] leading-snug break-words">{entry.text}</p>
        {entry.meta.length > 0 ? (
          <p className="mt-0.5 text-[13px] leading-snug text-muted break-words">
            {entry.meta.join(" · ")}
          </p>
        ) : null}
        {entry.notes.map((n) => (
          <p key={n} className="mt-0.5 text-[13px] leading-snug text-muted break-words">
            {n}
          </p>
        ))}
      </div>
      {children}
    </div>
  );
}

function Block({
  title,
  entries,
  testId,
}: {
  title: string;
  entries: NightEntry[];
  testId: string;
}) {
  return (
    <div data-testid={testId} className="flex flex-col">
      <p className="px-4 pt-3 text-[13px] font-semibold text-muted">
        {title}（{entries.length}）
      </p>
      {entries.length === 0 ? (
        <p className="px-4 py-2.5 text-[13px] text-muted">{t("settings.machine.night.none")}</p>
      ) : (
        entries.map((e, i) => <Entry key={keysOf(entries)[i]} entry={e} />)
      )}
    </div>
  );
}

// 設定 → 電腦 → 夜班清單（P6-6）：每週額度重置前最後兩晚，system 照這份做；沒放就不做。
// 看板可以新增和刪除「待做」。寫入一律以「畫面上那一版」為底、帶它的 revision（vault 樂觀鎖）；
// 別處剛改過（revision 不符）就不寫，重新載入並請使用者再按一次（P6 收尾 F5：避免刪錯一項）。
export function NightQueue() {
  const q = useQuery(nightQueueQuery);
  const client = useQueryClient();
  const [text, setText] = useState("");
  const [open, setOpen] = useState(false);
  const [what, setWhat] = useState("");
  const [read, setRead] = useState("");
  const [out, setOut] = useState("");
  const [limit, setLimit] = useState("");
  const [confirm, setConfirm] = useState<number | null>(null);
  const [error, setError] = useState("");

  const edit = useMutation({
    mutationFn: async ({ shown, change }: { shown: Shown; change: Change }) => {
      const body = change(shown?.body);
      if (body === null) return;
      await api.vault.put({
        record: {
          kind: NIGHT_KIND,
          id: NIGHT_ID,
          fields: shown?.fields ?? {},
          body,
          archived: false,
        },
        expected_revision: shown?.revision ?? "",
      });
    },
    onMutate: () => setError(""),
    onError: (err) => {
      if (err instanceof ApiError && err.code === "conflict") {
        setConfirm(null);
        setError(t("settings.machine.night.conflict"));
        return;
      }
      setError(t("settings.machine.night.failed", { reason: reasonOf(err) }));
    },
    onSettled: () => client.invalidateQueries({ queryKey: nightQueueQuery.queryKey }),
  });

  const view = viewOf(q.data?.body);
  const add = () => {
    const title = text;
    if (!title.trim()) return;
    const details = [
      what && `做什麼：${what}`,
      read && `要讀：${read}`,
      out && `產出：${out}`,
      limit && `範圍：${limit}`,
    ].filter((d): d is string => Boolean(d));
    edit.mutate(
      { shown: q.data, change: (body) => addTodo(body, title, new Date(), details) },
      {
        onSuccess: () => {
          setText("");
          setWhat("");
          setRead("");
          setOut("");
          setLimit("");
          setOpen(false);
        },
      },
    );
  };

  return (
    <SettingGroup title={t("settings.machine.night")}>
      <p className="px-4 py-3 text-[13px] leading-snug text-muted">
        {t("settings.machine.night.hint")}
      </p>
      <p className="px-4 pb-3 text-[13px] leading-snug text-muted">
        {t("settings.machine.night.howto")}
      </p>
      {q.data === undefined && q.isError ? (
        <LoadFailed what={t("settings.loadFailed")} onRetry={() => void q.refetch()} />
      ) : q.isPending ? (
        <LoadingBlock />
      ) : (
        <>
          <div data-testid="night-todo" className="flex flex-col">
            <p className="px-4 pt-2 text-[13px] font-semibold text-muted">
              {t("settings.machine.night.todo")}（{view.todo.length}）
            </p>
            {view.todo.length === 0 ? (
              <p className="px-4 py-2.5 text-[13px] text-muted">
                {t("settings.machine.night.todo.empty")}
              </p>
            ) : (
              view.todo.map((entry, i) => (
                <Entry key={keysOf(view.todo)[i]} entry={entry}>
                  {confirm === i ? (
                    <div className="flex shrink-0 gap-1.5">
                      <button
                        type="button"
                        onClick={() => {
                          setConfirm(null);
                          edit.mutate({ shown: q.data, change: (body) => removeTodo(body, i + 1) });
                        }}
                        className={`${buttonClass} bg-danger text-white`}
                      >
                        {t("settings.machine.night.rm.yes")}
                      </button>
                      <button
                        type="button"
                        onClick={() => setConfirm(null)}
                        className={`${buttonClass} border border-border-strong bg-surface font-medium`}
                      >
                        {t("settings.machine.night.rm.no")}
                      </button>
                    </div>
                  ) : (
                    <button
                      type="button"
                      aria-label={t("settings.machine.night.rm", { text: entry.text })}
                      onClick={() => setConfirm(i)}
                      className="min-h-11 shrink-0 rounded-lg px-2 text-[13px] font-semibold text-danger focus-visible:outline-2 focus-visible:outline-focus"
                    >
                      {t("settings.machine.night.rm.short")}
                    </button>
                  )}
                </Entry>
              ))
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2 px-4 py-3">
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              className={`${buttonClass} bg-accent text-accent-fg`}
            >
              {t("settings.machine.night.new")}
            </button>
            <Link
              to="/chat"
              className={`${buttonClass} inline-flex items-center border border-border-strong bg-surface font-medium`}
            >
              {t("settings.machine.night.discuss")}
            </Link>
          </div>
          {open ? (
            <form
              data-testid="night-form"
              className="flex flex-col gap-3 px-4 pb-3"
              onSubmit={(e) => {
                e.preventDefault();
                add();
              }}
            >
              <p className="text-[13px] leading-snug text-muted">
                {t("settings.machine.night.discuss.hint")}
              </p>
              {(
                [
                  ["title", text, setText, false],
                  ["what", what, setWhat, true],
                  ["read", read, setRead, false],
                  ["out", out, setOut, false],
                  ["limit", limit, setLimit, true],
                ] as const
              ).map(([k, v, set, multi]) => {
                const label = t(`settings.machine.night.form.${k}` as "settings.machine.night");
                const ph = t(`settings.machine.night.form.${k}.ph` as "settings.machine.night");
                return (
                  <label
                    key={k}
                    htmlFor={`night-field-${k}`}
                    className="flex flex-col gap-1 text-[13px] font-semibold text-muted"
                  >
                    {label}
                    {multi ? (
                      <textarea
                        id={`night-field-${k}`}
                        data-testid={`night-${k}`}
                        rows={3}
                        placeholder={ph}
                        value={v}
                        onChange={(e) => set(e.target.value)}
                        className={`${inputClass} font-normal`}
                      />
                    ) : (
                      <input
                        id={`night-field-${k}`}
                        data-testid={k === "title" ? "night-add" : `night-${k}`}
                        type="text"
                        autoComplete="off"
                        placeholder={ph}
                        value={v}
                        onChange={(e) => set(e.target.value)}
                        className={`${inputClass} min-h-11 font-normal`}
                      />
                    )}
                  </label>
                );
              })}
              <div className="flex gap-2">
                <button
                  type="submit"
                  disabled={edit.isPending || text.trim() === ""}
                  className={`${buttonClass} bg-accent text-accent-fg disabled:opacity-50`}
                >
                  {t("settings.machine.night.add")}
                </button>
                <button
                  type="button"
                  onClick={() => setOpen(false)}
                  className={`${buttonClass} border border-border-strong bg-surface font-medium`}
                >
                  {t("settings.machine.night.form.cancel")}
                </button>
              </div>
            </form>
          ) : null}
          {error ? (
            <p role="alert" className="px-4 pb-3 text-xs leading-snug text-danger">
              {error}
            </p>
          ) : null}
          <Block
            title={t("settings.machine.night.blocked")}
            entries={view.blocked}
            testId="night-blocked"
          />
          <Block title={t("settings.machine.night.done")} entries={view.done} testId="night-done" />
        </>
      )}
    </SettingGroup>
  );
}
