import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import type { APIRequestContext, Page } from "@playwright/test";
import { expect, type HgServer, test } from "./fixtures";

// 官方角色包帶衣服到家裡（DEV-157，PRS-16、PRS-28）：測試自己做一份角色包（跟 personas pack build 同一個格式：m/<版號>.json、f/<sha256><副檔名>），
// 帶 helper 的卡（跟測試家那張一模一樣，所以不算改過）、兩套衣服的圖與它們的 closet.outfit 紀錄（一套送的 cost 0、一套要兌換的）；
// 用 evergenie personas pack sync（EVERGENIE_PERSONAS_PACK_POINTER 指到這份包）請服務同步，衣櫃的程式看到官方紀錄寫進來就重算。
// 同一個 worker 的家是共用的：衣服編號每次隨機，結束時把自己造的紀錄封存、圖刪掉（衣櫃的 closet.spec 也只看 builder）。

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "../../../..");
const fx = path.join(here, "fixtures");
const IMG = ["outfit-a.png", "outfit-b.png"].map((n) => path.join(fx, "closet", n));
const rand = () =>
  Math.random()
    .toString(36)
    .slice(2, 8)
    .replace(/[^a-z0-9]/g, "x");

const tile = (page: Page, record: string) =>
  page.locator(`[data-testid="outfit-tile"][data-outfit="${record}"]`);

/** 在 dir 做一份角色包，回指標檔路徑。files 是 {邏輯路徑: 內容}。 */
function writePack(
  dir: string,
  version: string,
  files: Record<string, Buffer>,
  records: { kind: string; id: string; fields: Record<string, unknown> }[],
): string {
  mkdirSync(path.join(dir, "m"), { recursive: true });
  mkdirSync(path.join(dir, "f"), { recursive: true });
  const list = Object.entries(files).map(([p, body]) => {
    const sha = createHash("sha256").update(body).digest("hex");
    writeFileSync(path.join(dir, "f", sha + path.extname(p)), body);
    return { path: p, sha256: sha, size: body.length };
  });
  const manifest = Buffer.from(
    JSON.stringify({ kind: "evergenie.personas", schema: 1, version, files: list, records }),
  );
  writeFileSync(path.join(dir, "m", `${version}.json`), manifest);
  const sum = createHash("sha256").update(manifest).digest("hex");
  const ptr = path.join(dir, "pack.toml");
  writeFileSync(
    ptr,
    `version = "${version}"\nurl = "file://${dir}/m/${version}.json"\nsha256 = "${sum}"\n`,
  );
  return ptr;
}

function vault(request: APIRequestContext, base: string) {
  const post = async (route: string, data: unknown) => {
    const res = await request.post(`${base}/api/${route}`, { data });
    const json = (await res.json().catch(() => ({}))) as Record<string, unknown>;
    expect(res.status(), `${route} ${JSON.stringify(json)}`).toBeLessThan(300);
    return json;
  };
  const get = async (kind: string, id: string) => {
    const res = await request.post(`${base}/api/vault/get`, { data: { kind, id } });
    return res.ok()
      ? ((await res.json()) as { record: { fields: Record<string, unknown> } }).record
      : null;
  };
  return { post, get };
}

function syncPack(server: HgServer, pointer: string): Record<string, unknown> {
  const out = execFileSync(
    path.join(repo, "bin", "evergenie"),
    ["personas", "pack", "sync", "--home", server.home, "--json"],
    {
      env: {
        ...process.env,
        EVERGENIE_HOME: server.home,
        EVERGENIE_API_SOCKET: path.join(server.home, "runtime", "sockets", "api.sock"),
        EVERGENIE_AGENT: "",
        EVERGENIE_PERSONAS_PACK_POINTER: pointer,
      },
      encoding: "utf8",
    },
  );
  return JSON.parse(out) as Record<string, unknown>;
}

// Guards: PRS-28, PRS-16
test("TestPackSyncFillsShowroomWithOutfitsAndSilhouettes @smoke", async ({
  page,
  request,
  baseURL,
  hgServer,
}) => {
  const base = baseURL ?? "";
  const api = vault(request, base);
  const free = rand();
  const locked = rand();
  const packDir = path.join(hgServer.tmp, `pack-${free}`);
  const avatars = path.join(hgServer.home, "personas", "avatars");
  const outfit = (id: string, cost: number) => ({
    kind: "closet.outfit",
    id: `helper-${id}`,
    fields: {
      persona: "helper",
      outfit: id,
      name: `包裡的衣服${id.toUpperCase()}`,
      cost,
      state: "active",
      source: "official",
    },
  });
  try {
    // 版號每次都比上一次新（同一個家重跑時不會被當成舊版、不退版）。
    const version = `2026.11.14.${Date.now() % 100_000_000}`;
    const ptr = writePack(
      packDir,
      version,
      {
        "cards/helper.md": readFileSync(path.join(hgServer.home, "personas", "helper.md")),
        [`avatars/helper@${free}.png`]: readFileSync(IMG[0] as string),
        [`avatars/helper@${locked}.png`]: readFileSync(IMG[1] as string),
      },
      [outfit(free, 0), outfit(locked, 2)],
    );
    const res = syncPack(hgServer, ptr);
    expect(res.status, JSON.stringify(res)).toBe("synced");
    expect((res.records_added as string[]).sort()).toEqual(
      [`closet.outfit/helper-${free}`, `closet.outfit/helper-${locked}`].sort(),
    );

    // 衣櫃的程式重算好 helper 的檢視：兩套、送的那套擁有。
    await expect
      .poll(
        async () => {
          const p = await api.get("closet.person", "helper");
          const list = (p?.fields.outfits as { record: string; owned: boolean }[]) ?? [];
          return list
            .filter((o) => o.record.endsWith(free) || o.record.endsWith(locked))
            .map((o) => `${o.record}:${o.owned}`)
            .sort();
        },
        { timeout: 20_000 },
      )
      .toEqual([`helper-${free}:true`, `helper-${locked}:false`].sort());

    const seen: string[] = [];
    page.on("request", (r) => seen.push(new URL(r.url()).pathname));
    await page.goto("/p/closet/showroom?persona=helper");
    await expect(page.getByTestId("showroom")).toBeVisible();
    // 送的那套：真圖。
    await expect(tile(page, `helper-${free}`).getByTestId("outfit-img")).toHaveAttribute(
      "src",
      new RegExp(`/pkg-files/closet/o/helper/${free}/1\\.png`),
    );
    // 沒兌換那套：剪影加鎖頭，真圖拿不到。
    const t = tile(page, `helper-${locked}`);
    await expect(t.getByTestId("outfit-img")).toHaveAttribute(
      "src",
      new RegExp(`^/pkg-files/closet/s/helper/${locked}/`),
    );
    await expect(t.getByTestId("lock-badge")).toBeVisible();
    await expect
      .poll(() =>
        t
          .getByTestId("outfit-img")
          .evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth > 0),
      )
      .toBe(true);
    expect(seen.filter((p) => p.startsWith(`/pkg-files/closet/o/helper/${locked}/`))).toEqual([]);
    expect((await request.get(`${base}/pkg-files/closet/o/helper/${locked}/1.png`)).status()).toBe(
      404,
    );
  } finally {
    for (const id of [free, locked]) {
      if (await api.get("closet.outfit", `helper-${id}`))
        await api.post("vault/archive", { kind: "closet.outfit", id: `helper-${id}` });
      rmSync(path.join(avatars, `helper@${id}.png`), { force: true });
    }
    rmSync(packDir, { recursive: true, force: true });
    // 不留給同一個 worker 後面的測試：衣櫃把這兩套從 helper 的檢視拿掉。
    await expect
      .poll(
        async () => {
          const p = await api.get("closet.person", "helper");
          const list = (p?.fields.outfits as { record: string }[]) ?? [];
          return list.filter((o) => o.record.endsWith(free) || o.record.endsWith(locked)).length;
        },
        { timeout: 20_000 },
      )
      .toBe(0);
  }
});
