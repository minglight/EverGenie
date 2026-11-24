import { type Api, ApiError, upload } from "@evergenie/sdk-web";
import { Composer, cn, showToast } from "@evergenie/ui";
import { type KeyboardEvent, useEffect, useState } from "react";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { t } from "../../i18n/zh-TW";
import { chatDraftKey, useDraft } from "../../lib/draft";
import { SLASH_MENU_ID, SlashMenu, slashOptionId } from "./SlashMenu";
import { commandLabel, parseSlash, type SlashItem, type SlashSpec, slashItems } from "./slash";

// 討論串底部的輸入框：草稿、附件、「/」指令選單（WEB-83、D-45）。
// 指令送那台的 session.command，不當訊息送、不進佇列；做成了 toast＋訊息流一行紀錄（onCommand）。
export function ThreadComposer({
  name,
  session,
  api,
  base,
  agentName,
  onSend,
  onCommand,
  retype,
}: {
  /** 草稿的 key（別台加台名前綴，兩台同名的串不共用草稿）。 */
  name: string;
  /** 這一串的 session 名稱（指令送給它）。 */
  session: string;
  /** 這一串那台的 API（指令送那台，WEB-81）。 */
  api: Api;
  /** 附件傳到哪台（那台的 origin；主伺服器是 ""）。 */
  base: string;
  agentName: string;
  onSend: (text: string) => void;
  /** 指令做成了：在訊息流留一行紀錄。 */
  onCommand: (label: string) => void;
  retype?: { text: string; n: number } | null;
}) {
  const [draft, setDraft, clear] = useDraft(chatDraftKey(name));
  // biome-ignore lint/correctness/useExhaustiveDependencies: 只在按了「再說一次」（n 變了）時放回輸入框
  useEffect(() => {
    if (retype) setDraft(retype.text);
  }, [retype?.n]);
  // 「/」指令選單（WEB-83）：選單的列從輸入框的字算；Esc 關掉之後，字變了才再開。
  const [closedFor, setClosedFor] = useState<string | null>(null);
  const [active, setActive] = useState(0);
  const [hint, setHint] = useState<{ text: string; error: boolean } | null>(null);
  const [confirm, setConfirm] = useState<{ spec: SlashSpec; arg: string } | null>(null);
  const [sending, setSending] = useState(false);
  const items = closedFor === draft ? null : slashItems(draft);
  const open = !sending && confirm === null && items !== null && items.length > 0;
  // biome-ignore lint/correctness/useExhaustiveDependencies: 字變了才重來
  useEffect(() => {
    setActive(0);
    setHint(null);
  }, [draft]);

  const run = async (spec: SlashSpec, arg: string) => {
    const label = commandLabel(spec.name, arg);
    setSending(true);
    try {
      await api.session.command({ session, command: spec.name, arg });
      clear();
      setHint(null);
      showToast(t("slash.sent", { cmd: label }));
      onCommand(label);
    } catch (err) {
      setHint({
        text: t("slash.failed", {
          cmd: label,
          reason: err instanceof ApiError ? err.message : String(err),
        }),
        error: true,
      });
    } finally {
      setSending(false);
    }
  };
  const execute = (spec: SlashSpec, arg: string) => {
    if (spec.confirm) setConfirm({ spec, arg });
    else void run(spec, arg);
  };
  const pick = (item: SlashItem) => {
    if (item.spec.options && item.arg === undefined) {
      setDraft(`/${item.spec.name} `); // 第二層：列出選項
      return;
    }
    execute(item.spec, item.arg ?? "");
  };
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (!open || !items) return;
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const step = e.key === "ArrowDown" ? 1 : -1;
      setActive((i) => (i + step + items.length) % items.length);
    } else if ((e.key === "Enter" && !e.shiftKey) || e.key === "Tab") {
      e.preventDefault();
      const item = items[Math.min(active, items.length - 1)];
      if (item) pick(item);
    } else if (e.key === "Escape") {
      e.preventDefault();
      setClosedFor(draft);
    }
  };

  const confirmText = confirm
    ? confirm.spec.name === "clear"
      ? {
          title: t("slash.clear.title", { name: agentName }),
          body: t("slash.clear.body"),
          confirmLabel: t("slash.clear.confirm"),
          tone: "danger" as const,
        }
      : {
          title: t("slash.effort.title", { level: effortName(confirm.arg) }),
          body: t("slash.effort.body"),
          confirmLabel: t("slash.effort.confirm"),
          tone: "primary" as const,
        }
    : null;

  return (
    <div className="relative">
      {open && items ? (
        <SlashMenu
          items={items}
          active={active}
          label={t("slash.menu")}
          onPick={pick}
          onHover={setActive}
        />
      ) : null}
      {hint ? (
        <p
          role={hint.error ? "alert" : "status"}
          data-testid="slash-hint"
          className={cn(
            "mb-2 rounded-xl px-3.5 py-2.5 text-[13px] leading-snug",
            hint.error ? "bg-danger-soft text-danger" : "bg-surface-2 text-muted",
          )}
        >
          {hint.text}
        </p>
      ) : null}
      <Composer
        value={draft}
        onChange={setDraft}
        onUpload={(file) => upload(file, base)}
        attachLabels={{
          attach: t("chat.attach"),
          remove: t("chat.attach.remove"),
          retry: t("chat.attach.retry"),
          uploading: t("chat.attach.uploading"),
          failed: t("chat.attach.failed"),
        }}
        onKeyDown={onKeyDown}
        inputProps={{
          role: "combobox",
          "aria-expanded": open,
          "aria-controls": open ? SLASH_MENU_ID : undefined,
          "aria-activedescendant": open ? slashOptionId(active) : undefined,
          "aria-autocomplete": "list",
        }}
        onSend={(message, meta) => {
          if (!message || sending) return;
          const parsed = parseSlash(meta.raw, meta.attachments);
          if (parsed.kind === "unknown") {
            setHint({ text: t("slash.unknown"), error: false });
            return;
          }
          if (parsed.kind === "needArg") {
            setDraft(`/${parsed.spec.name} `);
            setClosedFor(null);
            return;
          }
          if (parsed.kind === "command") {
            execute(parsed.spec, parsed.arg);
            return;
          }
          onSend(message);
          clear();
        }}
        label={t("chat.compose", { name: agentName })}
        placeholder={t("chat.compose.placeholder", { name: agentName })}
        sendLabel={t("chat.send")}
        busy={sending}
      />
      <ConfirmDialog
        open={confirm !== null}
        title={confirmText?.title ?? ""}
        body={confirmText?.body ?? ""}
        confirmLabel={confirmText?.confirmLabel ?? ""}
        cancelLabel={t("slash.cancel")}
        tone={confirmText?.tone}
        onCancel={() => setConfirm(null)}
        onConfirm={() => {
          const c = confirm;
          setConfirm(null);
          if (c) void run(c.spec, c.arg);
        }}
      />
    </div>
  );
}

function effortName(arg: string): string {
  return t(`slash.effort.${arg}` as never).split("：")[0] ?? arg;
}
