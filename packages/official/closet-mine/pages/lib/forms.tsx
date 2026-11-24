// closet-mine 頁面共用的表單與卡片（DEV-143 從舊的上傳頁拆出來）：衣服分頁（outfits）與新增角色（new-character）共用。
// 一般家與開發家長得一樣（DEV-160 審查 S1，CMI-24）：開發／設計用的（採用、評論、整套的狀態、放回開發中、升成官方）
// 只在衣櫃的「修改」分頁（CLO-35）；這裡只差設計中、下架兩個標籤（tagsOf 看 edit）。頁面不問是哪種家。
// 頁面只上傳檔案並寫 closet-mine.request（先在本機擋掉太大與類型不對的）；真正的驗證與複製由套件的程式做，判斷以它為準（REQ-126）。
import { Button, Field } from "@evergenie/ui";
import { type ChangeEvent, type FormEvent, useRef, useState } from "react";
import {
  checkFile,
  checkName,
  costOf,
  type FileWant,
  locked,
  type MineOutfit,
  outfitDetailHref,
  outfitParts,
  type PhotoGroup,
  personaFromResult,
  pointsText,
  T,
  tagsOf,
  thumbOf,
  validCode,
} from "./mine";
import { card, ResultLine, type Sender, Thumb } from "./parts";
import { ConfirmLine, PhotoList } from "./photos";

const inputCls = "min-h-11 w-full rounded-lg border border-border bg-bg px-3 text-base";
const ACCEPT_IMAGE = "image/png,image/jpeg,image/webp";

export function Tag({ children, tone = "muted" }: { children: string; tone?: "muted" | "accent" }) {
  return (
    <span
      className={
        tone === "accent"
          ? "rounded-full bg-accent-soft px-2 py-0.5 text-xs font-medium text-accent"
          : "rounded-full bg-surface-2 px-2 py-0.5 text-xs text-muted"
      }
    >
      {children}
    </span>
  );
}

/**
 * 一套衣服缺什麼（DEV-143）：半身、全身、輪播、動畫四格，有的用強調色、缺的寫「缺…」。
 * 還沒兌換的衣服只看得到剪影，改寫「還沒兌換」，不判斷缺什麼（開發家沒兌換的照樣有真圖，照常列，CLO-27）。
 */
export function OutfitParts({ outfit }: { outfit: MineOutfit }) {
  if (locked(outfit)) {
    return (
      <p
        data-testid={`mine-parts-${outfit.record}`}
        data-locked="true"
        className="text-xs text-muted"
      >
        {T.lockedNote}
      </p>
    );
  }
  return (
    <ul
      data-testid={`mine-parts-${outfit.record}`}
      aria-label={T.partsLabel}
      className="flex flex-wrap gap-1"
    >
      {outfitParts(outfit).map((p) => (
        <li
          key={p.key}
          data-part={p.key}
          data-ok={p.ok ? "true" : "false"}
          className={
            p.ok
              ? "rounded-full bg-accent-soft px-2 py-0.5 text-xs font-medium text-accent"
              : "rounded-full border border-dashed border-border-strong px-2 py-0.5 text-xs text-muted"
          }
        >
          {p.label}
        </li>
      ))}
    </ul>
  );
}

/** 不是我的衣服（官方的）：只能看、到衣櫃兌換或看詳情。 */
export function OfficialOutfitCard({ outfit, edit }: { outfit: MineOutfit; edit: boolean }) {
  return (
    <li
      data-testid={`mine-outfit-${outfit.record}`}
      className="flex gap-3 rounded-xl bg-surface-2 p-3"
    >
      <div className="w-20 shrink-0">
        <Thumb url={thumbOf(outfit)} />
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="break-words font-semibold">{outfit.name}</p>
        <div className="flex flex-wrap gap-1">
          {tagsOf(outfit, edit).map((t) => (
            <Tag key={t}>{t}</Tag>
          ))}
        </div>
        <OutfitParts outfit={outfit} />
        <a
          href={outfitDetailHref(outfit.record)}
          className="mt-auto inline-flex min-h-11 items-center text-sm font-medium text-accent no-underline"
        >
          {outfit.owned ? T.openDetail : T.toRedeem}
        </a>
      </div>
    </li>
  );
}

export function FileField({
  label,
  hint,
  want,
  file,
  onFile,
  testId,
  error,
}: {
  label: string;
  hint?: string;
  want: FileWant;
  file: File | null;
  onFile: (f: File | null) => void;
  testId: string;
  error?: string;
}) {
  return (
    <Field label={label} hint={hint} error={error}>
      {(c) => (
        // 原生的檔案欄位文字是瀏覽器語言，所以藏起來、用整個可點的按鈕顯示「選擇檔案」或檔名。
        <label className="flex min-h-11 w-full cursor-pointer items-center rounded-lg border border-border bg-bg px-3 text-base focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-focus">
          <input
            {...c}
            data-testid={testId}
            type="file"
            accept={want === "video" ? "video/mp4" : ACCEPT_IMAGE}
            onChange={(e) => onFile(e.target.files?.[0] ?? null)}
            className="sr-only"
            key={file ? "has" : "none"}
          />
          <span className={file ? "min-w-0 truncate" : "text-muted"}>
            {file ? file.name : T.chooseFile}
          </span>
        </label>
      )}
    </Field>
  );
}

export function NewCharacter({
  sender,
  onDone,
  onCancel,
}: {
  sender: Sender;
  /** 建好了：帶新角色的代號（程式回填的 result 是 `<代號>-base`）。 */
  onDone: (persona: string) => void;
  onCancel?: () => void;
}) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [code, setCode] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [errors, setErrors] = useState<{ name?: string; file?: string; code?: string }>({});

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const next = {
      name: checkName(name) || undefined,
      file: checkFile(file, "image") || undefined,
      code: code.trim() !== "" && !validCode(code.trim()) ? T.badCode : undefined,
    };
    setErrors(next);
    if (next.name || next.file || next.code || !file) return;
    const out = await sender.send(file, { type: "character", persona: code, name, desc });
    if (out.kind === "done") {
      setName("");
      setDesc("");
      setCode("");
      setFile(null);
      onDone(personaFromResult(out.result));
    }
  };

  return (
    <form data-testid="mine-new" onSubmit={submit} className="flex flex-col gap-3" noValidate>
      <p className="text-sm text-muted">{T.newHint}</p>
      <Field label={T.charName} error={errors.name}>
        {(c) => (
          <input
            {...c}
            data-testid="mine-new-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className={inputCls}
          />
        )}
      </Field>
      <Field label={T.charDesc}>
        {(c) => (
          <input
            {...c}
            data-testid="mine-new-desc"
            value={desc}
            onChange={(e) => setDesc(e.target.value)}
            className={inputCls}
          />
        )}
      </Field>
      <FileField
        label={T.charPhoto}
        hint={T.charPhotoHint}
        want="image"
        file={file}
        onFile={setFile}
        testId="mine-new-file"
        error={errors.file}
      />
      <details>
        <summary className="min-h-11 cursor-pointer py-2.5 text-sm font-medium">
          {T.advanced}
        </summary>
        <Field label={T.codeLabel} hint={T.codeHint} error={errors.code}>
          {(c) => (
            <input
              {...c}
              data-testid="mine-new-code"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              autoCapitalize="none"
              className={inputCls}
            />
          )}
        </Field>
      </details>
      <ResultLine sender={sender} />
      <div className="flex flex-wrap gap-2">
        <Button
          type="submit"
          variant="primary"
          disabled={sender.busy}
          data-testid="mine-new-submit"
        >
          {sender.busy ? T.sending : T.createBtn}
        </Button>
        {onCancel ? (
          <Button variant="secondary" disabled={sender.busy} onClick={onCancel}>
            {T.cancelBtn}
          </Button>
        ) : null}
      </div>
    </form>
  );
}

/** 「加一套衣服」的結果行（表單最下面）。 */
const ADD_AT = "add";

export function AddOutfit({ sender, persona }: { sender: Sender; persona: string }) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [paid, setPaid] = useState(false);
  const [points, setPoints] = useState(pointsText(0));
  const [file, setFile] = useState<File | null>(null);
  const [errors, setErrors] = useState<{ name?: string; file?: string; points?: string }>({});

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const cost = costOf(paid, points);
    const next = {
      name: checkName(name) || undefined,
      file: checkFile(file, "image") || undefined,
      points: cost === null ? T.pointsBad : undefined,
    };
    setErrors(next);
    if (next.name || next.file || next.points || !file || cost === null) return;
    // 「要用點數換」關＝0 點（一上傳就擁有）；開＝填的點數，沒兌換前衣櫃只給剪影。頁面不送 draft：上傳直接生效（CMI-22），
    // 開發家要先收起來就到「修改」分頁「放回開發中」（CLO-35）。
    const out = await sender.send(
      file,
      { type: "outfit", persona, name, desc, cost },
      { at: ADD_AT },
    );
    if (out.kind === "done") {
      setName("");
      setDesc("");
      setPaid(false);
      setPoints(pointsText(0));
      setFile(null);
    }
  };

  return (
    <form data-testid="mine-outfit" onSubmit={submit} className={card} noValidate>
      <div>
        <h3 className="text-base font-semibold">{T.outfitTitle}</h3>
        <p className="text-sm text-muted">{T.outfitHint}</p>
      </div>
      <Field label={T.outfitName} error={errors.name}>
        {(c) => (
          <input
            {...c}
            data-testid="mine-outfit-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className={inputCls}
          />
        )}
      </Field>
      <Field label={T.outfitDesc}>
        {(c) => (
          <input
            {...c}
            data-testid="mine-outfit-desc"
            value={desc}
            onChange={(e) => setDesc(e.target.value)}
            className={inputCls}
          />
        )}
      </Field>
      <FileField
        label={T.outfitPhoto}
        hint={T.charPhotoHint}
        want="image"
        file={file}
        onFile={setFile}
        testId="mine-outfit-file"
        error={errors.file}
      />
      <Check
        role="switch"
        testId="mine-outfit-paid"
        label={T.paid}
        hint={T.paidHint}
        checked={paid}
        onChange={setPaid}
      />
      {paid ? (
        <PointsField
          testId="mine-outfit-cost"
          value={points}
          onChange={setPoints}
          error={errors.points}
        />
      ) : null}
      <Button
        type="submit"
        variant="primary"
        disabled={sender.busy}
        data-testid="mine-outfit-submit"
      >
        {sender.busy ? T.sending : T.addOutfitBtn}
      </Button>
      <ResultLine sender={sender} at={ADD_AT} />
    </form>
  );
}

function PointsField({
  value,
  onChange,
  error,
  testId,
}: {
  value: string;
  onChange: (v: string) => void;
  error?: string;
  testId: string;
}) {
  return (
    <Field label={T.pointsLabel} hint={T.pointsHint} error={error}>
      {(c) => (
        <input
          {...c}
          data-testid={testId}
          type="number"
          inputMode="numeric"
          min={1}
          max={99}
          step={1}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          className={`${inputCls} max-w-32`}
        />
      )}
    </Field>
  );
}

export function Check({
  label,
  hint,
  checked,
  onChange,
  testId,
  role,
}: {
  label: string;
  hint: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  testId: string;
  /** 開關（開／關一件事）用 switch，讀屏唸「開關」。 */
  role?: "switch";
}) {
  return (
    <label className="flex min-h-11 items-start gap-3 py-1">
      <input
        data-testid={testId}
        type="checkbox"
        role={role}
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
        className="mt-1 size-5 shrink-0"
      />
      <span className="flex flex-col">
        <span className="text-[15px] font-medium">{label}</span>
        <span className="text-xs text-muted">{hint}</span>
      </span>
    </label>
  );
}

/** 一套的「要用點數換」（set_cost，CMI-21）：開關＋幾點，改了才出現「改好了」。基本款一定免費，不畫。 */
function CostEditor({
  persona,
  outfit,
  sender,
}: {
  persona: string;
  outfit: MineOutfit;
  sender: Sender;
}) {
  const [paid, setPaid] = useState(outfit.cost > 0);
  const [points, setPoints] = useState(pointsText(outfit.cost));
  const [error, setError] = useState("");
  const cost = costOf(paid, points);
  const changed = cost !== outfit.cost;
  const save = async () => {
    if (cost === null) {
      setError(T.pointsBad);
      return;
    }
    setError("");
    await sender.send(
      null,
      { type: "set_cost", persona, outfit: outfit.outfit, cost },
      { doneText: T.doneCost, at: `cost:${outfit.record}` },
    );
  };
  return (
    <div data-testid={`mine-cost-${outfit.record}`} className="flex flex-col gap-1">
      <Check
        role="switch"
        testId={`mine-cost-paid-${outfit.record}`}
        label={T.costTitle}
        hint={T.costNow(outfit.cost)}
        checked={paid}
        onChange={(v) => {
          setPaid(v);
          setError("");
        }}
      />
      {paid ? (
        <PointsField
          testId={`mine-cost-points-${outfit.record}`}
          value={points}
          onChange={setPoints}
          error={error}
        />
      ) : null}
      {changed ? (
        <Button
          variant="secondary"
          className="self-start"
          disabled={sender.busy}
          onClick={() => void save()}
          data-testid={`mine-cost-save-${outfit.record}`}
        >
          {T.costSave}
        </Button>
      ) : null}
      <ResultLine sender={sender} at={`cost:${outfit.record}`} />
    </div>
  );
}

export function MineOutfitCard({
  persona,
  outfit,
  groups,
  sender,
  adult,
  edit,
  onRemoved,
}: {
  persona: string;
  outfit: MineOutfit;
  /** 這套的照片（照片紀錄，一版一組；photoGroups）。 */
  groups: PhotoGroup[];
  sender: Sender;
  /** 自己的隱藏版開關開著：才出現「放進自己的隱藏版」。 */
  adult: boolean;
  /** 這個家能修改衣服（開發家）：標籤多設計中、下架（tagsOf）；其他一樣。 */
  edit: boolean;
  /** 整套刪掉了（回總覽）。 */
  onRemoved: () => void;
}) {
  const [adultMode, setAdultMode] = useState(false);
  const [asking, setAsking] = useState(false);
  // 畫了「放進自己的隱藏版」才送 adult（沒勾送 false＝明說是一般版）；開關關著時沒畫，不送（照程式的預設，CMI-13）。
  const adultField = adult ? adultMode : undefined;
  const photoRef = useRef<HTMLInputElement>(null);
  const fullRef = useRef<HTMLInputElement>(null);
  const base = outfit.outfit === "base";
  // 「放全身照」只在那一版還沒有全身照時出現（有了就在那張的列上「換一張」）。
  const version = groups.find((g) => g.hidden === (adultField === true));
  // 加照片、全身照的結果行：在那幾顆鈕底下（UXC-9）。
  const cardAt = `card:${outfit.record}`;
  const hasFull = version?.full != null;

  const pick = async (e: ChangeEvent<HTMLInputElement>, type: "photo" | "full") => {
    const file = e.target.files?.[0] ?? null;
    e.target.value = "";
    if (!file) return;
    const bad = checkFile(file, "image");
    if (bad) {
      sender.fail(bad, cardAt);
      return;
    }
    await sender.send(
      file,
      { type, persona, outfit: outfit.outfit, adult: adultField },
      { at: cardAt },
    );
  };
  // 刪這套：處理中與失敗在刪的鈕旁邊；做完回總覽，結果在分頁最上面。
  const removeOutfit = async () => {
    const out = await sender.send(
      null,
      { type: "remove_outfit", persona, outfit: outfit.outfit },
      { doneText: T.doneRemoved, at: `remove:${outfit.record}`, doneAt: "" },
    );
    if (out.kind === "done") onRemoved();
    else if (out.kind !== "waiting") setAsking(false);
  };

  return (
    <li
      data-testid={`mine-outfit-${outfit.record}`}
      data-state={outfit.state}
      className="flex flex-col gap-3 rounded-xl bg-surface-2 p-3"
    >
      <div className="flex gap-3">
        <div className="w-20 shrink-0">
          <Thumb url={thumbOf(outfit)} />
        </div>
        <div className="min-w-0 flex-1">
          <p className="break-words font-semibold">{outfit.name}</p>
          <div className="mt-1 flex flex-wrap gap-1">
            {tagsOf(outfit, edit).map((t) => (
              <span key={t} className="rounded-full bg-surface px-2 py-0.5 text-xs text-muted">
                {t}
              </span>
            ))}
          </div>
          <div className="mt-1.5">
            <OutfitParts outfit={outfit} />
          </div>
        </div>
      </div>
      {adult ? (
        <Check
          label={T.adultMode}
          hint={T.adultHint}
          checked={adultMode}
          onChange={setAdultMode}
          testId={`mine-adult-${outfit.record}`}
        />
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="secondary"
          disabled={sender.busy}
          onClick={() => photoRef.current?.click()}
        >
          {T.addPhoto}
        </Button>
        {hasFull ? null : (
          <Button
            variant="secondary"
            disabled={sender.busy}
            onClick={() => fullRef.current?.click()}
          >
            {T.addFull}
          </Button>
        )}
      </div>
      <ResultLine sender={sender} at={cardAt} />
      <input
        ref={photoRef}
        data-testid={`mine-photo-input-${outfit.record}`}
        type="file"
        accept={ACCEPT_IMAGE}
        className="sr-only"
        tabIndex={-1}
        onChange={(e) => void pick(e, "photo")}
      />
      <input
        ref={fullRef}
        data-testid={`mine-full-input-${outfit.record}`}
        type="file"
        accept={ACCEPT_IMAGE}
        className="sr-only"
        tabIndex={-1}
        onChange={(e) => void pick(e, "full")}
      />
      <PhotoList
        record={outfit.record}
        name={outfit.name}
        groups={groups}
        target={{ persona, outfit: outfit.outfit, adultShown: adult }}
        sender={sender}
        locked={locked(outfit)}
      />
      {base ? null : (
        <CostEditor
          key={`${outfit.record}:${outfit.cost}`}
          persona={persona}
          outfit={outfit}
          sender={sender}
        />
      )}
      {base ? null : asking ? (
        <ConfirmLine
          testId="mine-remove-outfit-confirm"
          text={T.removeOutfitAsk}
          yes={T.removeYes}
          busy={sender.busy}
          onYes={() => void removeOutfit()}
          onNo={() => setAsking(false)}
        />
      ) : (
        <Button
          variant="ghost"
          className="self-start text-danger"
          disabled={sender.busy}
          onClick={() => setAsking(true)}
          data-testid={`mine-remove-outfit-${outfit.record}`}
        >
          {T.removeOutfit}
        </Button>
      )}
      <ResultLine sender={sender} at={`remove:${outfit.record}`} />
    </li>
  );
}
