import { beforeEach, describe, expect, it, vi } from "vitest"
import { ApiError } from "@/lib/api/types"
import {
  applySidebarAction,
  preferencesOf,
  saveSidebarAction,
} from "./sidebar-preferences"
import { request } from "@/lib/api/http"

vi.mock("@/lib/api/http", async (original) => ({
  ...(await original<typeof import("@/lib/api/http")>()),
  request: vi.fn(),
}))
const wire = vi.mocked(request)
beforeEach(() => wire.mockReset())

describe("sidebar preferences", () => {
  it("preserves unrelated state when starring, ordering, and collapsing", () => {
    let prefs = preferencesOf()
    prefs = applySidebarAction(prefs, {
      type: "favorite",
      key: "example.com/tasks/task",
      starred: true,
    })
    prefs = applySidebarAction(prefs, {
      type: "favorite",
      key: "example.com/people/person",
      starred: true,
    })
    prefs = applySidebarAction(prefs, {
      type: "move",
      key: "example.com/people/person",
      direction: -1,
    })
    prefs = applySidebarAction(prefs, {
      type: "collapse",
      key: "example.com/tasks",
      collapsed: true,
    })
    expect(prefs).toEqual({
      favorites: ["example.com/people/person", "example.com/tasks/task"],
      collapsed: ["example.com/tasks"],
      sidebarOpen: true,
    })
    prefs = applySidebarAction(prefs, {
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
    await saveSidebarAction({
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
          sidebarOpen: true,
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
    await saveSidebarAction({
      type: "collapse",
      key: "example.com",
      collapsed: true,
    })
    expect(wire).toHaveBeenLastCalledWith("PUT", expect.any(String), {
      properties: {
        collapsed: ["example.com"],
        favorites: ["example.com/tasks/task"],
        sidebarOpen: true,
      },
      ifVersion: 2,
    })
  })

  it("does not overwrite preferences when the read fails", async () => {
    wire.mockRejectedValueOnce(new ApiError("internal", "unavailable", 503))
    await expect(
      saveSidebarAction({ type: "sidebar", open: false })
    ).rejects.toThrow("unavailable")
    expect(wire).toHaveBeenCalledTimes(1)
  })
})
