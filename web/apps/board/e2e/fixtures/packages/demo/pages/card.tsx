export default function Card() {
  return (
    <section
      data-testid="demo-card"
      className="rounded-xl border border-border bg-surface p-4 text-sm shadow-card"
    >
      <h2 className="font-semibold">示範卡片</h2>
      <p className="text-muted">這張卡片是示範套件掛在今天頁的。</p>
    </section>
  );
}
