import { ApiError } from "@evergenie/sdk-web";
import { cn, Field } from "@evergenie/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { FormDialog } from "../../components/FormDialog";
import { SiteAgentPicker, useSiteTarget } from "../../components/SitePick";
import { SitePickChip, siteName } from "../../components/SiteTag";
import { homeSearch } from "../../data/sites";
import { t } from "../../i18n/zh-TW";
import { useDraft } from "../../lib/draft";
import { focusFirstInvalid } from "../../lib/focusInvalid";
import { createGoal } from "./goals";

const inputClass =
  "min-h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-base text-fg outline-none placeholder:text-muted focus-visible:outline-2 focus-visible:outline-focus";

// 「新增 Goal」（D-39、REQ-203）：手機底部抽屜、桌機對話框（FEN-24）。四欄（2026-11-01）：標題、要做到什麼（完成的樣子）、
// 驗收條件（必填，法律第 4 條）、負責秘書（選填，再按一次取消）。授權範圍的勾選 2026-11-01 拿掉（傳話不掛 Goal 後沒有作用，DEV-95）。
export function NewGoalDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <FormDialog open={open} title={t("goal.new.title")} body={t("goal.new.body")} onClose={onClose}>
      {open ? <NewGoalForm onClose={onClose} /> : null}
    </FormDialog>
  );
}

function NewGoalForm({ onClose }: { onClose: () => void }) {
  const client = useQueryClient();
  const navigate = useNavigate();
  // 草稿（FEN-23）：關掉再開還在，開成功才清掉。
  const [title, setTitle, clearTitle] = useDraft("eg.draft.goals.new.title");
  const [text, setText, clearText] = useDraft("eg.draft.goals.new.text");
  const [criteria, setCriteria, clearCriteria] = useDraft("eg.draft.goals.new.criteria");
  // 負責秘書（agentKey）與建在哪台（S3-7b）：選了秘書就建在她那台；沒選是主伺服器，膠囊可以改。
  const target = useSiteTarget("");
  const [tried, setTried] = useState(false);
  const [busy, setBusy] = useState(false);
  const [server, setServer] = useState<{ field?: string; text: string } | null>(null);

  const errors = {
    title: title.trim() === "" ? t("goal.new.title.required") : "",
    text: text.trim() === "" ? t("goal.new.text.required") : "",
    criteria: criteria.trim() === "" ? t("goal.new.criteria.required") : "",
  };
  const serverFor = (field: string) => (server?.field === field ? server.text : "");

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    setTried(true);
    if (errors.title || errors.text || errors.criteria) {
      // 錯誤畫出來之後把焦點帶到第一個沒填的欄（GOL-14）。
      const form = e.currentTarget as HTMLFormElement;
      requestAnimationFrame(() => focusFirstInvalid(form));
      return;
    }
    if (!target.ready) {
      // 選的秘書那台現在用不了：不送，也不改建到主伺服器。
      setServer({ text: t("site.create.blocked", { name: siteName(target.site) }) });
      return;
    }
    setBusy(true);
    setServer(null);
    const site = target.site;
    try {
      await createGoal(
        client,
        { title, text, criteria, agent: target.agentId },
        (id) => void navigate({ to: "/goals/$id", params: { id }, search: homeSearch(site) }),
        site,
      );
      clearTitle();
      clearText();
      clearCriteria();
      onClose();
    } catch (err) {
      const field = err instanceof ApiError ? err.field : undefined;
      const reason = err instanceof ApiError ? err.message : t("settings.error.generic");
      setServer(
        field && ["title", "text", "criteria", "agent"].includes(field)
          ? { field, text: reason }
          : { text: t("goal.new.failed", { reason }) },
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <form onSubmit={(e) => void submit(e)} noValidate className="flex flex-col">
      <div className="flex max-h-[calc(var(--hg-visible-h)*0.6)] flex-col gap-4 overflow-y-auto px-5 pb-3">
        <Field
          label={t("goal.new.field.title")}
          hint={t("goal.new.field.title.hint")}
          error={(tried && errors.title) || serverFor("title")}
        >
          {(control) => (
            <input
              {...control}
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              autoComplete="off"
              enterKeyHint="next"
              className={inputClass}
            />
          )}
        </Field>
        <Field
          label={t("goal.new.field.text")}
          hint={t("goal.new.field.text.hint")}
          error={(tried && errors.text) || serverFor("text")}
        >
          {(control) => (
            <textarea
              {...control}
              value={text}
              onChange={(e) => setText(e.target.value)}
              rows={5}
              autoComplete="off"
              className={cn(inputClass, "py-2")}
            />
          )}
        </Field>
        <Field
          label={t("goal.new.field.criteria")}
          hint={t("goal.new.field.criteria.hint")}
          error={(tried && errors.criteria) || serverFor("criteria")}
        >
          {(control) => (
            <textarea
              {...control}
              value={criteria}
              onChange={(e) => setCriteria(e.target.value)}
              rows={3}
              autoComplete="off"
              className={cn(inputClass, "py-2")}
            />
          )}
        </Field>
        <div className="flex flex-col gap-1.5">
          <span className="text-[13px] font-semibold">{t("goal.new.agent")}</span>
          <p className="text-xs leading-snug text-muted">{t("goal.new.agent.hint")}</p>
          <SiteAgentPicker
            label={t("goal.new.agent")}
            value={target.agent}
            agents={target.agents}
            onChange={(key) => target.setAgent(target.agent === key ? "" : key)}
          />
          <SitePickChip
            className="self-start"
            sites={target.sites}
            value={target.site.key}
            onChange={target.setSite}
          />
          {serverFor("agent") ? (
            <p role="alert" className="text-xs text-danger">
              {serverFor("agent")}
            </p>
          ) : null}
        </div>
        {server && !server.field ? (
          <p role="alert" className="text-[13px] text-danger">
            {server.text}
          </p>
        ) : null}
      </div>
      <div className="flex flex-wrap justify-end gap-2 border-t border-border px-5 pt-3 pb-1">
        <ActionButton tone="outline" onClick={onClose} disabled={busy}>
          {t("goal.new.cancel")}
        </ActionButton>
        <ActionButton type="submit" tone="primary" busy={busy}>
          {t("goal.new.create")}
        </ActionButton>
      </div>
    </form>
  );
}
