import { api } from "@evergenie/sdk-web";
import { cn } from "@evergenie/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useId, useRef, useState } from "react";
import { ActionButton } from "../../components/ActionButton";
import { Pill, type PillTone } from "../../components/Pill";
import { SiteNotice } from "../../components/SiteNotice";
import { siteColor, siteName } from "../../components/SiteTag";
import { LoadFailed, LoadingBlock } from "../../components/StateViews";
import { forgetSiteInAgentFilter } from "../../data/agentFilter";
import {
  reprobeSites,
  SITES_CONFIG_KEY,
  type Site,
  type SiteConfig,
  type SiteState,
  sitesConfigQuery,
  useSites,
} from "../../data/sites";
import { type TextKey, t } from "../../i18n/zh-TW";
import { reasonOf } from "../../lib/reason";
import { Field, fieldInputClass } from "./formBits";
import { webTrustQuery } from "./queries";
import { SettingGroup } from "./SettingRow";

// 設定 → 電腦 → 「看板連的電腦」與「誰可以連進這台」（S3-9，WEB-80；計畫 2026-11-01-board-multi-home）。
// 台清單（[[web.sites]]）可以加、拿掉；這台自己的信任設定（allow_boards、owner_login）可以看、可以改；
// 別台的信任設定只顯示，要改到那台自己的看板（伺服器也擋：跨站來的改信任設定一律 403，WEB-79）。

const stateTone: Record<SiteState, PillTone> = {
  ok: "ok",
  old: "muted",
  denied: "danger",
  down: "danger",
  checking: "muted",
};

/**
 * 加一台時可選的顏色（[[web.sites]] color 只收 #rgb／#rrggbb，存在 home.toml，所以是 hex 不是 token：FEN-7 的例外，WEB-80）。
 * 不給橘（只給「等你處理」，WEB-70）、紅（danger）。「預設」＝不寫，照電腦給預設色。
 */
export const SITE_COLORS: { name: string; hex: string }[] = [
  { name: "violet", hex: "#7c5cff" },
  { name: "blue", hex: "#3b82f6" },
  { name: "teal", hex: "#14b8a6" },
  { name: "green", hex: "#22c55e" },
];

/** 這個看板自己的 origin（別台的 allow_boards 要放的就是它）。 */
function boardOrigin(): string {
  return typeof window === "undefined" ? "" : window.location.origin;
}

function SiteDot({ color }: { color: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn("mt-1.5 size-3 shrink-0 rounded-full", color ? "" : "bg-border-strong")}
      style={color ? { backgroundColor: color } : undefined}
    />
  );
}

/** 別台的信任設定：只顯示，旁邊連到那台的看板去改。 */
function RemoteTrust({ site }: { site: Site }) {
  const q = useQuery(webTrustQuery(site));
  if (!q.data) return null;
  const none = t("settings.machine.sites.remote.none");
  // 用使用者自己設、伺服器整理過的網址，不用那台自己回報的 public_url（別台可能被塞進來，WEB-80）。
  const href = `${site.base}/settings/machine`;
  return (
    <div data-testid="site-remote-trust" className="flex flex-col gap-0.5 text-[13px] text-muted">
      <p className="break-all">
        {t("settings.machine.sites.remote.owner", { login: q.data.ownerLogin || none })}
      </p>
      <p className="break-all">
        {t("settings.machine.sites.remote.allow", {
          list: q.data.allowBoards.length > 0 ? q.data.allowBoards.join("、") : none,
        })}
      </p>
      {/* hg-allow-new-tab: 另一台的看板（別的網站） */}
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="self-start font-semibold text-accent underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-focus"
      >
        {t("settings.machine.sites.remote.edit")}
      </a>
    </div>
  );
}

function SiteRow({
  site,
  onRemove,
  removing,
  canEdit,
}: {
  site: Site;
  onRemove?: () => void;
  removing: boolean;
  canEdit: boolean;
}) {
  const [confirm, setConfirm] = useState(false);
  // 展開確認時把焦點放到「確定拿掉」（原本的按鈕被換掉，焦點不要掉到 body）。
  const actionsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (confirm) actionsRef.current?.querySelector("button")?.focus();
  }, [confirm]);
  // 主伺服器還沒讀到台名時叫「這台」。
  const name = site.primary && !site.label ? t("site.thisOne") : siteName(site);
  const url = site.primary ? site.publicUrl || boardOrigin() : site.base;
  return (
    <div
      data-testid="site-row"
      data-site={site.key}
      data-state={site.state}
      className="flex flex-col gap-2 px-4 py-3.5"
    >
      <div className="flex items-start gap-3">
        <SiteDot color={siteColor(site)} />
        <div className="min-w-0 flex-1">
          <p className="flex flex-wrap items-center gap-2 text-[15px] leading-snug font-semibold">
            <span className="min-w-0 break-all">{name}</span>
            {site.primary ? <Pill tone="muted">{t("settings.machine.sites.this")}</Pill> : null}
          </p>
          <p className="mt-0.5 text-[13px] leading-snug text-muted break-all">{url}</p>
          {site.version ? (
            <p data-testid="site-version" className="text-[13px] leading-snug text-muted">
              {t("settings.machine.sites.version", { version: site.version })}
            </p>
          ) : null}
        </div>
        <Pill tone={stateTone[site.state]} className="mt-0.5">
          <span data-testid="site-state">
            {t(`settings.machine.sites.state.${site.state}` as TextKey)}
          </span>
        </Pill>
      </div>
      <SiteNotice site={site} />
      {/* 被那台伺服器拒絕（env 讀不到）才提示去允許；正式／開發不同時加允許也沒用。 */}
      {!site.primary && site.state === "denied" && site.env === "" ? (
        <p className="text-[13px] leading-snug text-muted break-words">
          {t("settings.machine.sites.allowHint", { origin: boardOrigin() })}
        </p>
      ) : null}
      {!site.primary && site.state === "ok" ? <RemoteTrust site={site} /> : null}
      {onRemove ? (
        <div ref={actionsRef} className="flex justify-end gap-1.5">
          {confirm ? (
            <>
              <ActionButton
                tone="danger"
                size="pane"
                busy={removing}
                disabled={!canEdit}
                onClick={() => {
                  setConfirm(false);
                  onRemove();
                }}
              >
                {t("settings.machine.sites.rm.yes")}
              </ActionButton>
              <ActionButton tone="outline" size="pane" onClick={() => setConfirm(false)}>
                {t("settings.machine.sites.rm.no")}
              </ActionButton>
            </>
          ) : (
            <button
              type="button"
              aria-label={t("settings.machine.sites.rm", { name })}
              disabled={!canEdit}
              onClick={() => setConfirm(true)}
              className="min-h-11 shrink-0 rounded-lg px-2 text-[13px] font-semibold text-danger focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50"
            >
              {t("settings.machine.sites.rm.short")}
            </button>
          )}
        </div>
      ) : null}
    </div>
  );
}

function ColorPicker({ value, onChange }: { value: string; onChange: (hex: string) => void }) {
  const choices = [{ name: "default", hex: "" }, ...SITE_COLORS];
  return (
    <fieldset className="flex flex-col gap-1.5">
      <legend className="mb-1.5 text-[15px] font-semibold">
        {t("settings.machine.sites.add.color")}
      </legend>
      <div className="flex flex-wrap gap-2">
        {choices.map((c) => {
          const label = t(`settings.machine.sites.color.${c.name}` as TextKey);
          const on = value === c.hex;
          return (
            <label
              key={c.name}
              className={cn(
                "flex min-h-11 cursor-pointer items-center gap-1.5 rounded-xl border px-3 text-[14px] has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-focus",
                on ? "border-accent bg-accent-soft font-semibold" : "border-border bg-surface",
              )}
            >
              <input
                type="radio"
                name="site-color"
                value={c.hex}
                checked={on}
                onChange={() => onChange(c.hex)}
                className="sr-only"
              />
              <SiteDot color={c.hex} />
              {label}
            </label>
          );
        })}
      </div>
    </fieldset>
  );
}

function plain(configs: readonly SiteConfig[]): { url: string; color?: string }[] {
  return configs.map((c) => (c.color ? { url: c.url, color: c.color } : { url: c.url }));
}

/** 看板連的電腦：各台的狀態，加一台、拿掉一台、立刻試連。 */
function Sites() {
  const sites = useSites();
  const configs = useQuery(sitesConfigQuery);
  const client = useQueryClient();
  const urlId = useId();
  const [url, setUrl] = useState("");
  const [color, setColor] = useState("");
  const [error, setError] = useState("");

  const after = async () => {
    await client.invalidateQueries({ queryKey: SITES_CONFIG_KEY });
    await reprobeSites(client);
  };
  // set_sites 整份換掉：送之前先重讀最新那份再改一筆（CLI 或別的分頁剛改過的不會被蓋掉）。
  const save = useMutation({
    mutationFn: async (
      edit: (cur: { url: string; color?: string }[]) => { url: string; color?: string }[],
    ) => {
      const fresh = await client.fetchQuery({ ...sitesConfigQuery, staleTime: 0 });
      return api.web.setSites({ sites: edit(plain(fresh)) });
    },
    onMutate: () => setError(""),
    onError: (err) => setError(t("settings.machine.sites.failed", { reason: reasonOf(err) })),
    onSettled: after,
  });

  // 台清單沒讀到（載入中、讀不到）時不能加、不能拿掉：整份換掉會把原本的台蓋掉。
  const canEdit = configs.data !== undefined && !save.isPending;
  const add = () => {
    const u = url.trim();
    if (!u || !canEdit) return;
    const entry = color ? { url: u, color } : { url: u };
    save.mutate((cur) => [...cur, entry], {
      onSuccess: () => {
        setUrl("");
        setColor("");
      },
    });
  };
  const remove = (site: Site) =>
    save.mutate((cur) => cur.filter((c) => c.url !== site.base), {
      onSuccess: () => forgetSiteInAgentFilter(site.key),
    });

  return (
    <SettingGroup title={t("settings.machine.sites")}>
      <p className="px-4 py-3 text-[13px] leading-snug text-muted">
        {t("settings.machine.sites.hint")}
      </p>
      {configs.data === undefined && configs.isPending ? (
        <LoadingBlock />
      ) : configs.data === undefined ? (
        <LoadFailed what={t("settings.loadFailed")} onRetry={() => void configs.refetch()} />
      ) : (
        sites.map((s) => (
          <SiteRow
            key={s.base || "primary"}
            site={s}
            removing={save.isPending}
            canEdit={canEdit}
            onRemove={s.primary ? undefined : () => remove(s)}
          />
        ))
      )}
      <form
        data-testid="site-add"
        className="flex flex-col gap-3 px-4 py-3.5"
        onSubmit={(e) => {
          e.preventDefault();
          add();
        }}
      >
        <p className="text-[15px] font-semibold">{t("settings.machine.sites.add")}</p>
        <Field
          label={t("settings.machine.sites.add.url")}
          hint={t("settings.machine.sites.add.url.hint")}
        >
          <input
            id={urlId}
            type="text"
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
            placeholder="https://"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            className={fieldInputClass}
          />
        </Field>
        <ColorPicker value={color} onChange={setColor} />
        {error ? (
          <p role="alert" className="text-xs leading-snug text-danger">
            {error}
          </p>
        ) : null}
        <div className="flex flex-wrap gap-2">
          <ActionButton
            type="submit"
            tone="primary"
            busy={save.isPending}
            disabled={!url.trim() || configs.data === undefined}
          >
            {t("settings.machine.sites.add.submit")}
          </ActionButton>
          {sites.length > 1 ? (
            <ActionButton tone="outline" onClick={() => void reprobeSites(client)}>
              {t("settings.machine.sites.reprobe")}
            </ActionButton>
          ) : null}
        </div>
      </form>
    </SettingGroup>
  );
}

/** 誰可以連進這台：這台自己的 owner_login 與 allow_boards（只有這台自己的看板改得了）。 */
function LocalTrust() {
  const q = useQuery(webTrustQuery());
  const client = useQueryClient();
  const ownerId = useId();
  const allowId = useId();
  const [owner, setOwner] = useState<string | null>(null);
  const [origin, setOrigin] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(false);

  // 伺服器那份變了（別處改過、存好重抓）就換掉畫面上沒改的值。
  const serverOwner = q.data?.ownerLogin;
  useEffect(() => {
    if (serverOwner !== undefined) setOwner(null);
  }, [serverOwner]);

  const done = {
    onMutate: () => {
      setError("");
      setSaved(false);
    },
    onError: (err: unknown) =>
      setError(t("settings.machine.sites.failed", { reason: reasonOf(err) })),
    onSuccess: () => setSaved(true),
    onSettled: () => client.invalidateQueries({ queryKey: SITES_CONFIG_KEY }),
  };
  const setOwnerLogin = useMutation({
    mutationFn: (login: string) => api.web.setOwnerLogin({ owner_login: login }),
    ...done,
  });
  // allow_boards 也是整份換掉：送之前先重讀最新那份。
  const setAllow = useMutation({
    mutationFn: async (edit: (cur: string[]) => string[]) => {
      const fresh = await client.fetchQuery({ ...webTrustQuery(), staleTime: 0 });
      return api.web.setAllowBoards({ allow_boards: edit(fresh.allowBoards) });
    },
    ...done,
  });

  if (!q.data) {
    return (
      <SettingGroup title={t("settings.machine.trust")}>
        {q.isError ? (
          <LoadFailed what={t("settings.loadFailed")} onRetry={() => void q.refetch()} />
        ) : (
          <LoadingBlock />
        )}
      </SettingGroup>
    );
  }
  const allow = q.data.allowBoards;
  const ownerValue = owner ?? q.data.ownerLogin;
  return (
    <SettingGroup title={t("settings.machine.trust")}>
      <div data-testid="local-trust" className="flex flex-col gap-4 px-4 py-3.5">
        <p className="text-[13px] leading-snug text-muted">{t("settings.machine.trust.hint")}</p>
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setOwnerLogin.mutate(ownerValue.trim());
          }}
        >
          <Field
            label={t("settings.machine.trust.owner")}
            hint={t("settings.machine.trust.owner.hint")}
          >
            <input
              id={ownerId}
              type="text"
              inputMode="email"
              autoComplete="off"
              spellCheck={false}
              value={ownerValue}
              onChange={(e) => setOwner(e.target.value)}
              className={fieldInputClass}
            />
          </Field>
          <ActionButton
            type="submit"
            tone="outline"
            className="self-start"
            busy={setOwnerLogin.isPending}
            disabled={ownerValue.trim() === q.data.ownerLogin}
          >
            {t("settings.machine.trust.owner.save")}
          </ActionButton>
        </form>
        <div className="flex flex-col gap-2">
          <p className="text-[15px] font-semibold">{t("settings.machine.trust.allow")}</p>
          {allow.length === 0 ? (
            <p className="text-[13px] text-muted">{t("settings.machine.trust.allow.empty")}</p>
          ) : (
            <ul className="flex flex-col divide-y divide-border rounded-[10px] border border-border">
              {allow.map((o) => (
                <li
                  key={o}
                  data-testid="allow-board"
                  className="flex items-center justify-between gap-2 px-3 py-1"
                >
                  <span className="min-w-0 text-[14px] break-all">{o}</span>
                  <button
                    type="button"
                    aria-label={t("settings.machine.trust.allow.rm", { origin: o })}
                    disabled={setAllow.isPending}
                    onClick={() => setAllow.mutate((cur) => cur.filter((x) => x !== o))}
                    className="min-h-11 shrink-0 rounded-lg px-2 text-[13px] font-semibold text-danger focus-visible:outline-2 focus-visible:outline-focus disabled:opacity-50"
                  >
                    {t("settings.machine.sites.rm.short")}
                  </button>
                </li>
              ))}
            </ul>
          )}
          <form
            className="flex flex-col gap-2 sm:flex-row"
            onSubmit={(e) => {
              e.preventDefault();
              const o = origin.trim();
              if (!o) return;
              setAllow.mutate((cur) => (cur.includes(o) ? cur : [...cur, o]), {
                onSuccess: () => setOrigin(""),
              });
            }}
          >
            <label htmlFor={allowId} className="sr-only">
              {t("settings.machine.trust.allow.input")}
            </label>
            <input
              id={allowId}
              type="text"
              inputMode="url"
              autoComplete="off"
              spellCheck={false}
              placeholder="https://"
              value={origin}
              onChange={(e) => setOrigin(e.target.value)}
              className={fieldInputClass}
            />
            <ActionButton
              type="submit"
              tone="outline"
              busy={setAllow.isPending}
              disabled={!origin.trim()}
            >
              {t("settings.machine.trust.allow.add")}
            </ActionButton>
          </form>
        </div>
        {error ? (
          <p role="alert" className="text-xs leading-snug text-danger">
            {error}
          </p>
        ) : saved ? (
          <p role="status" className="text-xs leading-snug text-ok">
            {t("settings.machine.trust.saved")}
          </p>
        ) : null}
      </div>
    </SettingGroup>
  );
}

export function SitesSettings() {
  return (
    <>
      <Sites />
      <LocalTrust />
    </>
  );
}
