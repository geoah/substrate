import { QueryClient } from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { llmModelsQueryOptions, modelKindUnknown } from "./agents"
import { ApiError, type SubstrateRecord } from "./types"

const model = (id: string): SubstrateRecord =>
  ({
    kind: "substrate.reamde.dev/llm/model",
    id,
    properties: {},
  }) as unknown as SubstrateRecord

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  })

describe("the llm/model read", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => vi.stubGlobal("fetch", fetchMock))
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("reads every page, so a record past the first is never missing", async () => {
    fetchMock
      .mockResolvedValueOnce(
        json({
          records: [model("a")],
          cursor: "next",
          head: 7,
          generation: "g",
        })
      )
      .mockResolvedValueOnce(
        json({ records: [model("b")], head: 9, generation: "g" })
      )
    const page = await new QueryClient().fetchQuery(llmModelsQueryOptions())
    expect(page.records.map((r) => r.id)).toEqual(["a", "b"])
    expect(page.head).toBe(7)
    const second = new URL(String(fetchMock.mock.calls[1]?.[0]), "http://x")
    expect(second.searchParams.get("after")).toBe("next")
  })

  it("reads a server older than the kind as holding no records", async () => {
    // The envelope the records read answers an undeclared filter kind with.
    fetchMock.mockResolvedValueOnce(
      json(
        {
          error: {
            code: "validation",
            message:
              'substrate: validation failed: unknown type "substrate.reamde.dev/llm/model"',
          },
        },
        422
      )
    )
    const page = await new QueryClient().fetchQuery(llmModelsQueryOptions())
    expect(page.records).toEqual([])
  })

  it("keeps every other refusal a refusal", () => {
    expect(
      modelKindUnknown(
        new ApiError(
          "validation",
          'substrate: validation failed: unknown type "x.example.com/a/b"',
          422
        )
      )
    ).toBe(false)
    expect(modelKindUnknown(new ApiError("not_found", "not found", 404))).toBe(
      false
    )
  })
})
