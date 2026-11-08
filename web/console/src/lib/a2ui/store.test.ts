import { describe, expect, it, vi } from "vitest"

import type { A2UIMessage } from "@/lib/api/types"

import { SurfaceStore } from "./store"

const CATALOG =
  "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"

function create(dataModel: Record<string, unknown> = {}): A2UIMessage {
  return {
    version: "1.0",
    createSurface: {
      surfaceId: "applet",
      catalogId: CATALOG,
      components: [
        { id: "root", component: "Column", children: ["t"] },
        { id: "t", component: "Text", text: "one" },
      ],
      dataModel,
    },
  }
}

describe("the surface store", () => {
  it("creates a surface with its components and data model", () => {
    const store = new SurfaceStore()
    expect(store.process([create({ ui: { q: "" } })])).toEqual([])
    const surface = store.surface("applet")!
    expect(surface.catalogId).toBe(CATALOG)
    expect(surface.components.get("t")?.text).toBe("one")
    expect(surface.dataModel).toEqual({ ui: { q: "" } })
  })

  it("replaces components by id and keeps the rest", () => {
    const store = new SurfaceStore()
    store.process([
      create(),
      {
        version: "1.0",
        updateComponents: {
          surfaceId: "applet",
          components: [{ id: "t", component: "Text", text: "two" }],
        },
      },
    ])
    const surface = store.surface("applet")!
    expect(surface.components.get("t")?.text).toBe("two")
    expect(surface.components.get("root")?.component).toBe("Column")
  })

  it("sets a path, removes a key on null, and replaces the model at /", () => {
    const store = new SurfaceStore()
    const before =
      (store.process([create({ ui: { q: "a" }, data: { x: 1 } })]),
      store.surface("applet")!)
    const update = (path: string, value: unknown): A2UIMessage => ({
      version: "1.0",
      updateDataModel: { surfaceId: "applet", path, value },
    })
    store.process([
      update("/data/books", [{ id: "b1" }]),
      update("/ui/q", null),
    ])
    const after = store.surface("applet")!
    expect(after.dataModel).toEqual({
      ui: {},
      data: { x: 1, books: [{ id: "b1" }] },
    })
    // A new snapshot: what React drew before is untouched.
    expect(before.dataModel).toEqual({ ui: { q: "a" }, data: { x: 1 } })
    store.process([update("/", { fresh: true })])
    expect(store.surface("applet")!.dataModel).toEqual({ fresh: true })
  })

  it("deletes a surface, and refuses updates to one never created", () => {
    const store = new SurfaceStore()
    store.process([create()])
    store.process([{ version: "1.0", deleteSurface: { surfaceId: "applet" } }])
    expect(store.surface("applet")).toBeUndefined()
    expect(
      store.process([
        {
          version: "1.0",
          updateDataModel: { surfaceId: "applet", path: "/x", value: 1 },
        },
      ])
    ).toEqual(["data for unknown surface “applet”"])
  })

  it("tells subscribers of every change and input listeners of input writes", () => {
    const store = new SurfaceStore()
    const changed = vi.fn()
    const wrote = vi.fn()
    store.subscribe(changed)
    store.onInputWrite(wrote)
    store.process([create()])
    expect(changed).toHaveBeenCalledTimes(1)
    expect(wrote).not.toHaveBeenCalled()
    store.write("applet", "/ui/q", "dune")
    expect(store.surface("applet")!.dataModel).toEqual({ ui: { q: "dune" } })
    expect(changed).toHaveBeenCalledTimes(2)
    expect(wrote).toHaveBeenCalledWith("applet", "/ui/q")
  })
})
