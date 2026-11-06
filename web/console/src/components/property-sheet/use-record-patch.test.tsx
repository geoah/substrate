// @vitest-environment jsdom
/** An edit made in place reaches every read that shows the record: the
 * record and its history, every list over its kind (a collection, the
 * Agents page's provider read), a read over every kind, and the batched
 * title reads that name it. Reads over other kinds, and title reads for
 * other records, are left alone. Inside a draft the edit lands in the draft
 * and leaves nothing in the mutation cache, where the draft's record would
 * keep whatever secret was typed into it. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"

import { llmProvidersQueryOptions } from "@/lib/api/agents"
import type { SubstrateRecord } from "@/lib/api/types"
import { expectNoTrace } from "@/hooks/use-secret-mutation.test-fixtures"

const patchRecord = vi.fn(() => Promise.resolve({}))
vi.mock("@/lib/api/records", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/records")>()
  return {
    ...actual,
    patchRecord: () => patchRecord(),
  }
})

import {
  recordCountQueryOptions,
  recordQueryOptions,
  recordsQueryOptions,
  referenceTitlesQueryOptions,
} from "@/lib/api/records"
import { SheetDraftContext, type SheetDraft } from "./draft"
import { useRecordPatch } from "./use-record-patch"

const PROVIDER = "substrate.reamde.dev/llm/provider"

const record: SubstrateRecord = {
  id: "openai",
  kind: PROVIDER,
  properties: {},
  labels: {},
  version: 3,
  createdAt: "2026-09-25T10:00:00Z",
  updatedAt: "2026-09-25T10:00:00Z",
}

describe("useRecordPatch", () => {
  it("marks every read over the record's kind stale after a save", async () => {
    const client = new QueryClient()
    const seed = (key: readonly unknown[]) => {
      client.setQueryData(key, { records: [] })
      return key
    }
    const touched = [
      seed(
        recordQueryOptions("substrate.reamde.dev", "llm", "provider", "openai")
          .queryKey
      ),
      seed(["changes", "record", PROVIDER, "openai"]),
      seed(llmProvidersQueryOptions().queryKey),
      seed(
        recordsQueryOptions({
          authority: "substrate.reamde.dev",
          package: "llm",
          name: "provider",
          first: 50,
          orderBy: "updatedAt:desc",
        }).queryKey
      ),
      seed(recordsQueryOptions({ kinds: [] }).queryKey),
      seed(
        recordCountQueryOptions("substrate.reamde.dev", "llm", "provider")
          .queryKey
      ),
      seed(
        referenceTitlesQueryOptions({
          kinds: [PROVIDER, "substrate.reamde.dev/core/agent"],
          ids: ["openai", "helper"],
        }).queryKey
      ),
    ]
    const untouched = [
      seed(
        recordsQueryOptions({
          authority: "substrate.reamde.dev",
          package: "core",
          name: "agent",
        }).queryKey
      ),
      seed(
        referenceTitlesQueryOptions({ kinds: [PROVIDER], ids: ["anthropic"] })
          .queryKey
      ),
    ]

    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const { result } = renderHook(() => useRecordPatch(record), { wrapper })
    await act(() => result.current.mutateAsync({ apiKey: "set" }))

    for (const key of touched)
      expect(
        client.getQueryState(key)?.isInvalidated,
        JSON.stringify(key)
      ).toBe(true)
    for (const key of untouched)
      expect(
        client.getQueryState(key)?.isInvalidated,
        JSON.stringify(key)
      ).toBe(false)
  })
})

describe("useRecordPatch in a draft", () => {
  // The draft's record, as the create sheet and the change request review
  // hand it to the sheet: what was typed so far, a secret included.
  const drafted: SubstrateRecord = {
    ...record,
    properties: { label: "OpenAI", apiKey: "sk-drafted-secret" },
    version: 0,
  }

  function setup(write: SheetDraft["write"]) {
    const client = new QueryClient()
    const draft: SheetDraft = {
      write,
      arrange: (rows) => ({ shown: rows, folded: [] }),
      errors: {},
    }
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>
        <SheetDraftContext.Provider value={draft}>
          {children}
        </SheetDraftContext.Provider>
      </QueryClientProvider>
    )
    const view = renderHook(() => useRecordPatch(drafted), { wrapper })
    return { client, ...view }
  }

  it("writes the draft and leaves nothing in the mutation cache", async () => {
    patchRecord.mockClear()
    const write = vi.fn()
    const { client, result, unmount } = setup(write)

    await act(async () => {
      await expect(result.current.mutateAsync({ label: "Mine" })).resolves.toBe(
        drafted
      )
    })
    expectNoTrace(client, "sk-drafted-secret")

    const onSuccess = vi.fn()
    act(() => result.current.mutate({ label: null }, { onSuccess }))
    await waitFor(() => expect(onSuccess).toHaveBeenCalledTimes(1))
    expect(onSuccess.mock.calls[0][0]).toBe(drafted)

    expect(write.mock.calls).toEqual([[{ label: "Mine" }], [{ label: null }]])
    expect(patchRecord).not.toHaveBeenCalled()
    expect(result.current.isPending).toBe(false)
    unmount()
    expectNoTrace(client, "sk-drafted-secret")
  })

  it("reports a draft that refuses the write to the caller", async () => {
    const { client, result } = setup(() => {
      throw new Error("This YAML doesn’t read as a record.")
    })

    await act(async () => {
      await expect(result.current.mutateAsync({ label: "x" })).rejects.toThrow(
        "doesn’t read as a record"
      )
    })
    const onError = vi.fn()
    act(() => result.current.mutate({ label: "x" }, { onError }))
    await waitFor(() =>
      expect(onError.mock.calls[0][0].message).toMatch(
        /doesn’t read as a record/
      )
    )
    expectNoTrace(client, "sk-drafted-secret")
  })
})
