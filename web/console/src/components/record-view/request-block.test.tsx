// @vitest-environment jsdom
/** A record view's request block: a button that stamps one of the record's
 * own datetime properties with now once the owner confirms, as a patch
 * holding the version the page read; when the record already holds the
 * stamp it says when it was asked; and a property that is no datetime the
 * owner writes is said as a notice, never offered. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render as renderUI,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { RECORD_VIEW_KIND, viewsOf } from "@/lib/record-view"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

import { RecordViews } from "./record-views"
import { ShippedUnits } from "@/components/units-provider.test-fixtures"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const ACCOUNT: KindInfo = {
  identity: "ada.example.com/chat/account",
  name: "account",
  authority: "ada.example.com",
  package: "chat",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    authority: "ada.example.com",
    package: "chat",
    names: { singular: "account" },
    properties: {
      unlinkRequestedAt: { type: "datetime", writer: "owner" },
      lastSeenAt: { type: "datetime", writer: "connector" },
      handle: { type: "string" },
    },
  },
}

function record(properties: Record<string, unknown>): SubstrateRecord {
  return {
    id: "a1",
    kind: ACCOUNT.identity,
    properties,
    labels: {},
    version: 7,
    createdAt: "2026-11-09T09:00:00Z",
    updatedAt: "2026-11-09T09:00:00Z",
  }
}

function mount(
  request: Record<string, unknown>,
  properties: Record<string, unknown> = {}
) {
  const writes: { path: string; body: Record<string, unknown> }[] = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input), "http://test")
      if (init?.method === "PATCH") {
        writes.push({
          path: url.pathname,
          body: JSON.parse(String(init.body)) as Record<string, unknown>,
        })
        return new Response(JSON.stringify(record({})), { status: 200 })
      }
      return new Response(
        JSON.stringify({ records: [], head: 1, generation: "g" }),
        { status: 200 }
      )
    })
  )
  const view: SubstrateRecord = {
    ...record({
      appliesTo: {
        ref: `substrate.reamde.dev/core/kind/${ACCOUNT.identity}`,
      },
      sections: [{ request }],
    }),
    id: "account/view",
    kind: RECORD_VIEW_KIND,
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  renderUI(
    <QueryClientProvider client={client}>
      <RecordViews
        views={viewsOf([view])}
        record={record(properties)}
        kind={ACCOUNT}
        kinds={[ACCOUNT]}
      />
    </QueryClientProvider>,
    { wrapper: ShippedUnits }
  )
  return { writes }
}

it("stamps the property with now once the owner confirms, against the version read", async () => {
  const { writes } = mount({
    label: "Unlink this account",
    property: "unlinkRequestedAt",
    confirm: "The bridge signs this account out. Its chats stay.",
  })
  fireEvent.click(screen.getByRole("button", { name: "Unlink this account" }))
  const dialog = await screen.findByRole("dialog")
  expect(within(dialog).getByText("Unlink this account?")).toBeTruthy()
  expect(
    within(dialog).getByText(
      "The bridge signs this account out. Its chats stay."
    )
  ).toBeTruthy()
  const before = Date.now()
  fireEvent.click(
    within(dialog).getByRole("button", { name: "Unlink this account" })
  )
  await waitFor(() => expect(writes).toHaveLength(1))
  expect(writes[0].path).toBe(`/api/v1/${ACCOUNT.identity}/a1`)
  expect(writes[0].body.ifVersion).toBe(7)
  const stamped = (writes[0].body.properties as Record<string, string>)
    .unlinkRequestedAt
  expect(Date.parse(stamped)).toBeGreaterThanOrEqual(before - 1000)
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
})

it("says when it was asked, where the record holds the stamp", () => {
  mount(
    { label: "Unlink this account", property: "unlinkRequestedAt" },
    { unlinkRequestedAt: new Date(Date.now() - 5 * 60_000).toISOString() }
  )
  expect(screen.getByText("Asked 5 min ago")).toBeTruthy()
})

it("offers nothing for a property that is no datetime the owner writes", () => {
  mount({ label: "Seen", property: "lastSeenAt" })
  expect(
    screen.getByText(
      /This request block stamps lastSeenAt, which is no date and time you write/
    )
  ).toBeTruthy()
  expect(screen.queryByRole("button", { name: "Seen" })).toBeNull()
  cleanup()
  mount({ label: "Handle", property: "handle" })
  expect(screen.getByText(/stamps handle/)).toBeTruthy()
})
