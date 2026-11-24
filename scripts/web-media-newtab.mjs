#!/usr/bin/env node
// FEN-30：頁面不用新分頁開圖片或影片，一律用 @evergenie/ui 的 MediaViewer（看大圖）。
// 加到主畫面的看板沒有分頁列，開了新分頁就退不回來（作者 2026-11-03、2026-11-14「圖案關閉應該是大家都要有」）。
//   node scripts/web-media-newtab.mjs             掃看板、共用元件與官方套件的頁面，有違規就列出來、結束碼 1
//   node scripts/web-media-newtab.mjs <目錄>…      掃指定的目錄（測試用；主人自己的套件也可以這樣掃）
//   node scripts/web-media-newtab.mjs --self-test  用內建的樣本確認抓得到、放得過（make 的檢查每次都先跑這個）
// 靜態看不出一個網址是不是圖片，所以反過來管：**每一個開新分頁的地方都要寫明理由**（測試檔 *.spec.* 不看）。
//   1. `target="_blank"`（任何標籤）：前 6 行內或同一行要有 `hg-allow-new-tab: <原因>`（例：站外連結、下載用的檔案）。
//      圖片與影片沒有合理的原因——改用 MediaViewer／ImageViewer。
//   2. `window.open(`／`globalThis.open(`：同上；頁面一般不自己開視窗，站外連結用 <a target="_blank">。
// make web-ci、make check（scripts/web-check-changed.sh）與 make web-ci-full 都會跑（全掃，不到一秒）。
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const EXT = /\.(tsx|jsx|ts|js|mjs)$/;
const SKIP = /\.(spec|test)\.[a-z]+$/;
const ALLOW = /hg-allow-new-tab:\s*\S/;
const BLANK = /target\s*=\s*(\{\s*)?["'`]_blank["'`]/;
const OPEN = /\b(window|globalThis)\s*\.\s*open\s*\(/;
const COMMENT = /^\s*(\/\/|\*|\/\*|\{\/\*)/;
const LOOK_BACK = 6;

/** 一個檔的違規：[{ line, msg }]。 */
export function scan(text) {
  const lines = text.split("\n");
  const out = [];
  lines.forEach((line, i) => {
    if (COMMENT.test(line)) return;
    const blank = BLANK.test(line);
    const open = OPEN.test(line);
    if (!blank && !open) return;
    const near = lines.slice(Math.max(0, i - LOOK_BACK), i + 1);
    if (near.some((l) => ALLOW.test(l))) return;
    out.push({
      line: i + 1,
      msg: blank
        ? 'target="_blank"：圖片與影片改用 MediaViewer／ImageViewer；站外連結、下載用的檔案在上面寫 hg-allow-new-tab: <原因>'
        : "開新視窗（open）：圖片與影片改用 MediaViewer；真的要開就在上面寫 hg-allow-new-tab: <原因>",
    });
  });
  return out;
}

function selfTest() {
  const bad = [
    '<a href={url} target="_blank"><img src={url} /></a>',
    '<a\n  href={path}\n  target="_blank"\n>{name}</a>',
    "<Link to={u} target={'_blank'} />",
    "<button onClick={() => window.open(url)}>開</button>",
    "globalThis.open(url);",
  ];
  const good = [
    '<a href={url}>站內</a>',
    '{/* hg-allow-new-tab: 站外連結 */}\n<a\n  href={href}\n  target="_blank"\n  rel="noopener noreferrer"\n>x</a>',
    '// hg-allow-new-tab: 下載用\nwindow.open(url);',
    '// 註解裡提到 target="_blank" 與 window.open( 不算',
    "<MediaViewer items={items} index={at} onIndex={setAt} />",
  ];
  const fails = [];
  for (const s of bad) if (scan(s).length === 0) fails.push(`該紅沒紅：${s}`);
  for (const s of good) if (scan(s).length > 0) fails.push(`不該紅卻紅：${s}`);
  // 理由離太遠不算（不能在檔頭寫一次放行整個檔）。
  const far = `// hg-allow-new-tab: 太遠\n${"\n".repeat(LOOK_BACK + 1)}<a href={u} target="_blank">x</a>`;
  if (scan(far).length === 0) fails.push("理由離太遠也放行了");
  if (fails.length > 0) {
    console.error("FEN-30 的檢查自己壞了：");
    for (const f of fails) console.error(`  ${f}`);
    process.exit(1);
  }
}

function walk(dir) {
  const out = [];
  if (!fs.existsSync(dir)) return out;
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    if (e.name === "node_modules" || e.name === "gen" || e.name === "dist") continue;
    const p = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...walk(p));
    else if (EXT.test(e.name) && !SKIP.test(e.name)) out.push(p);
  }
  return out;
}

selfTest();
const args = process.argv.slice(2).filter((a) => a !== "--self-test");
if (process.argv.includes("--self-test") && args.length === 0) {
  console.log("FEN-30：檢查的內建樣本都對");
  process.exit(0);
}
const dirs =
  args.length > 0
    ? args.map((d) => path.resolve(d))
    : [
        path.join(root, "web/apps/board/src"),
        path.join(root, "web/packages/ui/src"),
        path.join(root, "web/packages/sdk-web/src"),
        path.join(root, "packages/official"),
      ];
const problems = [];
for (const file of dirs.flatMap(walk)) {
  for (const p of scan(fs.readFileSync(file, "utf8"))) {
    problems.push(`${path.relative(root, file)}:${p.line}：${p.msg}`);
  }
}
if (problems.length > 0) {
  console.error("FEN-30 違規：頁面不用新分頁開圖片或影片（加到主畫面的看板沒有分頁列，開了退不回來）。");
  console.error("改用 @evergenie/ui 的 MediaViewer（或一張縮圖用 ImageViewer）；見 ai_docs/conventions/frontend.md 的 FEN-30。");
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log("FEN-30：開新分頁的地方都寫了理由，沒有用新分頁開圖片或影片的頁面");
