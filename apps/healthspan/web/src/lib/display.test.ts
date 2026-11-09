import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import {
  cleanDisplay,
  DEFAULT_DISPLAY,
  DISPLAY_KEY,
  displayStore,
  gridColumns,
  isChanged,
  loadDisplay,
  mainWidth,
  patchDisplay,
  resetPatch,
  saveDisplay,
  wideAddsColumns,
} from "./display"

describe("display settings", () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => localStorage.clear())

  it("has the version 2 defaults", () => {
    expect(DEFAULT_DISPLAY).toEqual({
      v: 2,
      wide: false,
      labs: {
        details: false,
        trendline: true,
        targetZones: true,
        labRange: true,
        sourceShapes: true,
        key: true,
        groupByPanel: true,
      },
      healthspan: { details: false, key: true, trendView: true },
    })
    expect(DISPLAY_KEY).toBe("healthspan.display")
  })

  it("keeps only known booleans and takes defaults for anything else", () => {
    const d = cleanDisplay({
      v: 2,
      wide: "yes",
      labs: { trendline: false, compact: true, bogus: true },
      healthspan: { details: 1, key: false },
      extra: 1,
    })
    expect(d).toEqual({
      v: 2,
      wide: false,
      labs: { ...DEFAULT_DISPLAY.labs, trendline: false },
      healthspan: { ...DEFAULT_DISPLAY.healthspan, key: false },
    })
    for (const g of [
      null,
      "x",
      { v: 3, wide: true },
      { v: 1, labs: { key: false } },
      { labs: { key: false } },
    ])
      expect(cleanDisplay(g)).toEqual(DEFAULT_DISPLAY)
  })

  it("round-trips through localStorage", () => {
    const d = patchDisplay(DEFAULT_DISPLAY, {
      wide: true,
      labs: { key: false },
      healthspan: { trendView: false },
    })
    saveDisplay(d)
    expect(JSON.parse(localStorage.getItem(DISPLAY_KEY) ?? "{}")).toEqual(d)
    expect(loadDisplay()).toEqual(d)
  })

  it("reads defaults from an empty or broken store", () => {
    expect(loadDisplay()).toEqual(DEFAULT_DISPLAY)
    localStorage.setItem(DISPLAY_KEY, "{not json")
    expect(loadDisplay()).toEqual(DEFAULT_DISPLAY)
  })

  it("reads defaults from a store that throws, and writes without throwing", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("blocked", "SecurityError")
    })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("full", "QuotaExceededError")
    })
    expect(loadDisplay()).toEqual(DEFAULT_DISPLAY)
    expect(() => saveDisplay(DEFAULT_DISPLAY)).not.toThrow()
  })

  it("reports changes per page, the wide view on both, and resets a page with it", () => {
    const d = patchDisplay(DEFAULT_DISPLAY, { labs: { key: false } })
    expect(isChanged(d, "labs")).toBe(true)
    expect(isChanged(d, "healthspan")).toBe(false)
    const w = patchDisplay(DEFAULT_DISPLAY, { wide: true })
    expect(isChanged(w, "labs")).toBe(true)
    expect(isChanged(w, "healthspan")).toBe(true)
    const back = patchDisplay(
      patchDisplay(w, { labs: { key: false } }),
      resetPatch("labs")
    )
    expect(back).toEqual(DEFAULT_DISPLAY)
  })

  it("counts the card columns the wide view adds", () => {
    expect(gridColumns(1024)).toEqual({ narrow: 3, wide: 3 })
    expect(gridColumns(1180)).toEqual({ narrow: 3, wide: 4 })
    expect(gridColumns(1366)).toEqual({ narrow: 3, wide: 4 })
    expect(gridColumns(1440)).toEqual({ narrow: 3, wide: 5 })
    expect(gridColumns(1920)).toEqual({ narrow: 3, wide: 6 })
    expect(wideAddsColumns(390)).toBe(false)
    expect(wideAddsColumns(768)).toBe(false)
    expect(wideAddsColumns(1024)).toBe(false)
    expect(wideAddsColumns(1180)).toBe(true)
  })

  it("measures the main area beside the sidebar from 768 px", () => {
    expect(mainWidth(390)).toBe(390)
    expect(mainWidth(767)).toBe(767)
    expect(mainWidth(768)).toBe(528)
    expect(mainWidth(1440)).toBe(1200)
  })

  it("shares one value through the store and saves each change", () => {
    const seen: boolean[] = []
    const off = displayStore.subscribe(() =>
      seen.push(displayStore.get().labs.key)
    )
    displayStore.set(patchDisplay(displayStore.get(), { labs: { key: false } }))
    expect(displayStore.get().labs.key).toBe(false)
    expect(seen).toEqual([false])
    expect(loadDisplay().labs.key).toBe(false)
    displayStore.set(DEFAULT_DISPLAY)
    off()
  })
})
