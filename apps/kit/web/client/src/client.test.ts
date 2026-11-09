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
