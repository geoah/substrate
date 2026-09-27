// @vitest-environment jsdom
/** An edit made in place reaches every read that shows the record: the
 * record and its history, every list over its kind (a collection, the
 * Agents page's provider read), a read over every kind, and the batched
 * title reads that name it. Reads over other kinds, and title reads for
 * other records, are left alone. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"

import { llmProvidersQueryOptions } from "@/lib/api/agents"
import type { SubstrateRecord } from "@/lib/api/types"

vi.mock("@/lib/api/records", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/api/records")>()
  return {
    ...actual,
    patchRecord: () => Promise.resolve({}),
  }
})

import {
  recordCountQueryOptions,
  recordQueryOptions,
  recordsQueryOptions,
  referenceTitlesQueryOptions,
} from "@/lib/api/records"
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
