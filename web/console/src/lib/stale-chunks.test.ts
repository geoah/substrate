import { describe, expect, it, vi } from "vitest"

import { reloadOnStaleChunk } from "./stale-chunks"

/** A window with the three things the handler touches: events, a reload
 * that only counts, and this tab's storage. */
function fakeWindow(storage: Pick<Storage, "getItem" | "setItem">) {
  const events = new EventTarget()
  const reload = vi.fn()
  const win = {
    addEventListener: events.addEventListener.bind(events),
    location: { reload },
    sessionStorage: storage,
  } as unknown as Window
  const fail = () => {
    const event = new Event("vite:preloadError", { cancelable: true })
    events.dispatchEvent(event)
    return event
  }
  return { win, reload, fail }
}

function memoryStorage() {
  const held = new Map<string, string>()
  return {
    getItem: (key: string) => held.get(key) ?? null,
    setItem: (key: string, value: string) => void held.set(key, value),
  }
}

describe("reloadOnStaleChunk", () => {
  it("reloads once on a failed chunk load and leaves the error standing", () => {
    const { win, reload, fail } = fakeWindow(memoryStorage())
    reloadOnStaleChunk(win, () => 1_000_000)
    const event = fail()
    expect(reload).toHaveBeenCalledTimes(1)
    // Not cancelled: the import still rejects with the real error.
    expect(event.defaultPrevented).toBe(false)
  })

  it("does not reload again while the new build fails the same way", () => {
    const storage = memoryStorage()
    let clock = 1_000_000
    const before = fakeWindow(storage)
    reloadOnStaleChunk(before.win, () => clock)
    before.fail()
    expect(before.reload).toHaveBeenCalledTimes(1)

    // The reloaded page shares the tab's storage.
    clock += 5_000
    const after = fakeWindow(storage)
    reloadOnStaleChunk(after.win, () => clock)
    after.fail()
    expect(after.reload).not.toHaveBeenCalled()

    // A later deploy is a new stale tab, and reloads again.
    clock += 60_000
    after.fail()
    expect(after.reload).toHaveBeenCalledTimes(1)
  })

  it("does not reload when the tab's storage is refused", () => {
    const { win, reload, fail } = fakeWindow({
      getItem: () => {
        throw new Error("SecurityError")
      },
      setItem: () => {},
    })
    reloadOnStaleChunk(win)
    fail()
    expect(reload).not.toHaveBeenCalled()
  })
})
