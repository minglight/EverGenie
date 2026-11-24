import { act, cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { PersonaMedia } from "./PersonaMedia";

function mockMotion(reduce: boolean) {
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: query.includes("prefers-reduced-motion") && reduce,
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  }));
}

beforeEach(() => {
  mockMotion(false);
  vi.spyOn(HTMLMediaElement.prototype, "play").mockImplementation(() => Promise.resolve());
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
});
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("PersonaMedia", () => {
  // Guards: REQ-156
  test("有影片：靜音、行內，圖片當 poster，整個是裝飾；不循環（播一次就停）", () => {
    const { container } = render(<PersonaMedia name="Builder" src="/a.png" video="/a.mp4" />);
    const video = container.querySelector("video");
    expect(video?.getAttribute("src")).toBe("/a.mp4");
    expect(video?.getAttribute("poster")).toBe("/a.png");
    expect(video?.loop).toBe(false);
    expect(video?.autoplay).toBe(false);
    expect(video?.muted).toBe(true);
    expect(video?.hasAttribute("playsinline")).toBe(true);
    expect(video?.getAttribute("aria-hidden")).toBe("true");
    expect(container.firstElementChild?.getAttribute("data-persona-media")).toBe("video");
    // 還沒開始播：靜態圖在畫面上，影片讓位。
    expect(container.querySelector("img")).not.toBeNull();
    expect(video?.className).toContain("invisible");
  });

  // Guards: DEV-125
  describe("隨機輪播", () => {
    beforeEach(() => vi.useFakeTimers());
    afterEach(() => vi.useRealTimers());

    function setup(props: Partial<React.ComponentProps<typeof PersonaMedia>> = {}) {
      const play = vi.spyOn(HTMLMediaElement.prototype, "play");
      const view = render(<PersonaMedia name="B" src="/a.png" idleSec={[10, 20]} {...props} />);
      const video = () => view.container.querySelector("video") as HTMLVideoElement;
      return { play, view, video };
    }

    test("沒指定 idleSec 時，兩次播放之間最短 15 秒、最長 45 秒", () => {
      for (const [rand, wait] of [
        [0, 15_000],
        [0.999, 44_970],
      ] as const) {
        vi.spyOn(Math, "random").mockReturnValue(rand);
        const play = vi.spyOn(HTMLMediaElement.prototype, "play");
        play.mockClear();
        const view = render(<PersonaMedia name="B" src="/a.png" video="/a.mp4" />);
        const video = () => view.container.querySelector("video") as HTMLVideoElement;
        act(() => vi.advanceTimersByTime(4000)); // 第一次（1.5～4 秒）
        expect(play).toHaveBeenCalledTimes(1);
        act(() => {
          fireEvent.playing(video());
          fireEvent.ended(video());
        });
        act(() => vi.advanceTimersByTime(wait - 100));
        expect(play).toHaveBeenCalledTimes(1);
        act(() => vi.advanceTimersByTime(200));
        expect(play).toHaveBeenCalledTimes(2);
        view.unmount();
        vi.restoreAllMocks();
        vi.spyOn(HTMLMediaElement.prototype, "play").mockImplementation(() => Promise.resolve());
        vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
      }
    });

    test("不是一直播：等一個隨機秒數才播，播完停下來再等下一次", () => {
      vi.spyOn(Math, "random").mockReturnValue(0.5);
      const { play, video } = setup({ video: "/a.mp4" });
      expect(play).not.toHaveBeenCalled();
      // 第一次較快：1.5～4 秒（0.5 → 2.75 秒）。
      act(() => vi.advanceTimersByTime(2700));
      expect(play).not.toHaveBeenCalled();
      act(() => vi.advanceTimersByTime(100));
      expect(play).toHaveBeenCalledTimes(1);
      // 開始播了：影片出現；播完：影片讓位給圖，然後等 10～20 秒（0.5 → 15 秒）。
      act(() => {
        fireEvent.playing(video());
      });
      expect(video().className).not.toContain("invisible");
      act(() => {
        fireEvent.ended(video());
      });
      expect(video().className).toContain("invisible");
      act(() => vi.advanceTimersByTime(14900));
      expect(play).toHaveBeenCalledTimes(1);
      act(() => vi.advanceTimersByTime(200));
      expect(play).toHaveBeenCalledTimes(2);
    });

    test("多支影片：每次隨機挑一支，從頭播", () => {
      const random = vi.spyOn(Math, "random").mockReturnValue(0.5); // 等待 2.75 秒；挑第 2 支（0.5*3=1.5 → 索引 1）
      const { play, video } = setup({ videos: ["/a.mp4", "/a+2.mp4", "/a+3.mp4"] });
      act(() => vi.advanceTimersByTime(3000));
      expect(video().getAttribute("src")).toBe("/a+2.mp4");
      expect(play).toHaveBeenCalledTimes(1);
      act(() => {
        fireEvent.ended(video());
      });
      random.mockReturnValue(0.99);
      act(() => vi.advanceTimersByTime(20000));
      expect(video().getAttribute("src")).toBe("/a+3.mp4");
      expect(play).toHaveBeenCalledTimes(2);
    });

    test("有 videos 就以它為準，壞掉的那支從清單拿掉，剩下的照常播", () => {
      vi.spyOn(Math, "random").mockReturnValue(0);
      const { video } = setup({ video: "/old.mp4", videos: ["/a.mp4", "/a+2.mp4"] });
      expect(video().getAttribute("src")).toBe("/a.mp4");
      act(() => {
        fireEvent.error(video());
      });
      expect(video().getAttribute("src")).toBe("/a+2.mp4");
      act(() => {
        fireEvent.error(video());
      });
      expect(document.querySelector("video")).toBeNull();
    });

    test("play() 被擋不丟錯，這輪算沒播，之後再排", async () => {
      vi.spyOn(Math, "random").mockReturnValue(0);
      const { play } = setup({ video: "/a.mp4" });
      play.mockImplementation(() => Promise.reject(new Error("NotAllowedError")));
      await act(async () => {
        vi.advanceTimersByTime(2000);
      });
      expect(play).toHaveBeenCalledTimes(1);
      await act(async () => {
        vi.advanceTimersByTime(10100);
      });
      expect(play).toHaveBeenCalledTimes(2);
    });

    test("暫停時不排播放、正在播的暫停；回來重新排", () => {
      vi.spyOn(Math, "random").mockReturnValue(0);
      const pause = vi.spyOn(HTMLMediaElement.prototype, "pause");
      const { play, view, video } = setup({ video: "/a.mp4" });
      act(() => vi.advanceTimersByTime(1000));
      view.rerender(
        <PersonaMedia name="B" src="/a.png" video="/a.mp4" idleSec={[10, 20]} paused />,
      );
      expect(pause).toHaveBeenCalled();
      act(() => vi.advanceTimersByTime(60000));
      expect(play).not.toHaveBeenCalled();
      view.rerender(<PersonaMedia name="B" src="/a.png" video="/a.mp4" idleSec={[10, 20]} />);
      act(() => vi.advanceTimersByTime(2000));
      expect(play).toHaveBeenCalledTimes(1);
      expect(video()).not.toBeNull();
    });

    test("影片從無到有（滑鼠移上去才給）要重新從頭等，不沿用上一次的長等待", () => {
      vi.spyOn(Math, "random").mockReturnValue(0.5);
      const { play, view, video } = setup({ video: "/a.mp4" });
      act(() => vi.advanceTimersByTime(3000)); // 第一次播
      act(() => {
        fireEvent.playing(video());
        fireEvent.ended(video());
      });
      view.rerender(<PersonaMedia name="B" src="/a.png" idleSec={[10, 20]} />); // 滑鼠離開
      expect(document.querySelector("video")).toBeNull();
      play.mockClear();
      view.rerender(<PersonaMedia name="B" src="/a.png" video="/a.mp4" idleSec={[10, 20]} />); // 再移上去
      act(() => vi.advanceTimersByTime(3000));
      expect(play).toHaveBeenCalledTimes(1);
    });

    test("換秘書（影片清單換了）：不沿用舊的影片網址與播放狀態", () => {
      vi.spyOn(Math, "random").mockReturnValue(0);
      const { view, video } = setup({ videos: ["/old.mp4", "/old+2.mp4"] });
      act(() => vi.advanceTimersByTime(2000));
      act(() => {
        fireEvent.playing(video());
      });
      expect(video().className).not.toContain("invisible");
      view.rerender(
        <PersonaMedia name="C" src="/c.png" videos={["/new.mp4"]} idleSec={[10, 20]} />,
      );
      expect(video().getAttribute("src")).toBe("/new.mp4");
      expect(video().className).toContain("invisible"); // 舊的那支不能蓋在新秘書的圖上
    });

    test("播放中暫停再恢復：接著播，不會被新的計時打斷重播", () => {
      vi.spyOn(Math, "random").mockReturnValue(0);
      const { play, view, video } = setup({ video: "/a.mp4" });
      act(() => vi.advanceTimersByTime(2000));
      act(() => {
        fireEvent.playing(video());
      });
      expect(play).toHaveBeenCalledTimes(1);
      view.rerender(
        <PersonaMedia name="B" src="/a.png" video="/a.mp4" idleSec={[10, 20]} paused />,
      );
      view.rerender(<PersonaMedia name="B" src="/a.png" video="/a.mp4" idleSec={[10, 20]} />);
      expect(play).toHaveBeenCalledTimes(2); // 恢復時接著播
      act(() => vi.advanceTimersByTime(5000)); // 比第一次的等待還久：不能又被叫一次從頭播
      expect(play).toHaveBeenCalledTimes(2);
    });

    test("叫它播卻一直沒開始（網路卡住）：看門狗放棄這一輪，之後照常排下一次", () => {
      vi.spyOn(Math, "random").mockReturnValue(0);
      const { play } = setup({ video: "/a.mp4" });
      act(() => vi.advanceTimersByTime(2000));
      expect(play).toHaveBeenCalledTimes(1);
      act(() => vi.advanceTimersByTime(10_100)); // 看門狗 10 秒
      act(() => vi.advanceTimersByTime(10_100)); // 下一次最短等 10 秒
      expect(play).toHaveBeenCalledTimes(2);
    });

    test("沒有靜態圖時影片不隱藏（總比空白好）", () => {
      const { video } = setup({ src: undefined, video: "/a.mp4" });
      expect(video().className).not.toContain("invisible");
    });
  });

  // Guards: REQ-156
  test("省流量時不播影片", () => {
    vi.stubGlobal("navigator", { ...navigator, connection: { saveData: true } });
    const { container } = render(<PersonaMedia name="Builder" src="/a.png" video="/a.mp4" />);
    expect(container.querySelector("video")).toBeNull();
    expect(container.querySelector("img")).not.toBeNull();
  });

  // Guards: REQ-156
  test("圖也載入失敗：用首字與秘書顏色鋪滿", () => {
    const { container } = render(<PersonaMedia name="Helper" src="/missing.png" color="#336699" />);
    fireEvent.error(container.querySelector("img") as HTMLImageElement);
    expect(container.querySelector("img")).toBeNull();
    const root = container.firstElementChild as HTMLElement;
    expect(root.getAttribute("data-persona-media")).toBe("fallback");
    expect(root.textContent).toBe("H");
    expect(root.style.getPropertyValue("--agent-color")).toBe("#336699");
  });
});
