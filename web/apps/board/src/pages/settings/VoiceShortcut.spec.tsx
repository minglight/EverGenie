import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { VOICE_SHORTCUT_URL, VoiceShortcut } from "./VoiceShortcut";

afterEach(cleanup);

// WEB-170：下載鈕指到核心的捷徑網址；安裝、手錶步驟與作者遇過的問題都在頁面上。
test("語音捷徑：下載連結、步驟與常見問題", () => {
  render(<VoiceShortcut />);
  expect(screen.getByTestId("voice-shortcut-download").getAttribute("href")).toBe(
    VOICE_SHORTCUT_URL,
  );
  expect(VOICE_SHORTCUT_URL).toBe("/voice-shortcut");
  expect(screen.getByText("放到 Apple Watch")).toBeTruthy();
  expect(screen.getAllByRole("group")).toHaveLength(9); // <details> 一題一個
  for (const q of ["結果變成 iPhone 的提醒", "變成英文", "播放音樂失敗", "一串亂碼"]) {
    expect(screen.getByText(new RegExp(q))).toBeTruthy();
  }
});
