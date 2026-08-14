import { describe, expect, it, vi } from "vitest"

import { installChunkReload, type ChunkReloadWindow } from "./chunk-reload"

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
  return { win, reload, preloadError, listeners }
}

describe("installChunkReload", () => {
  it("reloads on the first preload error and cancels the event", () => {
    const { win, reload, preloadError } = fakeWindow()
    installChunkReload(win)

    const event = preloadError()

    expect(reload).toHaveBeenCalledTimes(1)
    expect(event.preventDefault).toHaveBeenCalled()
  })

  it("reloads at most once per session, so a missing chunk cannot loop", () => {
    const { win, reload, preloadError } = fakeWindow()
    installChunkReload(win)

    preloadError()
    const second = preloadError()

    expect(reload).toHaveBeenCalledTimes(1)
    // The second error is left to throw: the reload did not help, and the
    // browser console reporting it beats reloading forever.
    expect(second.preventDefault).not.toHaveBeenCalled()
  })

  it("does not reload when storage refuses the guard", () => {
    const { win, reload, preloadError } = fakeWindow()
    win.sessionStorage.getItem = () => {
      throw new Error("storage disabled")
    }
    installChunkReload(win)

    preloadError()

    expect(reload).not.toHaveBeenCalled()
  })

  it("uninstalls its listener", () => {
    const { win, reload, preloadError, listeners } = fakeWindow()
    installChunkReload(win)()

    expect(listeners.size).toBe(0)
    preloadError()
    expect(reload).not.toHaveBeenCalled()
  })
})
