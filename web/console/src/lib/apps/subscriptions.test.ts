/** A subscription is an observer on the console's query cache: it answers
 * with the page once the read settles, fires again when an invalidation
 * refetches a changed page, stays quiet for an unchanged one, and lets go of
 * the query when closed. */

import { QueryClient } from "@tanstack/react-query"
import { describe, expect, it } from "vitest"

import type { ListParams } from "@/lib/api/records"
import type { Page } from "@/lib/api/types"
import { createSubscriptions, type OptionsFor } from "./subscriptions"

const tick = (ms = 20) => new Promise((r) => setTimeout(r, ms))

function harness() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Infinity } },
  })
  const pages = new Map<string, Page>()
  const fetches: string[] = []
  const optionsFor: OptionsFor = (p: ListParams) => ({
    queryKey: ["records", p.authority, p.package, p.name, { f: p.filter }],
    queryFn: async () => {
      fetches.push(p.name)
      const page = pages.get(p.name)
      if (!page) throw new Error(`no ${p.name}`)
      return page
    },
  })
  return {
    client,
    pages,
    fetches,
    store: createSubscriptions(client, optionsFor),
  }
}

const params = (name: string): ListParams => ({
  authority: "ada.example.com",
  package: "tasks",
  name,
})

const page = (ids: string[]): Page => ({
  records: ids.map((id) => ({
    id,
    kind: "ada.example.com/tasks/task",
    properties: {},
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  })),
  head: ids.length,
  generation: "g",
})

describe("createSubscriptions", () => {
  it("answers once settled and pushes a changed page after an invalidation", async () => {
    const h = harness()
    h.pages.set("task", page(["a"]))
    const results: string[] = []
    h.store.open("s1", [params("task")], (r) =>
      results.push(
        `${r.settled ? "settled" : "pending"}:${r.pages[0]?.records.map((x) => x.id).join(",") ?? "-"}`
      )
    )
    expect(h.store.size).toBe(1)
    expect(h.store.kinds()).toEqual(["ada.example.com/tasks/task"])
    await tick()
    expect(results.at(-1)).toBe("settled:a")

    // An invalidation that changes nothing pushes nothing.
    await h.client.invalidateQueries({ queryKey: ["records"] })
    await tick()
    const before = results.length
    h.pages.set("task", page(["a", "b"]))
    await h.client.invalidateQueries({ queryKey: ["records"] })
    await tick()
    expect(results.slice(before)).toEqual(["settled:a,b"])
    expect(h.fetches.length).toBe(3)

    h.store.close("s1")
    expect(h.store.size).toBe(0)
    h.pages.set("task", page(["c"]))
    await h.client.invalidateQueries({ queryKey: ["records"] })
    await tick()
    expect(results.at(-1)).toBe("settled:a,b")
  })

  it("settles a failed read with its error and fans out over several reads", async () => {
    const h = harness()
    h.pages.set("task", page(["t"]))
    let last: { settled: boolean; errors: (Error | null)[] } | undefined
    h.store.open("s2", [params("task"), params("missing")], (r) => {
      last = r
    })
    await tick()
    expect(last?.settled).toBe(true)
    expect(last?.errors[0]).toBeNull()
    expect(last?.errors[1]?.message).toBe("no missing")
    expect(h.store.kinds()).toEqual([
      "ada.example.com/tasks/missing",
      "ada.example.com/tasks/task",
    ])
    h.store.closeAll()
    expect(h.store.kinds()).toEqual([])
  })

  it("replaces a subscription reopened under the same id", async () => {
    const h = harness()
    h.pages.set("task", page(["t"]))
    h.pages.set("project", page(["p"]))
    h.store.open("s3", [params("task")], () => {})
    h.store.open("s3", [params("project")], () => {})
    expect(h.store.size).toBe(1)
    expect(h.store.kinds()).toEqual(["ada.example.com/tasks/project"])
    h.store.closeAll()
  })
})
