// @vitest-environment jsdom
/** The edit route over a record the console offers no edit of: an adapter's
 * output, single or packed, and a record its owner wrote, which the route
 * edits as ever. The record page hides its edits; this is the door a typed
 * address or an old link still reaches. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"

const PARAMS = {
  authority: "ada.example.com",
  pkg: "fitness",
  name: "session",
  id: "run-1",
}

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: ReactNode }) => <a>{children}</a>,
  useNavigate: () => vi.fn(),
}))
vi.mock("@/router", () => ({
  recordNewRoute: { useParams: () => ({}) },
  recordEditRoute: { useParams: () => PARAMS },
}))

import { kindsQueryOptions } from "@/lib/api/kinds"
import { recordQueryOptions } from "@/lib/api/records"
import { RecordEditPage } from "./record-editor"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const SESSION: KindInfo = {
  identity: "ada.example.com/fitness/session",
  name: "session",
  authority: "ada.example.com",
  package: "fitness",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    authority: "ada.example.com",
    package: "fitness",
    names: { singular: "session" },
    properties: {
      sport: { type: "string" },
      origin: { type: "reference", kind: "any" },
      origins: { type: "reference", kind: "any", repeated: true },
    },
  },
}

const RUN: SubstrateRecord = {
  id: "run-1",
  kind: SESSION.identity,
  properties: { sport: "running" },
  labels: {},
  version: 1,
  createdAt: "2026-10-11T07:00:00Z",
  updatedAt: "2026-10-11T07:00:00Z",
}

function mount(record: SubstrateRecord) {
  // Every read is answered from the cache; nothing reaches the network.
  vi.stubGlobal(
    "fetch",
    vi.fn(() => new Promise(() => {}))
  )
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(kindsQueryOptions.queryKey, [SESSION])
  client.setQueryData(
    recordQueryOptions(PARAMS.authority, PARAMS.pkg, PARAMS.name, PARAMS.id)
      .queryKey,
    record
  )
  return render(
    <QueryClientProvider client={client}>
      <RecordEditPage />
    </QueryClientProvider>
  )
}

it.each([
  ["a single", { origin: { ref: "vendor.example.com/mirror/session/abc" } }],
  ["a packed", { origins: [{ ref: "vendor.example.com/mirror/session/abc" }] }],
])(
  "offers no edit of %s adapter output, only how to correct it",
  async (_, provenance) => {
    mount({ ...RUN, properties: { ...RUN.properties, ...provenance } })
    expect(await screen.findByText("This record is read only")).toBeTruthy()
    expect(
      document.querySelector("[data-slot=adapter-output]")?.textContent
    ).toContain("To correct it, write your own session")
    expect(screen.queryByRole("button", { name: /Save/ })).toBe(null)
  }
)

it("edits a record its owner wrote", async () => {
  mount(RUN)
  expect(await screen.findByRole("button", { name: /Save/ })).toBeTruthy()
  expect(screen.queryByText("This record is read only")).toBe(null)
})
