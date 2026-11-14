import { afterEach, describe, expect, it, vi } from "vitest"
import { SubstrateClient } from "./client"

afterEach(() => vi.unstubAllGlobals())

describe("implementors", () => {
  it("asks the trait's route, its reference one segment, and answers the kinds", async () => {
    const seen: string[] = []
    vi.stubGlobal("fetch", async (input: string) => {
      seen.push(input)
      return new Response(
        JSON.stringify({
          items: [
            {
              identity: "ada.example.com/work/chore",
              label: { singular: "Chore", plural: "Chores" },
              definition: { traits: ["substrate.reamde.dev/core/lifecycle"] },
            },
          ],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      )
    })
    const client = new SubstrateClient("https://substrate.example.com", "tok")
    const kinds = await client.implementors(
      "substrate.reamde.dev/core/lifecycle"
    )
    expect(seen).toEqual([
      "https://substrate.example.com/api/v1/substrate.reamde.dev/core/trait/substrate.reamde.dev%2Fcore%2Flifecycle/implementors",
    ])
    expect(kinds.map((k) => [k.identity, k.label?.plural])).toEqual([
      ["ada.example.com/work/chore", "Chores"],
    ])
  })

  it("answers none for a reply with no items", async () => {
    vi.stubGlobal(
      "fetch",
      async () =>
        new Response("{}", {
          status: 200,
          headers: { "Content-Type": "application/json" },
        })
    )
    const client = new SubstrateClient("https://substrate.example.com", "tok")
    expect(await client.implementors("lifecycle")).toEqual([])
  })
})

describe("listAll", () => {
  /** A list of five rows two to a page, its cursor the next row's offset,
   * that answers each continuation as `refuse` says. */
  function pages(refuse: (after: string, n: number) => Response | undefined) {
    const afters: (string | null)[] = []
    let continuations = 0
    vi.stubGlobal("fetch", async (input: string) => {
      const after = new URL(input).searchParams.get("after")
      afters.push(after)
      if (after) {
        const refused = refuse(after, ++continuations)
        if (refused) return refused
      }
      const from = Number(after ?? 0)
      const rows = ["a", "b", "c", "d", "e"].slice(from, from + 2)
      const body: Record<string, unknown> = {
        records: rows.map((id) => ({ id, kind: "ada.example.com/t/t" })),
        head: 9,
        generation: "g2",
      }
      if (from + 2 < 5) body.cursor = String(from + 2)
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    })
    return afters
  }
  const compacted = () =>
    new Response(
      JSON.stringify({
        error: {
          code: "compacted",
          message: "list again from the first page",
          head: 9,
          generation: "g2",
        },
      }),
      { status: 410, headers: { "Content-Type": "application/json" } }
    )
  const read = () =>
    new SubstrateClient("https://substrate.example.com", "tok").listAll({
      kinds: ["ada.example.com/t/t"],
      first: 2,
    })

  it("reads again from the first page when a cursor is compacted, keeping nothing twice", async () => {
    const afters = pages((_, n) => (n === 1 ? compacted() : undefined))
    const page = await read()
    expect(page.records.map((r) => r.id)).toEqual(["a", "b", "c", "d", "e"])
    expect(afters).toEqual([null, "2", null, "2", "4"])
  })

  it("throws a second refusal after its one restart", async () => {
    const afters = pages(() => compacted())
    await expect(read()).rejects.toMatchObject({
      code: "compacted",
      status: 410,
      head: 9,
      generation: "g2",
    })
    expect(afters).toEqual([null, "2", null, "2"])
  })

  it("throws any other refusal of a continuation as it is", async () => {
    const afters = pages(
      () =>
        new Response(
          JSON.stringify({
            error: { code: "validation", message: "another filter" },
          }),
          { status: 422, headers: { "Content-Type": "application/json" } }
        )
    )
    await expect(read()).rejects.toMatchObject({ code: "validation" })
    expect(afters).toEqual([null, "2"])
  })
})
