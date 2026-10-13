// @vitest-environment jsdom
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

/** jsdom reports an unimplemented method on its virtual console, once per
 * call; a stub that took leaves the console silent. */
it("answers window.scrollTo and canvas getContext without a report", () => {
  const reports: string[] = []
  const report = (error: Error) => reports.push(error.message)
  jsdom.virtualConsole.on("jsdomError", report)
  try {
    window.scrollTo({ top: 0 })
    expect(document.createElement("canvas").getContext("2d")).toBeNull()
  } finally {
    jsdom.virtualConsole.off("jsdomError", report)
  }
  expect(reports).toEqual([])
})
