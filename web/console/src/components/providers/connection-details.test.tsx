// @vitest-environment jsdom
/** Connection details' parked deliveries: one the door accepted and is still
 * filing reads as filing, never as a failure, and cannot be sent again,
 * while a delivery that failed keeps its error and its retry. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { ConnectionDetails } from "./connection-details"
import {
  syncStatusesQueryOptions,
  triggerParkedQueryOptions,
  triggerRecordsQueryOptions,
  triggerRunsQueryOptions,
  triggerStatusesQueryOptions,
} from "@/lib/api/sync"
import type { SubstrateRecord, TriggerFailure } from "@/lib/api/types"
import { TRIGGER_KIND } from "@/lib/core-kinds"
import { accountViewOf } from "@/lib/sync"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    ...rest
  }: { children: React.ReactNode } & Record<string, unknown>) => (
    <a className={rest.className as string | undefined}>{children}</a>
  ),
}))

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new Error("offline")))
  )
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ACCOUNT_KIND = "providers.substrate.reamde.dev/applehealth/account"
const TRIGGER = "applehealth-to-health"
const AT = "2026-10-31T09:00:00Z"

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: AT,
    updatedAt: AT,
  }
}

const ACCOUNT = record(ACCOUNT_KIND, "substrate-sync-1a2b3c4d", {
  displayName: "Ada's iPhone",
})

const TRIGGER_RECORD = record(TRIGGER_KIND, TRIGGER, {
  enabled: true,
  source: { record: { kinds: [ACCOUNT_KIND], ops: ["update"] } },
})

function parked(over: Partial<TriggerFailure>): TriggerFailure {
  return {
    id: 1,
    trigger: TRIGGER,
    attempts: 1,
    lastError: "",
    parkedAt: AT,
    ...over,
  }
}

function renderDetails(rows: TriggerFailure[]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(triggerRecordsQueryOptions.queryKey, [TRIGGER_RECORD])
  client.setQueryData(triggerStatusesQueryOptions.queryKey, [])
  client.setQueryData(syncStatusesQueryOptions.queryKey, [])
  client.setQueryData(triggerParkedQueryOptions(TRIGGER).queryKey, rows)
  client.setQueryData(triggerRunsQueryOptions(TRIGGER, 10).queryKey, [])
  return render(
    <QueryClientProvider client={client}>
      <ConnectionDetails accounts={[accountViewOf(ACCOUNT, [])]} />
    </QueryClientProvider>
  )
}

describe("ConnectionDetails parked deliveries", () => {
  it("reads a delivery still filing as filing, with no retry, beside one that failed", () => {
    renderDetails([
      parked({
        id: 2,
        fireId: "fire-2",
        pending: true,
        lastError:
          "delivery accepted: the fire has not settled, and a restart resumes it",
      }),
      parked({
        id: 1,
        seq: 41,
        recordId: ACCOUNT.id,
        attempts: 3,
        lastError: "the function raised: ValueError",
        parkedAt: "2026-10-30T09:00:00Z",
      }),
    ])
    const [filing, failed] = screen.getAllByRole("listitem")

    expect(
      within(filing).getByText(
        "Accepted, still being filed. It leaves this list when it is filed."
      )
    ).toBeTruthy()
    expect(within(filing).queryByText(/fire has not settled/)).toBeNull()
    expect(
      within(filing).getByRole<HTMLButtonElement>("button", {
        name: "Try again",
      }).disabled
    ).toBe(true)

    expect(
      within(failed).getByText("the function raised: ValueError")
    ).toBeTruthy()
    expect(
      within(failed).getByRole<HTMLButtonElement>("button", {
        name: "Try again",
      }).disabled
    ).toBe(false)
  })
})
