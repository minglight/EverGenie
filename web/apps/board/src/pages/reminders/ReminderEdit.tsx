import type { Cycle, Difficulty, Follow, Reminder } from "@evergenie/sdk-web";
import {
  type AgentOption,
  AgentPicker,
  cn,
  Field,
  Segmented,
  type SegmentedOption,
} from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { AppWhenField } from "../../components/AppWhenField";
import { EditButton, EditDialog, EditGroup, editInputClass } from "../../components/EditForm";
import { refreshAll } from "../../data/live";
import { apiFor, PRIMARY, type SiteRef } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import {
  type ReminderDraft,
  reminderDraftErrors,
  reminderDraftOf,
  reminderPatch,
} from "../../lib/editPatch";

const followKeys = ["none", "remind", "confirm", "check", "discuss"] as const satisfies Follow[];
const difficultyKeys = ["easy", "normal", "hard"] as const satisfies Difficulty[];
const cycleKeys = ["once", "daily", "weekly", "biweekly", "monthly"] as const satisfies Cycle[];

/**
 * 待辦詳情的「編輯」（WEB-161）：一顆按鈕打開整張表單（內容、時間、負責秘書、跟進方式、週期、難度、
 * 要確認什麼〔跟進是「確認」才有〕、描述），預填現值，儲存一次 reminders.update、只送改過的欄位。
 */
export function ReminderEdit({
  reminder,
  agents,
  site = PRIMARY,
}: {
  reminder: Reminder;
  agents: AgentOption[];
  site?: SiteRef;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <EditButton onClick={() => setOpen(true)} />
      {/* 只在打開時掛上：每次打開都從當下的值重新抄一份草稿。 */}
      {open ? (
        <ReminderEditDialog
          reminder={reminder}
          agents={agents}
          site={site}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  );
}

function ReminderEditDialog({
  reminder,
  agents,
  site,
  onClose,
}: {
  reminder: Reminder;
  agents: AgentOption[];
  site: SiteRef;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const [before] = useState(() => reminderDraftOf(reminder));
  const [draft, setDraft] = useState<ReminderDraft>(before);
  const [tried, setTried] = useState(false);
  const set = <K extends keyof ReminderDraft>(key: K, value: ReminderDraft[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));
  const errors = reminderDraftErrors(draft);
  const shown = (key: keyof ReminderDraft) => {
    const e = errors[key];
    return tried && e ? t(e) : undefined;
  };

  return (
    <EditDialog
      open
      title={t("reminder.editForm.title")}
      onClose={onClose}
      invalid={Object.keys(errors).length > 0}
      onTried={() => setTried(true)}
      patch={() => reminderPatch(before, draft)}
      onSave={async (patch) => {
        // 存進去了才算數：之後刷新失敗不當成「沒存成功」（免得再按一次重送）。
        await apiFor(site).reminders.update({ id: reminder.id ?? "", ...patch });
        await refreshAll(client, site).catch(() => undefined);
      }}
    >
      <Field label={t("reminder.field.content")} error={shown("content")}>
        {(control) => (
          <input
            {...control}
            value={draft.content}
            onChange={(e) => set("content", e.target.value)}
            autoComplete="off"
            enterKeyHint="next"
            className={editInputClass}
          />
        )}
      </Field>
      {/* 時間欄位自己有「時間」標籤；底下只補表單才有的說明。 */}
      <div className="flex flex-col gap-1.5">
        <AppWhenField value={draft.at} onChange={(d) => set("at", d)} />
        {before.at && draft.at === null ? (
          <p className="text-xs text-muted">{t("reminder.editForm.atKeep")}</p>
        ) : draft.cycle !== "once" && draft.at === null ? (
          <p role="status" className="text-xs text-attention-fg">
            {t("quick.cycleNeedsTime")}
          </p>
        ) : null}
      </div>
      <EditGroup label={t("reminder.field.owner")}>
        <AgentPicker
          label={t("reminder.field.owner")}
          value={draft.owner}
          agents={agents}
          onChange={(v) => set("owner", v)}
        />
      </EditGroup>
      <EditGroup label={t("reminder.field.follow")}>
        <Segmented
          wrap
          ariaLabel={t("reminder.field.follow")}
          value={draft.follow}
          onChange={(v) => set("follow", v)}
          options={followKeys.map<SegmentedOption<Follow>>((k) => ({
            value: k,
            label: t(`follow.opt.${k}`),
          }))}
        />
      </EditGroup>
      {draft.follow === "check" ? (
        <Field
          label={t("reminder.field.check")}
          hint={t("quick.check.hint")}
          error={shown("check")}
        >
          {(control) => (
            <input
              {...control}
              value={draft.check}
              onChange={(e) => set("check", e.target.value)}
              placeholder={t("quick.check.example")}
              autoComplete="off"
              className={editInputClass}
            />
          )}
        </Field>
      ) : null}
      <EditGroup label={t("reminder.field.cycle")}>
        <Segmented
          ariaLabel={t("reminder.field.cycle")}
          value={draft.cycle}
          onChange={(v) => set("cycle", v)}
          options={cycleKeys.map<SegmentedOption<Cycle>>((k) => ({
            value: k,
            label: t(`cycle.${k}`),
          }))}
        />
      </EditGroup>
      {/* 難度只對會集點的跟進方式有意義（RMD-26，FEN-16）。 */}
      {draft.follow === "confirm" || draft.follow === "check" ? (
        <EditGroup label={t("reminder.field.difficulty")}>
          <Segmented
            ariaLabel={t("reminder.field.difficulty")}
            value={draft.difficulty}
            onChange={(v) => set("difficulty", v)}
            options={difficultyKeys.map<SegmentedOption<Difficulty>>((k) => ({
              value: k,
              label: t(`difficulty.${k}`),
            }))}
          />
        </EditGroup>
      ) : null}
      <Field label={t("reminder.body")}>
        {(control) => (
          <textarea
            {...control}
            value={draft.body}
            onChange={(e) => set("body", e.target.value)}
            rows={4}
            placeholder={t("quick.desc.example")}
            className={cn(editInputClass, "resize-y py-2.5")}
          />
        )}
      </Field>
    </EditDialog>
  );
}
