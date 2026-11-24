import type { Task } from "@evergenie/sdk-web";
import { type AgentOption, AgentPicker, cn, Field, Segmented } from "@evergenie/ui";
import { useState } from "react";
import { t } from "../i18n/zh-TW";
import { type TaskDraft, taskDraftErrors, taskDraftOf, taskPatch } from "../lib/editPatch";
import { EditButton, EditDialog, EditGroup, editInputClass } from "./EditForm";

/**
 * 任務詳情的「編輯」（WEB-161）：一顆按鈕打開整張表單（標題、負責秘書、類型、需求、驗收條件），
 * 預填現值，儲存一次 tasks.update、只送改過的欄位。存檔怎麼送、送完怎麼刷新由呼叫端給（單在哪台由它決定）。
 */
export function TaskEdit({
  task,
  agents,
  onSave,
}: {
  task: Task;
  agents: AgentOption[];
  onSave: (patch: ReturnType<typeof taskPatch>) => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <EditButton onClick={() => setOpen(true)} />
      {/* 只在打開時掛上：每次打開都從當下的值重新抄一份草稿。 */}
      {open ? (
        <TaskEditDialog
          task={task}
          agents={agents}
          onSave={onSave}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  );
}

function TaskEditDialog({
  task,
  agents,
  onSave,
  onClose,
}: {
  task: Task;
  agents: AgentOption[];
  onSave: (patch: ReturnType<typeof taskPatch>) => Promise<void>;
  onClose: () => void;
}) {
  const [before] = useState(() => taskDraftOf(task));
  const [draft, setDraft] = useState<TaskDraft>(before);
  const [tried, setTried] = useState(false);
  const set = <K extends keyof TaskDraft>(key: K, value: TaskDraft[K]) =>
    setDraft((d) => ({ ...d, [key]: value }));
  const errors = taskDraftErrors(draft);
  const titleError = tried && errors.title ? t(errors.title) : undefined;
  // 驗收條件的說明跟詳情頁同一套（TSK-24）：主人（或 Goal）的條件秘書照它驗過就自己結；改了就算主人的。
  const criteriaHint =
    draft.criteria.trim() !== before.criteria.trim()
      ? t("task.editForm.criteria.hint")
      : before.criteria
        ? task.criteria_owner || task.goal
          ? t("task.criteria.owner")
          : t("task.criteria.agent")
        : t("tasks.new.criteria.hint");

  return (
    <EditDialog
      open
      title={t("task.editForm.title")}
      onClose={onClose}
      invalid={Object.keys(errors).length > 0}
      onTried={() => setTried(true)}
      patch={() => taskPatch(before, draft)}
      onSave={onSave}
    >
      <Field label={t("task.section.title")} error={titleError}>
        {(control) => (
          <input
            {...control}
            value={draft.title}
            onChange={(e) => set("title", e.target.value)}
            autoComplete="off"
            enterKeyHint="next"
            className={editInputClass}
          />
        )}
      </Field>
      <EditGroup label={t("task.field.agent")}>
        <AgentPicker
          label={t("task.field.agent")}
          value={draft.agent}
          agents={agents}
          onChange={(v) => set("agent", v)}
        />
      </EditGroup>
      <EditGroup label={t("tasks.new.type")} hint={t("tasks.new.type.hint")}>
        <Segmented
          ariaLabel={t("tasks.new.type")}
          value={draft.type}
          onChange={(v) => set("type", v)}
          options={[
            { value: "task", label: t("tasks.new.type.task") },
            { value: "ask", label: t("tasks.new.type.ask") },
          ]}
        />
      </EditGroup>
      <Field label={t("task.section.body")}>
        {(control) => (
          <textarea
            {...control}
            value={draft.body}
            onChange={(e) => set("body", e.target.value)}
            rows={4}
            className={cn(editInputClass, "resize-y py-2.5")}
          />
        )}
      </Field>
      {/* 問答類沒有驗收條件（跟詳情頁一樣不顯示，也不送）。 */}
      {draft.type === "task" ? (
        <Field label={t("task.section.criteria")} hint={criteriaHint}>
          {(control) => (
            <textarea
              {...control}
              value={draft.criteria}
              onChange={(e) => set("criteria", e.target.value)}
              rows={3}
              className={cn(editInputClass, "resize-y py-2.5")}
            />
          )}
        </Field>
      ) : null}
    </EditDialog>
  );
}
