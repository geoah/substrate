import { beforeEach, describe, expect, it, vi } from "vitest"
import { ApiError } from "@/lib/api/types"
import type { KindInfo } from "@/lib/api/types"
import {
  DEFAULT_SETTINGS,
  applyConsoleAction,
  declaredSettings,
  preferencesOf,
  readLocalSettings,
  saveConsoleAction,
} from "./console-preferences"
import { request } from "@/lib/api/http"

vi.mock("@/lib/api/http", async (original) => ({
  ...(await original<typeof import("@/lib/api/http")>()),
  request: vi.fn(),
}))
const wire = vi.mocked(request)
beforeEach(() => wire.mockReset())

describe("console preferences", () => {
  it("preserves unrelated state when starring, ordering, and collapsing", () => {
    let prefs = preferencesOf()
    prefs = applyConsoleAction(prefs, {
      type: "favorite",
      key: "example.com/tasks/task",
      starred: true,
    })
    prefs = applyConsoleAction(prefs, {
      type: "favorite",
      key: "example.com/people/person",
      starred: true,
    })
    prefs = applyConsoleAction(prefs, {
      type: "move",
      key: "example.com/people/person",
      direction: -1,
    })
    prefs = applyConsoleAction(prefs, {
      type: "collapse",
      key: "example.com/tasks",
      collapsed: true,
    })
    expect(prefs).toEqual({
      favorites: ["example.com/people/person", "example.com/tasks/task"],
      collapsed: ["example.com/tasks"],
      sidebarOpen: true,
      ...DEFAULT_SETTINGS,
    })
    prefs = applyConsoleAction(prefs, {
      type: "move",
      key: "example.com/people/person",
      direction: -1,
    })
    expect(prefs.favorites[0]).toBe("example.com/people/person")
  })

  it("creates the first preference record with a version precondition", async () => {
    wire
      .mockRejectedValueOnce(new ApiError("not_found", "absent", 404))
      .mockResolvedValueOnce({ version: 1 })
    await saveConsoleAction({
      type: "collapse",
      key: "example.com",
      collapsed: true,
    })
    expect(wire).toHaveBeenLastCalledWith(
      "PUT",
      "/api/v1/substrate.reamde.dev/core/consolepreference/navigation",
      {
        properties: {
          collapsed: ["example.com"],
          favorites: [],
        },
        ifVersion: 0,
      }
    )
  })

  it("retries a conflict against fresh state without losing another browser's edit", async () => {
    wire
      .mockResolvedValueOnce({ version: 1, properties: {} })
      .mockRejectedValueOnce(new ApiError("conflict", "changed", 409))
      .mockResolvedValueOnce({
        version: 2,
        properties: { favorites: ["example.com/tasks/task"] },
      })
      .mockResolvedValueOnce({ version: 3 })
    await saveConsoleAction({
      type: "collapse",
      key: "example.com",
      collapsed: true,
    })
    expect(wire).toHaveBeenLastCalledWith("PUT", expect.any(String), {
      properties: {
        collapsed: ["example.com"],
        favorites: ["example.com/tasks/task"],
      },
      ifVersion: 2,
    })
  })

  it("does not overwrite preferences when the read fails", async () => {
    wire.mockRejectedValueOnce(new ApiError("internal", "unavailable", 503))
    await expect(
      saveConsoleAction({ type: "collapse", key: "a", collapsed: true })
    ).rejects.toThrow("unavailable")
    expect(wire).toHaveBeenCalledTimes(1)
  })
})

/** The stored preference kind, declaring the navigation properties plus
 * whichever display settings this repository's version carries. */
function preferenceKind(...settings: string[]): KindInfo {
  const properties: Record<string, unknown> = {
    collapsed: { type: "string", repeated: true },
    favorites: { type: "string", repeated: true },
    sidebarOpen: { type: "bool" },
  }
  for (const s of settings) properties[s] = { type: "string" }
  return {
    identity: "substrate.reamde.dev/core/consolepreference",
    name: "consolepreference",
    authority: "substrate.reamde.dev",
    package: "core",
    version: settings.length ? 2 : 1,
    source: "builtin",
    description: "",
    definition: { properties },
  }
}

describe("display settings", () => {
  const store = new Map<string, string>()
  beforeEach(() => {
    store.clear()
    vi.stubGlobal("localStorage", {
      getItem: (k: string) => store.get(k) ?? null,
      setItem: (k: string, v: string) => void store.set(k, v),
      removeItem: (k: string) => void store.delete(k),
    })
  })

  it("reads the record first, then this browser, then the default", () => {
    const record = {
      id: "navigation",
      kind: "substrate.reamde.dev/core/consolepreference",
      version: 3,
      properties: { recordWidth: "narrow", density: "sideways" },
    } as never
    const prefs = preferencesOf(record, {
      recordWidth: "full",
      density: "compact",
      technicalDetails: true,
    })
    expect(prefs.recordWidth).toBe("narrow")
    // an invalid stored value is no value
    expect(prefs.density).toBe("compact")
    expect(prefs.technicalDetails).toBe(true)
    expect(prefs.tableWidth).toBe("full")
    expect(prefs.theme).toBe("system")
  })

  it("knows which settings the stored kind declares", () => {
    expect(declaredSettings([preferenceKind()])).toEqual(new Set())
    expect(
      declaredSettings([preferenceKind("technicalDetails", "theme")])
    ).toEqual(new Set(["technicalDetails", "theme"]))
    expect(declaredSettings([])).toEqual(new Set())
  })

  it("keeps an undeclared setting in this browser and never writes it", async () => {
    const saved = await saveConsoleAction(
      { type: "set", key: "technicalDetails", value: true },
      declaredSettings([preferenceKind()])
    )
    expect(saved).toBeNull()
    expect(wire).not.toHaveBeenCalled()
    expect(store.get("substrate.console.technicalDetails")).toBe("true")
  })

  it("writes a declared setting to the record, and only declared ones", async () => {
    store.set("substrate.console.density", '"compact"')
    wire
      .mockResolvedValueOnce({ version: 4, properties: { favorites: ["a"] } })
      .mockResolvedValueOnce({ version: 5 })
    await saveConsoleAction(
      { type: "set", key: "recordWidth", value: "narrow" },
      declaredSettings([preferenceKind("recordWidth")])
    )
    expect(wire).toHaveBeenLastCalledWith("PUT", expect.any(String), {
      properties: {
        collapsed: [],
        favorites: ["a"],
        recordWidth: "narrow",
      },
      ifVersion: 4,
    })
    expect(store.get("substrate.console.recordWidth")).toBe("narrow")
  })

  it("keeps the theme under the key the theme provider has always used", async () => {
    store.set("theme", "dark")
    expect(preferencesOf(null, readLocalSettings()).theme).toBe("dark")
    await saveConsoleAction(
      { type: "set", key: "theme", value: "light" },
      new Set()
    )
    expect(store.get("theme")).toBe("light")
  })

  it("keeps the sidebar in this browser and never writes it to the record", async () => {
    const saved = await saveConsoleAction(
      { type: "sidebar", open: false },
      declaredSettings([preferenceKind("technicalDetails")])
    )
    expect(saved).toBeNull()
    expect(wire).not.toHaveBeenCalled()
    expect(readLocalSettings().sidebarOpen).toBe(false)
    expect(preferencesOf(null, readLocalSettings()).sidebarOpen).toBe(false)
  })

  it("ignores a sidebar state an older console stored on the record", () => {
    const record = {
      id: "navigation",
      kind: "substrate.reamde.dev/core/consolepreference",
      version: 2,
      properties: { sidebarOpen: false },
    } as never
    expect(preferencesOf(record).sidebarOpen).toBe(true)
    expect(preferencesOf(record, { sidebarOpen: false }).sidebarOpen).toBe(
      false
    )
  })

  it("ignores a value outside the setting's range", () => {
    const prefs = preferencesOf()
    expect(
      applyConsoleAction(prefs, {
        type: "set",
        key: "density",
        value: "roomy" as never,
      })
    ).toBe(prefs)
  })
})
