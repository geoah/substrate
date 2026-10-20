// @vitest-environment jsdom
/** The connect hook's success notice: when the consent tab reports back, the
 * toast says what the approval starts, from the account's sync as the
 * connect began (approvedWords). */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { toast } from "@/components/ui/toast"
import { SUBSTRATE_OAUTH_SOURCE } from "@/lib/api/bundles"

import { useOAuthConnect } from "./use-oauth-connect"

vi.mock("@/lib/api/bundles", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/bundles")>()),
  startOAuth: vi.fn(async () => ({ url: "https://provider.example/consent" })),
}))

const RECORD = "providers.substrate.reamde.dev/whoop/account/geoah"

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>
}

describe("useOAuthConnect", () => {
  let added: { title?: unknown; description?: unknown }[]
  beforeEach(() => {
    added = []
    vi.spyOn(toast, "add").mockImplementation((t) => {
      added.push(t as { title?: unknown; description?: unknown })
      return ""
    })
    vi.spyOn(window, "open").mockReturnValue({
      location: { href: "" },
      close: vi.fn(),
    } as unknown as Window)
  })
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  async function approve(
    sync?: Parameters<typeof useOAuthConnect>[2]
  ): Promise<unknown> {
    const { result } = renderHook(
      () => useOAuthConnect(RECORD, "geoah", sync),
      { wrapper }
    )
    await act(async () => {
      await result.current.mutateAsync()
    })
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          origin: window.location.origin,
          data: { source: SUBSTRATE_OAUTH_SOURCE, ok: true, record: RECORD },
        })
      )
    })
    await waitFor(() =>
      expect(added.some((t) => t.title === "Account connected")).toBe(true)
    )
    return added.find((t) => t.title === "Account connected")?.description
  }

  it("says a sync is starting on a reconnect of an account that synced", async () => {
    expect(
      await approve({ paused: false, lastSyncedAt: "2026-10-19T10:00:00Z" })
    ).toBe("geoah is approved and a sync is starting.")
  })

  it("says the first sync is starting on an account that never synced", async () => {
    expect(await approve({ paused: false })).toBe(
      "geoah is approved and its first sync is starting."
    )
  })

  it("says nothing runs on a paused account", async () => {
    expect(
      await approve({ paused: true, lastSyncedAt: "2026-10-19T10:00:00Z" })
    ).toBe(
      "geoah is approved. Its sync is paused, so nothing runs until you resume it."
    )
  })
})
