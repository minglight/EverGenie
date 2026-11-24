#!/usr/bin/env node
// FEN-27：看板的頁面資料夾之間不互相 import（一組頁面一個資料夾，彼此不互相依賴；法律第 8 條）。
//   node scripts/web-page-imports.mjs            掃 web/apps/board/src，有違規就列出來、結束碼 1
//   node scripts/web-page-imports.mjs <src 目錄>  掃指定的目錄（測試用）
// 規則：
//   1. pages/<A>/ 底下的檔（含測試）不 import pages/<B>/（B ≠ A）。兩個以上頁面要用的元件放 components/、
//      純函式放 lib/、查詢放 data/。
//   2. 共用層 components/、lib/、data/（含測試）不 import pages/（共用的東西不能反過來依賴某個頁面）。
//      掛頁面的 router.tsx、外殼 shell/、套件頁的宿主 pkg/ 不在此限。
// 看的是相對路徑的 import／export from／import()／vi.mock() 解析後落在哪個資料夾；套件（`react`、`@evergenie/ui`）不看。
// make web-ci、make check（scripts/web-check-changed.sh）與 make web-ci-full 都會跑（全掃，不到一秒）。
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const src = path.resolve(process.argv[2] ?? path.join(root, "web/apps/board/src"));
const SHARED = new Set(["components", "lib", "data"]);
const EXT = /\.(ts|tsx|js|jsx|mjs)$/;

function walk(dir) {
  const out = [];
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    if (e.name === "node_modules" || e.name === "gen") continue;
    const p = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...walk(p));
    else if (EXT.test(e.name)) out.push(p);
  }
  return out;
}

// 檔案落在 src 底下的哪一區：{ top: "pages", page: "tasks" }、{ top: "components" }…；不在 src 底下回 null。
function area(file) {
  const rel = path.relative(src, file).split(path.sep);
  if (rel[0] === "..") return null;
  if (rel.length < 2) return { top: "" }; // router.tsx、main.tsx 這類直接在 src/ 底下的檔
  return rel[0] === "pages" ? { top: "pages", page: rel.length > 2 ? rel[1] : "" } : { top: rel[0] };
}

const SPEC = /(?:\bfrom\s*|\bimport\s*\(\s*|\bimport\s+|\bvi\.(?:mock|doMock|importActual)\s*\(\s*)["'](\.{1,2}\/[^"']*)["']/g;

const problems = [];
for (const file of walk(src)) {
  const from = area(file);
  if (!from || !(from.top === "pages" || SHARED.has(from.top))) continue;
  const text = fs.readFileSync(file, "utf8");
  const lines = text.split("\n");
  lines.forEach((line, i) => {
    for (const m of line.matchAll(SPEC)) {
      const to = area(path.resolve(path.dirname(file), m[1]));
      if (!to || to.top !== "pages") continue;
      const rel = path.relative(root, file);
      if (from.top === "pages" && to.page !== from.page) {
        problems.push(`${rel}:${i + 1}：pages/${from.page}/ import 了 pages/${to.page}/（${m[1]}）`);
      } else if (SHARED.has(from.top)) {
        problems.push(`${rel}:${i + 1}：共用層 ${from.top}/ import 了 pages/（${m[1]}）`);
      }
    }
  });
}

if (problems.length > 0) {
  console.error("FEN-27 違規：頁面資料夾之間不互相 import，共用層不 import 頁面。");
  console.error("兩個以上頁面要用的：元件搬到 components/、純函式搬到 lib/、查詢搬到 data/（見 ai_docs/conventions/frontend.md）。");
  for (const p of problems) console.error(`  ${p}`);
  process.exit(1);
}
console.log("FEN-27：頁面資料夾之間沒有互相 import");
