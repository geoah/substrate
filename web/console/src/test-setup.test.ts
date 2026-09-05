import { expect, it } from "vitest"

/** node 26 answers `sessionStorage` itself and `localStorage` with `undefined`,
 * so identity, not the round trip, is what proves test-setup.ts ran. */
it.each(["localStorage", "sessionStorage"] as const)(
  "%s is jsdom's, and stores and reads back",
  (key) => {
    const storage = globalThis[key]
    expect(storage).toBe((jsdom.window as Window)[key])
    storage.setItem("substrate.probe", "kept")
    expect(storage.getItem("substrate.probe")).toBe("kept")
    storage.removeItem("substrate.probe")
    expect(storage.getItem("substrate.probe")).toBeNull()
  }
)
