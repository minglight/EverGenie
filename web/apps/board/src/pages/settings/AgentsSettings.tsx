import { api } from "@evergenie/sdk-web";
import { Avatar, showToast } from "@evergenie/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Pill } from "../../components/Pill";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { Toggle } from "../../components/Toggle";
import { useAgents } from "../../data/agents";
import { useShowCodes } from "../../data/queries";
import { type TextKey, t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { restartSession } from "../../lib/restartSession";
import { AccountRestart } from "./AccountRestart";
import { AgentKnowledge } from "./AgentKnowledge";
import { ClaimFunctions } from "./ClaimFunctions";
import { effortChoices, functionTitle, modelChoices, withCurrent } from "./functions";
import { PersonaOptions } from "./PersonaOptions";
import { assignablePersonas } from "./personas";
import { agentSettingsQuery, personasQuery } from "./queries";
import { SettingGroup } from "./SettingRow";

const selectClass =
  "min-h-11 w-full rounded-xl border border-border-strong bg-bg px-3 text-[15px] text-fg outline-none focus-visible:outline-2 focus-visible:outline-focus";

// 設定 → 職位與秘書（REQ-237，作者 2026-10-28）：一個職位一張卡，標題是職位（不是人名）；
// 卡裡選由誰（哪個角色）負責、用哪個帳號、預設模型與思考力度，還有啟用開關。
// 套件帶來、還沒人負責的職位在下面另一組（REQ-238）。compact：設定精靈用，只有負責的人與開關（帳號、模型之後再談）。
export function AgentsSettings({
  compact = false,
  focusClaim,
}: {
  compact?: boolean;
  /** 要捲到畫面上的認領卡（職位 id，`?claim=`）。 */
  focusClaim?: string;
}) {
  const query = useQuery(agentSettingsQuery);
  const personas = useQuery(personasQuery);
  const client = useQueryClient();
  const { view } = useAgents();
  const showCode = useShowCodes();
  const [confirming, setConfirming] = useState<{ id: string; name: string } | null>(null);
  const [errors, setErrors] = useState<Record<string, string>>({});
  // 剛換的角色（DEV-156 U-24）：一選就先畫新的頭像與下拉，不等名冊重讀；重讀好或失敗就拿掉。
  const [picked, setPicked] = useState<Record<string, string>>({});
  // 只拿掉自己選的那個：連續快速換時，前一次存好不會把後一次剛選的蓋回去（頭像不會跳一下）。
  const dropPicked = (id: string, persona: string) =>
    setPicked((p) => {
      if (p[id] !== persona) return p;
      const { [id]: _gone, ...rest } = p;
      return rest;
    });
  // 換帳號成功之後，那位秘書旁邊出現「重開她的對話」（DEV-106）：已經開著的對話還在用舊帳號，不重開不會換。
  const [restart, setRestart] = useState<Record<string, "offer" | "working" | "done">>({});
  const restartAgent = async (id: string) => {
    setRestart((r) => ({ ...r, [id]: "working" }));
    setErrors((e) => ({ ...e, [id]: "" }));
    try {
      await restartSession(api, { name: id, agent: id, kind: "resident" });
      setRestart((r) => ({ ...r, [id]: "done" }));
    } catch (err) {
      setRestart((r) => ({ ...r, [id]: "offer" }));
      setErrors((e) => ({ ...e, [id]: t("chat.restart.failed", { reason: reasonOf(err) }) }));
    }
  };

  const refresh = () =>
    Promise.all([
      client.invalidateQueries({ queryKey: agentSettingsQuery.queryKey }),
      client.invalidateQueries({ queryKey: ["roster"] }),
      client.invalidateQueries({ queryKey: ["personas"] }),
    ]);

  const fail = (id: string, err: unknown) =>
    setErrors((e) => ({ ...e, [id]: t("settings.agents.failed", { reason: reasonOf(err) }) }));
  // 下拉改了就存，存好要有反應（WEB-103）：說一聲已存、什麼時候生效（頁尾那行在手機上要捲很遠才看得到）。
  const saved = () => showToast(t("settings.agents.saved"));

  const setEnabled = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.agents.setField({ id, field: "enabled", value: enabled ? "true" : "false" }),
    onMutate: ({ id }) => setErrors((e) => ({ ...e, [id]: "" })),
    onError: (err, { id }) => fail(id, err),
    onSettled: refresh,
  });
  const setAccount = useMutation({
    mutationFn: ({ id, account }: { id: string; account: string }) =>
      api.agents.setAccount({ id, account }),
    onMutate: ({ id }) => setErrors((e) => ({ ...e, [id]: "" })),
    onSuccess: (_data, { id }) => {
      saved();
      setRestart((r) => ({ ...r, [id]: "offer" }));
    },
    onError: (err, { id }) => fail(id, err),
    onSettled: refresh,
  });

  const setModel = useMutation({
    mutationFn: ({ id, field, value }: { id: string; field: "model" | "effort"; value: string }) =>
      api.agents.setField({ id, field, value }),
    onMutate: ({ id }) => setErrors((e) => ({ ...e, [id]: "" })),
    onSuccess: saved,
    onError: (err, { id }) => fail(id, err),
    onSettled: refresh,
  });

  // 換角色（DEV-156 U-24）：一選就存，提示帶「復原」（換回原本那位）；復原後只說一聲「已換回」。
  const setPersona = useMutation({
    mutationFn: ({ id, persona }: { id: string; persona: string; prev: string; undo?: boolean }) =>
      api.agents.assign({ id, persona }),
    onMutate: ({ id, persona }) => {
      setErrors((e) => ({ ...e, [id]: "" }));
      setPicked((p) => ({ ...p, [id]: persona }));
    },
    onSuccess: (_data, { id, persona, prev, undo }) => {
      if (undo) {
        showToast(t("settings.agents.persona.undone"));
        return;
      }
      const name = personaName(persona);
      showToast(t("settings.agents.persona.saved", { name }), {
        undo: prev
          ? {
              label: t("ui.undo"),
              onClick: () => setPersona.mutate({ id, persona: prev, prev: persona, undo: true }),
            }
          : undefined,
      });
    },
    onError: (err, { id }) => fail(id, err),
    onSettled: async (_data, _err, { id, persona }) => {
      await refresh();
      dropPicked(id, persona);
    },
  });
  const personaName = (slug: string) =>
    (personas.data ?? []).find((c) => c.slug === slug)?.name || slug;

  if (query.data === undefined) {
    return query.isError ? (
      <LoadFailed what={t("settings.loadFailed")} onRetry={() => void query.refetch()} />
    ) : (
      <LoadingBlock />
    );
  }
  const { agents = [], accounts = [] } = query.data;
  // 換角色只列已上線、沒被官方蓋住的卡（設計中的不能指派，D-31）。
  const livePersonas = assignablePersonas(personas.data ?? []);
  return (
    <div className="flex flex-col gap-4">
      {agents.length === 0 ? (
        <p className="px-1 text-[13px] text-muted">{t("settings.agents.empty")}</p>
      ) : null}
      <SettingGroup className={agents.length === 0 ? "hidden" : undefined}>
        {agents.map((a) => {
          const id = a.id ?? "";
          const v = view(id);
          const fn = functionTitle(a.duty, id);
          // 帳號下拉：名冊上的帳號不在清單裡（被改掉了）也要顯示出來，免得看起來像「沒設定」。
          const choices = withCurrent(accounts, a.account);
          // 頭像跟著選的角色走：用那張角色卡自己的頭像（換了馬上換，不是等瀏覽器重抓同一個網址，DEV-156 U-24）；
          // 卡沒有頭像才用這位秘書的頭像，網址帶上角色代號，換了一樣會重抓。
          const current = picked[id] ?? a.persona ?? "";
          const card = (personas.data ?? []).find((c) => c.slug === current);
          return (
            <div
              key={id}
              data-testid="agent-row"
              data-agent={id}
              className="flex flex-col gap-3 px-4 py-3.5"
            >
              <div className="flex items-center gap-3">
                <div className="min-w-0 flex-1">
                  <h3 className="m-0 flex flex-wrap items-center gap-x-2 gap-y-1 text-[16px] leading-snug font-semibold">
                    {fn}
                    {showCode ? (
                      <span className="font-mono text-xs font-normal text-muted">
                        {t("settings.agents.fn", { id })}
                      </span>
                    ) : null}
                    {a.enabled ? null : <Pill tone="muted">{t("settings.agents.off")}</Pill>}
                  </h3>
                  {a.duty && a.duty !== fn ? (
                    <p className="mt-0.5 text-[13px] leading-snug text-muted">{a.duty}</p>
                  ) : null}
                </div>
                <Toggle
                  checked={a.enabled}
                  label={t("settings.agents.enabled", { name: fn })}
                  onChange={(next) =>
                    next
                      ? setEnabled.mutate({ id, enabled: true })
                      : setConfirming({ id, name: fn })
                  }
                />
              </div>
              <label className="flex flex-col gap-1">
                <span className="text-xs font-semibold text-muted">
                  {t("settings.agents.persona")}
                </span>
                <span className="flex items-center gap-2.5">
                  <Avatar
                    name={card?.name || a.name || id}
                    color={v.color}
                    colorDark={v.colorDark}
                    // 卡沒給網址＝這位角色沒有圖（DEV-157）：畫名字，不去抓一張一定 404 的圖。
                    src={card?.avatar_url || undefined}
                    size={40}
                  />
                  <select
                    className={selectClass}
                    value={current}
                    data-testid="agent-persona"
                    aria-label={t("settings.agents.persona.aria", { name: fn })}
                    onChange={(e) =>
                      setPersona.mutate({ id, persona: e.target.value, prev: current })
                    }
                  >
                    {/* 現任的角色不在清單裡（設計中、卡片不見了）也要顯示出來，免得看起來像「沒設定」。 */}
                    {livePersonas.some((c) => c.slug === current) ? null : (
                      <option value={current}>{card?.name || a.name || current || "—"}</option>
                    )}
                    <PersonaOptions cards={livePersonas} />
                  </select>
                </span>
                <span className="text-xs leading-snug text-muted">
                  {t("settings.agents.persona.hint")}
                </span>
              </label>
              <div className={compact ? "hidden" : "grid gap-3 md:grid-cols-3"}>
                <label className="flex flex-col gap-1">
                  <span className="text-xs font-semibold text-muted">
                    {t("settings.agents.account")}
                  </span>
                  <select
                    className={selectClass}
                    value={a.account ?? ""}
                    aria-label={t("settings.agents.account.aria", { name: fn })}
                    disabled={restart[id] === "working"}
                    onChange={(e) => setAccount.mutate({ id, account: e.target.value })}
                  >
                    {a.account ? null : <option value="">{t("settings.agents.notSet")}</option>}
                    {choices.map((c) => (
                      <option key={c} value={c}>
                        {c}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="flex flex-col gap-1">
                  <span className="text-xs font-semibold text-muted">
                    {t("settings.agents.model")}
                  </span>
                  <select
                    className={selectClass}
                    value={a.model ?? ""}
                    data-testid="agent-model"
                    aria-label={t("settings.agents.model.aria", { name: fn })}
                    onChange={(e) => setModel.mutate({ id, field: "model", value: e.target.value })}
                  >
                    {a.model ? null : <option value="">{t("settings.agents.notSet")}</option>}
                    {withCurrent(modelChoices, a.model).map((m) => (
                      <option key={m} value={m}>
                        {m}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="flex flex-col gap-1">
                  <span className="text-xs font-semibold text-muted">
                    {t("settings.agents.effort")}
                  </span>
                  <select
                    className={selectClass}
                    value={a.effort ?? ""}
                    data-testid="agent-effort"
                    aria-label={t("settings.agents.effort.aria", { name: fn })}
                    onChange={(e) =>
                      setModel.mutate({ id, field: "effort", value: e.target.value })
                    }
                  >
                    {withCurrent(effortChoices, a.effort).map((x) => (
                      <option key={x} value={x}>
                        {x === ""
                          ? t("settings.agents.notSet")
                          : effortChoices.includes(x as (typeof effortChoices)[number])
                            ? t(`settings.agents.effort.${x}` as TextKey)
                            : x}
                      </option>
                    ))}
                  </select>
                </label>
              </div>
              {compact ? null : <AgentKnowledge agent={id} />}
              {restart[id] && !compact && a.enabled !== false ? (
                <AccountRestart
                  phase={restart[id]}
                  name={a.name || fn}
                  onRestart={() => void restartAgent(id)}
                />
              ) : null}
              {errors[id] ? (
                <p role="alert" className="text-xs leading-snug text-danger">
                  {errors[id]}
                </p>
              ) : null}
            </div>
          );
        })}
      </SettingGroup>
      <ClaimFunctions focus={focusClaim} withOptional={!compact} />
      {compact ? null : (
        <p className="px-1 text-[13px] text-muted">{t("settings.agents.footer")}</p>
      )}
      <ConfirmDialog
        open={confirming !== null}
        title={t("settings.agents.disable.title", { name: confirming?.name ?? "" })}
        body={t("settings.agents.disable.body")}
        confirmLabel={t("settings.agents.disable.yes")}
        cancelLabel={t("settings.agents.disable.no")}
        onCancel={() => setConfirming(null)}
        onConfirm={() => {
          if (confirming) setEnabled.mutate({ id: confirming.id, enabled: false });
          setConfirming(null);
        }}
      />
    </div>
  );
}
