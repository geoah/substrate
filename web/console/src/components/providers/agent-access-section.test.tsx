// @vitest-environment jsdom
/** What your agents may do: one row per account and write switch, each in
 * the state its toggle and the account's grant put it in. An account
 * connected before the switch existed reads Off and offers Turn on; one
 * turned on since reads Needs your approval and offers Reconnect; one whose
 * grant covers it reads Allowed. Turn on writes the toggle alone. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"

import { AgentAccessSection } from "./agent-access-section"
import type { ProviderEntry } from "./use-providers"
import { Toaster } from "@/components/ui/toast"
import type { BundleFeature, SubstrateRecord } from "@/lib/api/types"
import { accountViewOf } from "@/lib/sync"

const ACCOUNT = "providers.substrate.reamde.dev/google/account"
const READONLY = "https://www.googleapis.com/auth/gmail.readonly"
const MODIFY = "https://www.googleapis.com/auth/gmail.modify"
const EMAIL = "https://www.googleapis.com/auth/userinfo.email"

const FEATURES: BundleFeature[] = [
  { toggle: "enabledGmail", scopes: [READONLY, EMAIL], access: "read" },
  { toggle: "enabledGmailWrite", scopes: [MODIFY, EMAIL], access: "write" },
]

const fetchMock = vi.fn<typeof fetch>()
beforeEach(() => vi.stubGlobal("fetch", fetchMock))
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  fetchMock.mockReset()
})

function account(
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: ACCOUNT,
    properties: { tokenStatus: "connected", ...properties },
    labels: {},
    version: 3,
    createdAt: "2026-11-01T00:00:00Z",
    updatedAt: "2026-11-01T00:00:00Z",
  }
}

function entry(records: SubstrateRecord[]): ProviderEntry {
  return {
    info: { name: "Google" },
    permissions: [
      {
        name: "enabledGmailWrite",
        label: "Gmail sending",
        description: "let your agents draft, send and label mail",
      },
    ],
    view: {
      oauth: true,
      configured: true,
      status: { features: FEATURES },
      accounts: records.map((r) => accountViewOf(r, [])),
    },
  } as unknown as ProviderEntry
}

function renderSection(records: SubstrateRecord[]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <Toaster>
        <AgentAccessSection entry={entry(records)} />
      </Toaster>
    </QueryClientProvider>
  )
}

function rowOf(email: string): HTMLElement {
  const row = screen.getByText(email).closest("tr")
  if (!row) throw new Error(`no row for ${email}`)
  return row
}

it("says where each account's switch stands, and offers the verb that moves it", () => {
  renderSection([
    // Connected before the switch existed: its grant reads Gmail only.
    account("old", {
      email: "old@example.com",
      enabledGmail: true,
      grantedScopes: [READONLY, EMAIL],
    }),
    // Turned on since, without a reconnect.
    account("waiting", {
      email: "waiting@example.com",
      enabledGmail: true,
      enabledGmailWrite: true,
      grantedScopes: [READONLY, EMAIL],
    }),
    account("done", {
      email: "done@example.com",
      enabledGmailWrite: true,
      grantedScopes: [MODIFY, EMAIL],
    }),
  ])

  const old = within(rowOf("old@example.com"))
  expect(old.getByText("Gmail sending")).toBeTruthy()
  expect(old.getByText("Off")).toBeTruthy()
  expect(old.getByRole("button", { name: "Turn on" })).toBeTruthy()
  expect(old.queryByRole("button", { name: "Reconnect" })).toBeNull()

  const waiting = within(rowOf("waiting@example.com"))
  expect(waiting.getByText("Needs your approval")).toBeTruthy()
  expect(waiting.getByRole("button", { name: "Reconnect" })).toBeTruthy()
  expect(waiting.getByRole("button", { name: "Turn off" })).toBeTruthy()

  const done = within(rowOf("done@example.com"))
  expect(done.getByText("Allowed")).toBeTruthy()
  expect(done.getByRole("button", { name: "Turn off" })).toBeTruthy()
  expect(done.queryByRole("button", { name: "Reconnect" })).toBeNull()
})

it("turns a switch on by writing that toggle alone, and says to reconnect", async () => {
  fetchMock.mockResolvedValueOnce(
    Response.json(
      account("old", { email: "old@example.com", enabledGmailWrite: true })
    )
  )
  renderSection([
    account("old", {
      email: "old@example.com",
      enabledGmail: true,
      grantedScopes: [READONLY, EMAIL],
    }),
  ])
  fireEvent.click(screen.getByRole("button", { name: "Turn on" }))
  await waitFor(() => expect(fetchMock).toHaveBeenCalled())
  const [url, init] = fetchMock.mock.calls[0]
  expect(init?.method).toBe("PATCH")
  expect(String(url)).toContain(
    "/api/v1/providers.substrate.reamde.dev/google/account/old"
  )
  expect(JSON.parse(String(init?.body))).toEqual({
    properties: { enabledGmailWrite: true },
  })
  expect(
    await screen.findByText("Gmail sending is on for old@example.com")
  ).toBeTruthy()
  expect(
    screen.getByText(
      "Reconnect the account so Google can ask you to approve it."
    )
  ).toBeTruthy()
})

it("lists nothing for a provider whose bundle marks no switch as a write", () => {
  const e = entry([account("a", { email: "a@example.com" })])
  e.permissions = []
  const client = new QueryClient()
  const { container } = render(
    <QueryClientProvider client={client}>
      <AgentAccessSection entry={e} />
    </QueryClientProvider>
  )
  expect(container.querySelector("[data-slot=agent-access]")).toBeNull()
})
