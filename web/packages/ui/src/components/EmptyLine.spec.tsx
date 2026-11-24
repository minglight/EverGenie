import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { CompactEmpty, EmptyLine } from "./EmptyLine";

afterEach(cleanup);

const card = <div data-testid="full-card">整張卡</div>;

// 宿主沒要求精簡（桌機、別的 slot）：照舊畫整張卡，一行版不出現。
test("TestEmptyLineRendersFullCardByDefault", () => {
  render(
    <EmptyLine label="體重" text="還沒記" action={{ label: "＋記一筆", href: "/p/body/log" }}>
      {card}
    </EmptyLine>,
  );
  expect(screen.getByTestId("full-card")).toBeTruthy();
  expect(screen.queryByTestId("empty-line")).toBeNull();
});

// 宿主包了 CompactEmpty（今天頁手機版）：只畫一行「體重：還沒記　＋記一筆」，動作是連結。
test("TestEmptyLineCollapsesToOneLineInCompactHost", () => {
  render(
    <CompactEmpty>
      <EmptyLine label="體重" text="還沒記" action={{ label: "＋記一筆", href: "/p/body/log" }}>
        {card}
      </EmptyLine>
    </CompactEmpty>,
  );
  expect(screen.queryByTestId("full-card")).toBeNull();
  const line = screen.getByTestId("empty-line");
  expect(line.textContent).toBe("體重：還沒記＋記一筆");
  expect(screen.getByRole("link", { name: "＋記一筆" }).getAttribute("href")).toBe("/p/body/log");
});

// 動作也可以是按鈕（開抽屜這類）；沒給動作就只有字。
test("TestEmptyLineActionCanBeButtonOrAbsent", () => {
  const onClick = vi.fn();
  render(
    <CompactEmpty>
      <EmptyLine label="今天的習慣" text="還沒約" action={{ label: "約一個", onClick }}>
        {card}
      </EmptyLine>
      <EmptyLine label="運動" text="這週還沒練" data-testid="no-action">
        {card}
      </EmptyLine>
    </CompactEmpty>,
  );
  fireEvent.click(screen.getByRole("button", { name: "約一個" }));
  expect(onClick).toHaveBeenCalledTimes(1);
  const plain = screen.getByTestId("no-action");
  expect(plain.querySelector("a, button")).toBeNull();
});
