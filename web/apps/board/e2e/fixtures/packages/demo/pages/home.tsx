import { api } from "@evergenie/sdk-web";
import { Button } from "@evergenie/ui";
import { useEffect, useState } from "react";

// 一般頁：標題之外的內容。props.search 是網址的 ?後面那串。
// 下面的筆記清單示範「套件頁面用同一組 vault API 讀自己的資料」（demo.note，P6-3a）。
export default function Home({ search }: { pkg: string; page: string; search: string }) {
  const [n, setN] = useState(0);
  const [notes, setNotes] = useState<{ id: string; text: string }[] | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    api.vault
      .list({ kind: "demo.note", include_archived: false })
      .then((r) =>
        setNotes(
          (r.records ?? []).map((x) => ({ id: x.id ?? "", text: String(x.fields?.text ?? "") })),
        ),
      )
      .catch((e: unknown) => setError(e instanceof Error ? e.message : String(e)));
  }, []);
  return (
    <section data-testid="demo-page" className="flex flex-col gap-3">
      <p>這是示範套件的頁面</p>
      <p data-testid="demo-search">search={search}</p>
      <Button onClick={() => setN(n + 1)}>按了 {n} 次</Button>
      <h2 className="font-semibold">示範筆記</h2>
      {error ? (
        <p role="alert">讀不到筆記：{error}</p>
      ) : notes === null ? (
        <p className="text-muted">載入中…</p>
      ) : (
        <ul data-testid="demo-notes" className="flex flex-col gap-1">
          {notes.map((x) => (
            <li key={x.id}>
              {x.id}：{x.text}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
