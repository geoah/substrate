import { describe, expect, it, vi } from "vitest"

import { installChunkReload, type ChunkReloadWindow } from "./chunk-reload"

const MINUTE = 60 * 1000

function fakeWindow() {
  const listeners = new Map<string, (event: Event) => void>()
  const store = new Map<string, string>()
  const reload = vi.fn()
  const win: ChunkReloadWindow = {
    addEventListener: (type, listener) => listeners.set(type, listener),
    removeEventListener: (type, listener) => {
      if (listeners.get(type) === listener) listeners.delete(type)
    },
    location: { reload },
    sessionStorage: {
      getItem: (key) => store.get(key) ?? null,
      setItem: (key, value) => {
        store.set(key, value)
      },
    },
  }
  const preloadError = () => {
    const event = { preventDefault: vi.fn() } as unknown as Event
    listeners.get("vite:preloadError")?.(event)
    return event
  }
  return { win, store, reload, preloadError, listeners }
}

/** A clock the test moves by hand, so the ten-minute window costs no seconds. */
function fakeClock(start = 1_700_000_000_000) {
  let t = start
  return {
    now: () => t,
    advance: (ms: number) => {
      t += ms
    },
  }
}

describe("installChunkReload", () => {
  it("reloads on the first preload error and cancels the event", () => {
    const { win, reload, preloadError } = fakeWindow()
    installChunkReload(win, fakeClock().now)

    const event = preloadError()

    expect(reload).toHaveBeenCalledTimes(1)
    expect(event.preventDefault).toHaveBeenCalled()
  })

  it("will not reload twice in a row, so a missing chunk cannot loop", () => {
    const { win, reload, preloadError } = fakeWindow()
    const clock = fakeClock()
    installChunkReload(win, clock.now)

    preloadError()
    // A loop is seconds apart, which is what the window is sized against.
    clock.advance(2000)
    const second = preloadError()

    expect(reload).toHaveBeenCalledTimes(1)
    // The suppressed error is left to throw, where the browser console
    // reports it — better than reloading forever.
    expect(second.preventDefault).not.toHaveBeenCalled()
  })

  it("recovers from a SECOND deploy once the window has passed (#54.5)", () => {
    // The behaviour the old once-per-session guard could not give: a tab that
    // outlives two deploys self-recovers from both.
    const { win, reload, preloadError } = fakeWindow()
    const clock = fakeClock()
    installChunkReload(win, clock.now)

    preloadError()
    clock.advance(11 * MINUTE)
    preloadError()

    expect(reload).toHaveBeenCalledTimes(2)
  })

  it("stops after three, so a permanently broken deploy is bounded", () => {
    const { win, reload, preloadError } = fakeWindow()
    const clock = fakeClock()
    installChunkReload(win, clock.now)

    for (let i = 0; i < 5; i++) {
      preloadError()
      clock.advance(11 * MINUTE)
    }

    // Three reloads spread over at least twenty minutes, then the error is
    // left to throw: reloading did not fix it and will not.
    expect(reload).toHaveBeenCalledTimes(3)
  })

  it("treats the old one-shot mark as spent and long ago", () => {
    // A tab that upgraded across this change holds `"1"`, which is not JSON.
    // Reading it as "one reload, at an unknown time" lets the window admit the
    // next deploy rather than stranding the tab for its whole life.
    const { win, store, reload, preloadError } = fakeWindow()
    store.set("substrate.chunk-reload", "1")
    installChunkReload(win, fakeClock().now)

    preloadError()

    expect(reload).toHaveBeenCalledTimes(1)
  })

  it("does not reload when storage refuses the guard", () => {
    const { win, reload, preloadError } = fakeWindow()
    win.sessionStorage.getItem = () => {
      throw new Error("storage disabled")
    }
    installChunkReload(win, fakeClock().now)

    preloadError()

    expect(reload).not.toHaveBeenCalled()
  })

  it("uninstalls its listener", () => {
    const { win, reload, preloadError, listeners } = fakeWindow()
    installChunkReload(win, fakeClock().now)()

    expect(listeners.size).toBe(0)
    preloadError()
    expect(reload).not.toHaveBeenCalled()
  })
})
