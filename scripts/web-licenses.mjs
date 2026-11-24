#!/usr/bin/env node
// 看板網頁打包進去的第三方套件的授權聲明：產出 LICENSES/web-third-party.txt（進 git，發行檔的 LICENSES/ 也帶它，scripts/dist.sh）。
//   node scripts/web-licenses.mjs           重產 LICENSES/web-third-party.txt
//   node scripts/web-licenses.mjs --check   不寫檔，跟重產的結果比；不一樣就說怎麼修、結束碼 1（make check、make web-ci、make web-ci-full 都跑）
//   node scripts/web-licenses.mjs --list    印「名稱 版本 授權」（除錯用）
// 收哪些：看板（web/apps/board）與它用到的工作區套件（web/packages/*）的 dependencies（工作區套件另加 peerDependencies），
//   順著 node_modules 往下收間接相依（dependencies、有裝的 optionalDependencies；peerDependencies 一定由上層的 dependencies 提供，不另外跟）。
//   只有型別宣告的 @types/* 不會打包，不收。官方套件的頁面（packages/official/*/pages）只 import 外殼提供的 react、@evergenie/ui、
//   @evergenie/sdk-web（internal/base/app/board/bundle.go 的 sharedImports），沒有自己的第三方套件。
//   另外收打包工具放進產物的部分（BUILD_TOOLS）：vite 的 modulepreload polyfill 與 CommonJS 轉換小工具、tailwindcss 產生的 CSS。
//   收的範圍比實際打包的大一點（tree-shaking 掉的也算），寧多不少。
// 授權全文讀套件資料夾最上層的 LICENSE*／LICENCE*／COPYING*／NOTICE*；套件沒附檔就照實寫 package.json 的授權欄位。
// 輸出只看相依的內容（排序固定、不含日期與路徑），同一組相依產出一字不差，--check 才比得出來。
// 要先裝好 web/node_modules（scripts/web-deps.sh；make 的 _web 會先跑）。
// 結束碼：0 一致／寫好了；1 --check 對不上；2 用法錯誤、node_modules 不完整或讀不了。
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const web = path.join(root, "web");
const OUT = path.join(root, "LICENSES", "web-third-party.txt");
const OUT_REL = "LICENSES/web-third-party.txt";
const APP = path.join(web, "apps", "board");

// 打包工具放進產物的程式碼：sections 是授權檔裡要收的段（markdown 標題的正規表示式；找不到就算失敗，升版改了格式才會發現）
const BUILD_TOOLS = [
  {
    name: "vite",
    why: "打包工具；產物裡的 modulepreload polyfill 是 Vite 的，CommonJS 轉換的小工具來自 Vite 內附的 @rollup/plugin-commonjs（只收這兩段）",
    sections: [/^# Vite core license\b/, /^## .*@rollup\/plugin-commonjs\b/],
  },
  { name: "tailwindcss", why: "產物的 CSS 含 tailwindcss 的 preflight 與 theme" },
];

const LICENSE_FILE = /^(licen[cs]e|copying|notice)([.\-_].*)?$/i;

function die(msg, code = 2) {
  console.error(`web-licenses：${msg}`);
  process.exit(code);
}

const readJSON = (p) => JSON.parse(fs.readFileSync(p, "utf8"));

// Node 的找法：從套件的真實位置往上找 node_modules/<name>（pnpm 的相依就放在同層的 node_modules）
function resolveDir(name, fromDir) {
  let dir = fromDir;
  for (;;) {
    if (path.basename(dir) !== "node_modules") {
      const p = path.join(dir, "node_modules", name, "package.json");
      if (fs.existsSync(p)) return fs.realpathSync(path.dirname(p));
    }
    const up = path.dirname(dir);
    if (up === dir || !up.startsWith(web)) return null;
    dir = up;
  }
}

function licenseOf(pkg) {
  if (typeof pkg.license === "string") return pkg.license;
  if (pkg.license && typeof pkg.license.type === "string") return pkg.license.type;
  if (Array.isArray(pkg.licenses)) return pkg.licenses.map((l) => l.type ?? l).join(" OR ");
  return "（package.json 沒寫）";
}

function sourceOf(pkg) {
  let u = typeof pkg.repository === "string" ? pkg.repository : pkg.repository?.url;
  if (u) {
    u = u
      .replace(/^git\+/, "")
      .replace(/\.git$/, "")
      .replace(/^git:\/\//, "https://");
    if (/^[\w.-]+\/[\w.-]+$/.test(u)) u = `https://github.com/${u}`;
    u = u.replace(/^github:/, "https://github.com/");
    return u;
  }
  return pkg.homepage ?? "";
}

const clean = (s) => `${s.replace(/\r\n?/g, "\n").replace(/\s+$/, "")}\n`;

// 只取 markdown 授權檔裡標題對得上的段（到下一個同級或更上層的標題為止）
function pickSections(text, patterns, label) {
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  const out = [];
  for (const re of patterns) {
    const start = lines.findIndex((l) => re.test(l));
    if (start < 0)
      die(
        `${label} 的授權檔裡找不到「${re.source}」那一段（升版改了格式？更新 scripts/web-licenses.mjs 的 BUILD_TOOLS）`,
      );
    const level = lines[start].match(/^#+/)[0].length;
    let end = start + 1;
    while (
      end < lines.length &&
      !(lines[end].match(/^#+ /) && lines[end].match(/^#+/)[0].length <= level)
    )
      end++;
    out.push(
      lines
        .slice(start, end)
        .join("\n")
        .replace(/\n-{3,}\s*$/, "")
        .trimEnd(),
    );
  }
  return `${out.join("\n\n")}\n`;
}

function licenseTexts(dir, entry) {
  const files = fs
    .readdirSync(dir, { withFileTypes: true })
    .filter((e) => e.isFile() && LICENSE_FILE.test(e.name))
    .map((e) => e.name)
    .sort();
  return files.map((f) => {
    let text = fs.readFileSync(path.join(dir, f), "utf8");
    if (entry?.sections) text = pickSections(text, entry.sections, entry.name);
    return { file: f, text: clean(text) };
  });
}

function collect() {
  if (!fs.existsSync(path.join(web, "node_modules")))
    die("web/node_modules 不在：先跑 scripts/web-deps.sh");
  const workspace = new Map(); // 名稱 → 資料夾
  for (const group of ["apps", "packages"]) {
    for (const d of fs.readdirSync(path.join(web, group))) {
      const p = path.join(web, group, d, "package.json");
      if (fs.existsSync(p)) workspace.set(readJSON(p).name, path.dirname(p));
    }
  }
  const found = new Map(); // 名稱@版本 → 項目
  const seenDirs = new Set();
  const seenWs = new Set();
  const visit = (dir, extra) => {
    if (seenDirs.has(dir)) return;
    seenDirs.add(dir);
    const pkg = readJSON(path.join(dir, "package.json"));
    const key = `${pkg.name}@${pkg.version}`;
    if (!found.has(key) && !pkg.name.startsWith("@types/")) {
      found.set(key, {
        name: pkg.name,
        version: pkg.version,
        license: licenseOf(pkg),
        source: sourceOf(pkg),
        why: extra?.why ?? "",
        texts: licenseTexts(dir, extra),
        author: typeof pkg.author === "string" ? pkg.author : (pkg.author?.name ?? ""),
      });
    }
    if (extra) return; // 打包工具只收它自己（它的相依不進產物）
    const deps = pkg.dependencies ?? {};
    const optional = pkg.optionalDependencies ?? {};
    for (const name of Object.keys({ ...deps, ...optional })) {
      const d = resolveDir(name, dir);
      if (!d) {
        if (name in optional) continue;
        die(`${pkg.name} 要的 ${name} 在 node_modules 裡找不到：先跑 scripts/web-deps.sh`);
      }
      visit(d);
    }
  };
  const visitWorkspace = (name) => {
    if (seenWs.has(name)) return;
    seenWs.add(name);
    const dir = workspace.get(name);
    const pkg = readJSON(path.join(dir, "package.json"));
    const deps = {
      ...pkg.dependencies,
      ...(name === "@evergenie/board" ? {} : pkg.peerDependencies),
    };
    for (const [dep, spec] of Object.entries(deps)) {
      if (String(spec).startsWith("workspace:") || workspace.has(dep)) {
        if (!workspace.has(dep)) die(`${name} 要的工作區套件 ${dep} 不在 web/ 裡`);
        visitWorkspace(dep);
        continue;
      }
      const d = resolveDir(dep, dir) ?? resolveDir(dep, APP);
      if (!d) die(`${name} 要的 ${dep} 在 node_modules 裡找不到：先跑 scripts/web-deps.sh`);
      visit(d);
    }
  };
  if (!workspace.has("@evergenie/board")) die("找不到 web/apps/board/package.json");
  visitWorkspace("@evergenie/board");
  for (const t of BUILD_TOOLS) {
    const d = resolveDir(t.name, APP);
    if (!d) die(`打包工具 ${t.name} 在 node_modules 裡找不到：先跑 scripts/web-deps.sh`);
    seenDirs.delete(d);
    visit(d, t);
  }
  const cmp = (a, b) => (a < b ? -1 : a > b ? 1 : 0);
  return [...found.values()].sort((a, b) => cmp(a.name, b.name) || cmp(a.version, b.version));
}

function render(items) {
  const rule = "-".repeat(72);
  const lines = [
    "看板網頁用到的第三方套件與授權",
    "",
    "看板的網頁（internal/base/app/board/dist/，編進核心執行檔）打包了下面這些別人寫的套件；每個套件的授權全文在後面。",
    `這份由 scripts/web-licenses.mjs 從 web/ 的相依產生，不要手改；相依變了就重跑 \`node scripts/web-licenses.mjs\`（make web-ci 會檢查）。`,
    "",
    `共 ${items.length} 個（名稱 版本 授權）：`,
    ...items.map((i) => `  ${i.name} ${i.version} ${i.license}`),
  ];
  for (const i of items) {
    lines.push("", rule, `${i.name} ${i.version}`, `授權：${i.license}`);
    if (i.source) lines.push(`來源：${i.source}`);
    if (i.why) lines.push(`為什麼收：${i.why}`);
    lines.push("");
    if (i.texts.length === 0) {
      lines.push(
        `（套件沒有附授權全文；package.json 寫的授權是 ${i.license}${i.author ? `，作者 ${i.author}` : ""}）`,
      );
      continue;
    }
    i.texts.forEach((t, n) => {
      if (n > 0) lines.push("");
      if (i.texts.length > 1) lines.push(`[${t.file}]`);
      lines.push(t.text.replace(/\n$/, ""));
    });
  }
  return `${lines.join("\n")}\n`;
}

const args = process.argv.slice(2);
const mode = args[0] ?? "write";
if (args.length > 1 || !["write", "--check", "--list"].includes(mode)) {
  die("用法：node scripts/web-licenses.mjs [--check | --list]");
}
const items = collect();
if (mode === "--list") {
  for (const i of items) console.log(`${i.name} ${i.version} ${i.license}`);
  process.exit(0);
}
const text = render(items);
if (mode === "--check") {
  const old = fs.existsSync(OUT) ? fs.readFileSync(OUT, "utf8") : "";
  if (old === text) process.exit(0);
  const names = (s) => new Set([...s.matchAll(/^ {2}(\S+ \S+) /gm)].map((m) => m[1]));
  const was = names(old);
  const now = new Set(items.map((i) => `${i.name} ${i.version}`));
  const added = [...now].filter((k) => !was.has(k));
  const gone = [...was].filter((k) => !now.has(k));
  console.error(
    `web-licenses：${OUT_REL} 跟 web/ 現在的相依對不上，請重跑 node scripts/web-licenses.mjs 並 commit。`,
  );
  if (added.length) console.error(`  少了：${added.join("、")}`);
  if (gone.length) console.error(`  多了：${gone.join("、")}`);
  if (!added.length && !gone.length)
    console.error("  （套件一樣，內容不一樣：授權文字或欄位變了）");
  process.exit(1);
}
fs.mkdirSync(path.dirname(OUT), { recursive: true });
fs.writeFileSync(OUT, text);
console.log(`web-licenses：寫好 ${OUT_REL}（${items.length} 個套件）`);
