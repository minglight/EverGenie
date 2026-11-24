#!/usr/bin/env node
// 看板的輸入指紋（T2 第 2 條）：sha256(所有「會影響打包結果」的輸入)；用工作目錄裡的內容（含未提交的改動）。
//   node scripts/web-hash.mjs board          印指紋
//   node scripts/web-hash.mjs board --list   印輸入清單（除錯、測試用）
//   node scripts/web-hash.mjs stamp biome|typecheck|vitest   前端檢查的「通過紀錄」指紋（scripts/web-stamp.sh 用）
//
// 輸入清單照實際的 import 與 Tailwind 掃描範圍確認（2026-10-27）：
//  共同：web/package.json、pnpm-lock.yaml、pnpm-workspace.yaml、tsconfig.base.json、web/packages/**（ui、sdk-web，全部內容）、
//        該 app 的 index.html、package.json、tsconfig*.json、vite.config.ts、public/**、src/**（不含 *.spec.*）。
//  官方套件頁面（packages/official/*/pages）不會被 import 進 bundle（執行時才由核心提供），
//        但看板的 index.css 有 `@source "…/packages/official/**/pages/**/*.tsx"`：Tailwind 只掃 class 名稱。
// 「只被 Tailwind 掃」的目錄不用整份內容當指紋：Tailwind 只會從裡面取出 class 候選字，所以用「去掉整行註解之後、
// 以空白與引號切開的不重複字串集合」當指紋——字串集合沒變，Tailwind 的輸出就不會變；改程式邏輯但沒新增字串、
// 或只改註解，都不會重打包。
import { createHash } from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const web = path.join(root, "web");
const rel = (p) => path.relative(root, p).split(path.sep).join("/");

const SKIP_DIRS = new Set(["node_modules", "dist", ".git", "test-results", "playwright-report", ".cache"]);
const isSpec = (f) => /\.spec\.[cm]?[jt]sx?$/.test(f);
const SCAN_EXT = new Set([".ts", ".tsx", ".js", ".jsx", ".mjs", ".html", ".css", ".md", ".json", ".svg", ".vue"]);

function walk(dir, { specs = false, exts = null, skipDirs = SKIP_DIRS } = {}) {
  const out = [];
  if (!fs.existsSync(dir)) return out;
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) {
      if (skipDirs.has(e.name)) continue;
      out.push(...walk(p, { specs, exts, skipDirs }));
    } else if (e.isFile()) {
      if (!specs && isSpec(e.name)) continue;
      if (exts && !exts.has(path.extname(e.name))) continue;
      out.push(p);
    }
  }
  return out;
}

function appFiles(app) {
  const d = path.join(web, "apps", app);
  const files = [];
  for (const f of ["index.html", "package.json", "vite.config.ts"]) files.push(path.join(d, f));
  for (const e of fs.readdirSync(d)) if (/^tsconfig.*\.json$/.test(e)) files.push(path.join(d, e));
  files.push(...walk(path.join(d, "public")), ...walk(path.join(d, "src")));
  return files.filter((f) => fs.existsSync(f));
}

const sha = (buf) => createHash("sha256").update(buf).digest("hex");

function tokenSet(files) {
  const toks = new Set();
  for (const f of files) {
    if (!SCAN_EXT.has(path.extname(f))) continue;
    for (const line of fs.readFileSync(f, "utf8").split("\n")) {
      if (/^\s*(\/\/|\/\*|\*)/.test(line)) continue; // 整行註解
      for (const t of line.split(/[\s"'`]+/)) if (t) toks.add(t);
    }
  }
  return [...toks].sort();
}

function inputs(app) {
  const files = new Set();
  const add = (list) => list.forEach((f) => files.add(f));
  add(["package.json", "pnpm-lock.yaml", "pnpm-workspace.yaml", "tsconfig.base.json"].map((f) => path.join(web, f)));
  add(walk(path.join(web, "packages")));
  add(appFiles(app));
  const scans = []; // [名稱, 檔案清單]
  scans.push(["tailwind-scan:packages/official/**/pages/**/*.tsx", officialPageFiles()]);
  return { files: [...files].filter((f) => fs.existsSync(f)).sort(), scans };
}

function officialPageFiles() {
  const out = [];
  const base = path.join(root, "packages/official");
  if (!fs.existsSync(base)) return out;
  for (const pkg of fs.readdirSync(base)) {
    out.push(...walk(path.join(base, pkg, "pages"), { specs: true }).filter((f) => f.endsWith(".tsx")));
  }
  return out.sort();
}

// 檢查的指紋：所有可能影響這項檢查結果的檔案（比打包寬：含 spec，因為 vitest／tsc／biome 都會讀它們）。
//   biome：web/** 全部（含 e2e）＋ biome.json；
//   typecheck、vitest：web/** 除了 e2e ＋ packages/official/*/pages/**（tsconfig.packages.json、vitest.config.ts 都會讀）。
function stampHash(name) {
  if (!["biome", "typecheck", "vitest"].includes(name)) {
    console.error("用法：node scripts/web-hash.mjs stamp biome|typecheck|vitest");
    process.exit(2);
  }
  const skip = new Set(SKIP_DIRS);
  if (name !== "biome") skip.add("e2e");
  const files = walk(web, { specs: true, skipDirs: skip });
  if (name !== "biome") {
    const base = path.join(root, "packages/official");
    if (fs.existsSync(base)) {
      for (const pkg of fs.readdirSync(base)) files.push(...walk(path.join(base, pkg, "pages"), { specs: true }));
    }
  }
  files.sort();
  const h = createHash("sha256");
  h.update(`web-stamp v1 ${name} node ${process.version}\n`);
  for (const f of files) h.update(`${rel(f)}\0${sha(fs.readFileSync(f))}\n`);
  return h.digest("hex");
}

if (process.argv[2] === "stamp") {
  console.log(stampHash(process.argv[3]));
  process.exit(0);
}

const app = process.argv[2];
if (app !== "board") {
  console.error("用法：node scripts/web-hash.mjs board [--list]");
  process.exit(2);
}
const { files, scans } = inputs(app);
if (process.argv.includes("--list")) {
  for (const f of files) console.log(rel(f));
  for (const [name, fl] of scans) console.log(`${name}（${fl.length} 檔，字串集合）`);
  process.exit(0);
}
const h = createHash("sha256");
h.update(`web-hash v1 ${app}\n`);
for (const f of files) h.update(`${rel(f)}\0${sha(fs.readFileSync(f))}\n`);
for (const [name, fl] of scans) h.update(`${name}\0${sha(tokenSet(fl).join("\n"))}\n`);
console.log(h.digest("hex"));
