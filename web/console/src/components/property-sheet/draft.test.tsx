// @vitest-environment jsdom
/** A sheet over a record that does not exist yet writes into its draft: the
 * same write an in-place edit makes, and nothing reaches the server. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

const patchRecord = vi.fn()
vi.mock("@/lib/api/records", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/records")>()),
  patchRecord: (...args: unknown[]) => patchRecord(...args),
}))

import { SheetDraftContext, type SheetDraft } from "./draft"
import { useRecordPatch } from "./use-record-patch"

const unsaved: SubstrateRecord = {
  id: "",
  kind: "example.com/tasks/task",
  properties: {},
  labels: {},
  version: 0,
  createdAt: "",
  updatedAt: "",
}

describe("a draft sheet", () => {
  it("writes an edit into the draft, and never patches", async () => {
    const write = vi.fn()
    const draft: SheetDraft = {
      write,
      arrange: (rows) => ({ shown: rows, folded: [] }),
      errors: {},
    }
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={new QueryClient()}>
        <SheetDraftContext.Provider value={draft}>
          {children}
        </SheetDraftContext.Provider>
      </QueryClientProvider>
    )
    const { result } = renderHook(() => useRecordPatch(unsaved), { wrapper })
    await act(() => result.current.mutateAsync({ name: "hi", due: null }))
    expect(write).toHaveBeenCalledWith({ name: "hi", due: null })
    expect(patchRecord).not.toHaveBeenCalled()
  })
})
