import type { SessionCommand } from "@evergenie/sdk-web";
import { t } from "../../i18n/zh-TW";
import { EFFORT_LEVELS, effortFromWord, effortWord, MODEL_CHOICES } from "../../lib/modelChoices";

// 輸入框的「/」指令（WEB-83、D-45）：看板只認這四個，送 session.command，不當訊息送。
// 選單的列是從輸入框的字算出來的：「/」＋開頭幾個字＝第一層（指令）；「/model 」之後＝第二層（選項）。

export interface SlashSpec {
  name: SessionCommand;
  /** 要選一個參數的（第二層）；沒有就是直接執行。 */
  options?: readonly string[];
  /** 執行前要不要先確認（清掉對話、改帳號的預設）。 */
  confirm: boolean;
}

export const MODEL_OPTIONS = MODEL_CHOICES;
export const EFFORT_OPTIONS = [...EFFORT_LEVELS, "auto"] as const;

export const SLASH_COMMANDS: readonly SlashSpec[] = [
  { name: "clear", confirm: true },
  { name: "compact", confirm: false },
  { name: "model", options: MODEL_OPTIONS, confirm: false },
  { name: "effort", options: EFFORT_OPTIONS, confirm: true },
];

export interface SlashItem {
  /** 選單上的大字：/clear、opus… */
  label: string;
  hint: string;
  spec: SlashSpec;
  /** 第二層才有：選了就是這個參數。 */
  arg?: string;
}

/** 畫面上的參數（力度的 xhigh 寫成 extra）。 */
function argWord(cmd: SessionCommand, arg: string): string {
  return cmd === "effort" ? effortWord(arg) : arg;
}

export function commandLabel(cmd: SessionCommand, arg?: string): string {
  return arg ? `/${cmd} ${argWord(cmd, arg)}` : `/${cmd}`;
}

function specOf(name: string): SlashSpec | undefined {
  return SLASH_COMMANDS.find((s) => s.name === name);
}

/** 輸入框的字是不是在打指令：開頭就是「/」（前面有空白就是一般訊息）、只有一行。 */
export function isSlashText(raw: string): boolean {
  return raw.startsWith("/") && !raw.includes("\n");
}

/** 選單要列的東西；不是在打指令回 null（不開選單），打了但沒有符合的回空陣列。 */
export function slashItems(raw: string): SlashItem[] | null {
  if (!isSlashText(raw)) return null;
  const body = raw.slice(1);
  const space = body.indexOf(" ");
  if (space < 0) {
    const q = body.toLowerCase();
    return SLASH_COMMANDS.filter((s) => s.name.startsWith(q)).map((spec) => ({
      label: commandLabel(spec.name),
      hint: t(`slash.${spec.name}` as never),
      spec,
    }));
  }
  const spec = specOf(body.slice(0, space).toLowerCase());
  if (!spec?.options) return [];
  const q = body
    .slice(space + 1)
    .trim()
    .toLowerCase();
  return spec.options
    .filter((o) => o.startsWith(q) || argWord(spec.name, o).startsWith(q))
    .map((arg) => ({
      label: argWord(spec.name, arg),
      hint: t(`slash.${spec.name}.${arg}` as never),
      spec,
      arg,
    }));
}

export type SlashParse =
  | { kind: "message" }
  | { kind: "unknown" }
  | { kind: "needArg"; spec: SlashSpec }
  | { kind: "command"; spec: SlashSpec; arg: string };

/**
 * 按送出時：輸入框的字（沒去掉前面空白的原字）是不是指令。有附件就是一般訊息；
 * 「/model opus」這種打完整的直接是指令；不認得的 /xxx 不送（提示看板支援哪些）。
 * 不認得的開頭、後面還有字（「/tmp 為什麼滿了」）是一般訊息。
 */
export function parseSlash(raw: string, attachments: number): SlashParse {
  if (attachments > 0 || !isSlashText(raw)) return { kind: "message" };
  const [head = "", ...rest] = raw.slice(1).trim().split(/\s+/);
  const spec = specOf(head.toLowerCase());
  if (!spec) return rest.length > 0 ? { kind: "message" } : { kind: "unknown" };
  const typed = (rest[0] ?? "").toLowerCase();
  const arg = spec.name === "effort" ? effortFromWord(typed) : typed;
  if (rest.length > 1) return { kind: "unknown" };
  if (!spec.options) return arg ? { kind: "unknown" } : { kind: "command", spec, arg: "" };
  if (!arg) return { kind: "needArg", spec };
  if (!spec.options.includes(arg)) return { kind: "unknown" };
  return { kind: "command", spec, arg };
}
