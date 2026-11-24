import type { Account } from "@evergenie/sdk-web";
import { api } from "@evergenie/sdk-web";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { FormDialog } from "../../components/FormDialog";
import { Pill } from "../../components/Pill";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { Toggle } from "../../components/Toggle";
import { t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { Field, fieldInputClass } from "./formBits";
import { accountsQuery } from "./queries";
import { SettingGroup } from "./SettingRow";

const blank: Account = {
  name: "",
  engine: "",
  config_dir: "",
  env_file: "",
  remote_access: false,
  usage_warmup: false,
  default: false,
};

// engineChoices：伺服器給的引擎清單（不寫死哪一家）；編輯舊帳號時它原本的引擎不在清單裡也照樣列出來。
function engineChoices(engines: string[], current: string | undefined): string[] {
  return current && !engines.includes(current) ? [current, ...engines] : engines;
}

// 設定 → 電腦 → AI 帳號（DEV-33）：列出 home.toml 的帳號，可以新增、編輯、設成預設。
// 只有一個帳號時（大部分的家，DEV-153、WEB-193）：引擎、登入資料夾、環境檔、用量暖機是裝好就對的東西，
// 清單上不列、編輯時收在「進階」裡，按了才展開；有兩個以上（或正在新增）才全部攤開。
export function AccountsSettings() {
  const q = useQuery(accountsQuery);
  const engines = q.data?.engines ?? [];
  const client = useQueryClient();
  const [form, setForm] = useState<{ account: Account; isNew: boolean } | null>(null);
  const [makeDefault, setMakeDefault] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);
  const [advanced, setAdvanced] = useState(false);
  const single = (q.data?.accounts.length ?? 0) <= 1;

  const save = useMutation({
    mutationFn: async () => {
      if (!form) return;
      await api.system.saveAccount({ account: form.account, make_default: makeDefault });
    },
    onSuccess: () => {
      setForm(null);
      setSaved(true);
      void client.invalidateQueries({ queryKey: accountsQuery.queryKey });
      void client.invalidateQueries({ queryKey: ["system.usage"] });
    },
    onError: (err) => setError(t("settings.machine.save.failed", { reason: reasonOf(err) })),
  });

  const open = (account: Account, isNew: boolean) => {
    setError("");
    setSaved(false);
    setMakeDefault(false);
    setAdvanced(false);
    setForm({ account, isNew });
  };
  const patch = (p: Partial<Account>) =>
    setForm((f) => (f ? { ...f, account: { ...f.account, ...p } } : f));

  return (
    <SettingGroup title={t("settings.machine.accounts")}>
      {q.data === undefined ? (
        q.isError ? (
          <LoadFailed what={t("settings.loadFailed")} onRetry={() => void q.refetch()} />
        ) : (
          <LoadingBlock />
        )
      ) : q.data.accounts.length === 0 ? (
        <p className="px-4 py-3.5 text-[13px] text-muted">{t("settings.machine.accounts.empty")}</p>
      ) : (
        q.data.accounts.map((a) => (
          <div
            key={a.name}
            data-testid="account-row"
            className="flex items-center gap-3 px-4 py-3.5"
          >
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <p className="text-[15px] font-semibold break-all">{a.name}</p>
                {a.default ? (
                  <Pill tone="accent">{t("settings.machine.accounts.default")}</Pill>
                ) : null}
                {single ? null : <Pill tone="muted">{a.engine}</Pill>}
                {a.remote_access ? (
                  <Pill tone="muted">{t("settings.machine.accounts.remote")}</Pill>
                ) : null}
              </div>
              {single ? null : (
                <p className="mt-0.5 text-[13px] leading-snug text-muted break-all">
                  {a.config_dir}
                </p>
              )}
            </div>
            <ActionButton tone="outline" size="pane" onClick={() => open(a, false)}>
              {t("settings.machine.accounts.edit")}
            </ActionButton>
          </div>
        ))
      )}
      <div className="flex flex-col gap-2 px-4 py-3">
        {saved ? (
          <p role="status" className="text-[13px] text-muted">
            {t("settings.machine.accounts.saved")}
          </p>
        ) : null}
        <p className="text-[13px] leading-snug text-muted">{t("settings.machine.accounts.hint")}</p>
        <div>
          <ActionButton
            tone="secondary"
            size="pane"
            onClick={() => open({ ...blank, engine: engines[0] ?? "" }, true)}
          >
            {t("settings.machine.accounts.add")}
          </ActionButton>
        </div>
      </div>
      <FormDialog
        open={form !== null}
        title={t(form?.isNew ? "settings.machine.accounts.add" : "settings.machine.accounts.edit")}
        onClose={() => setForm(null)}
      >
        {form ? (
          <form
            className="flex flex-col gap-3.5 px-5 pb-5"
            onSubmit={(e) => {
              e.preventDefault();
              setError("");
              save.mutate();
            }}
          >
            <Field
              label={t("settings.machine.accounts.name")}
              hint={t("settings.machine.accounts.name.hint")}
            >
              <input
                className={fieldInputClass}
                value={form.account.name ?? ""}
                disabled={!form.isNew}
                onChange={(e) => patch({ name: e.target.value })}
                autoCapitalize="none"
                required
              />
            </Field>
            <div className="flex items-center justify-between gap-3">
              <span className="text-[15px] font-semibold">
                {t("settings.machine.accounts.remote")}
              </span>
              <Toggle
                checked={form.account.remote_access}
                onChange={(v) => patch({ remote_access: v })}
                label={t("settings.machine.accounts.remote")}
              />
            </div>
            {single && !form.isNew ? (
              <button
                type="button"
                data-testid="account-advanced"
                aria-expanded={advanced}
                onClick={() => setAdvanced((v) => !v)}
                className="flex min-h-11 items-center justify-between gap-3 rounded-lg text-left text-[15px] font-semibold text-muted focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus"
              >
                <span>{t("settings.machine.accounts.advanced")}</span>
                <span aria-hidden="true" className="text-[13px] font-normal">
                  {t(
                    advanced
                      ? "settings.machine.accounts.advanced.hide"
                      : "settings.machine.accounts.advanced.show",
                  )}
                </span>
              </button>
            ) : null}
            {!single || form.isNew || advanced ? (
              <>
                <Field label={t("settings.machine.accounts.engine")}>
                  <select
                    className={fieldInputClass}
                    value={form.account.engine}
                    onChange={(e) => patch({ engine: e.target.value })}
                  >
                    {engineChoices(engines, form.account.engine).map((id) => (
                      <option key={id} value={id}>
                        {id}
                      </option>
                    ))}
                  </select>
                </Field>
                <Field
                  label={t("settings.machine.accounts.dir")}
                  hint={t("settings.machine.accounts.dir.hint")}
                >
                  <input
                    className={fieldInputClass}
                    value={form.account.config_dir ?? ""}
                    onChange={(e) => patch({ config_dir: e.target.value })}
                    autoCapitalize="none"
                    required
                  />
                </Field>
                <Field
                  label={t("settings.machine.accounts.env")}
                  hint={t("settings.machine.accounts.env.hint")}
                >
                  <input
                    className={fieldInputClass}
                    value={form.account.env_file ?? ""}
                    onChange={(e) => patch({ env_file: e.target.value })}
                    autoCapitalize="none"
                  />
                </Field>
                <div className="flex items-center justify-between gap-3">
                  <span className="text-[15px] font-semibold">
                    {t("settings.machine.accounts.warmup")}
                  </span>
                  <Toggle
                    checked={form.account.usage_warmup}
                    onChange={(v) => patch({ usage_warmup: v })}
                    label={t("settings.machine.accounts.warmup")}
                  />
                </div>
              </>
            ) : null}
            {form.account.default ? null : (
              <div className="flex items-center justify-between gap-3">
                <span className="text-[15px] font-semibold">
                  {t("settings.machine.accounts.makeDefault")}
                </span>
                <Toggle
                  checked={makeDefault}
                  onChange={setMakeDefault}
                  label={t("settings.machine.accounts.makeDefault")}
                />
              </div>
            )}
            {error ? (
              <p role="alert" className="text-xs leading-snug text-danger">
                {error}
              </p>
            ) : null}
            <div className="flex justify-end gap-2">
              <ActionButton tone="outline" onClick={() => setForm(null)}>
                {t("settings.machine.accounts.cancel")}
              </ActionButton>
              <ActionButton type="submit" tone="primary" busy={save.isPending}>
                {t("settings.machine.accounts.save")}
              </ActionButton>
            </div>
          </form>
        ) : null}
      </FormDialog>
    </SettingGroup>
  );
}
