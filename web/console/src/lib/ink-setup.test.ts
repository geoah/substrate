/** The paper a new note starts on, kept in this browser: the default until
 * a note is saved, the last saved one after, and the default again for
 * anything this browser cannot read. */

import { describe, expect, it } from "vitest"

import { DEFAULT_INK_SETUP, readInkSetup, writeInkSetup } from "./ink-setup"

function memory(): Storage {
  const items = new Map<string, string>()
  return {
    get length() {
      return items.size
    },
    clear: () => items.clear(),
    getItem: (key) => items.get(key) ?? null,
    key: (i) => [...items.keys()][i] ?? null,
    removeItem: (key) => void items.delete(key),
    setItem: (key, value) => void items.set(key, value),
  }
}

describe("the ink setup", () => {
  it("starts on lined A4 portrait, with a finger scrolling", () => {
    expect(readInkSetup(memory())).toEqual(DEFAULT_INK_SETUP)
    expect(DEFAULT_INK_SETUP).toEqual({
      size: { width: 1240, height: 1754 },
      background: "lined",
      fingerDraws: false,
    })
  })

  it("remembers what was last saved", () => {
    const storage = memory()
    writeInkSetup(
      { size: { width: 1376, height: 1032 }, background: "dots" },
      storage
    )
    writeInkSetup({ fingerDraws: true }, storage)
    expect(readInkSetup(storage)).toEqual({
      size: { width: 1376, height: 1032 },
      background: "dots",
      fingerDraws: true,
    })
  })

  it("falls back to the default for what it cannot read", () => {
    const storage = memory()
    storage.setItem(
      "substrate.ink.setup",
      JSON.stringify({ size: { width: -1, height: "x" }, background: "hex" })
    )
    expect(readInkSetup(storage)).toEqual(DEFAULT_INK_SETUP)
    storage.setItem("substrate.ink.setup", "{not json")
    expect(readInkSetup(storage)).toEqual(DEFAULT_INK_SETUP)
  })
})
