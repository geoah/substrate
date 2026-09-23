// @vitest-environment jsdom
/** The Connections page over one provider with two accounts: the provider
 * card says whether the client credentials are set and counts the accounts
 * by token status; the accounts table shows the token, the trait's state
 * and message, the last run as relative time and one health dot per row;
 * and the row verbs go out as the existing routes — Sync now is a PATCH of
 * `syncRequestedAt` followed by a wake of the on-request trigger, Pause a
 * PATCH of `syncPaused`, Disconnect a DELETE of the record. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactElement } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import type {
  BundleStatus,
  CatalogItem,
  KindInfo,
  SubstrateRecord,
  TriggerStatus,
} from "@/lib/api/types"

const navigate = vi.fn().mockResolvedValue(undefined)

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(params ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

import { ConnectionsPage } from "./connections"

const GOOGLE = "providers.substrate.reamde.dev/google"
const ACCOUNT = `${GOOGLE}/account`
const CONFIG = `${GOOGLE}/config`
const STATUS_PATH = "/api/v1/substrate.reamde.dev/core/bundle/status"
const TRIGGER_STATUS_PATH = "/api/v1/substrate.reamde.dev/core/trigger/status"
const CATALOG_PATH = "/api/v1/catalog"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

function kind(
  identity: string,
  traits: string[],
  properties: Record<string, unknown> = {}
): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "published",
    description: "",
    definition: { traits, properties, names: { singular: name } },
  }
}

const KINDS: KindInfo[] = [
  kind(ACCOUNT, ["accountconfig", "sync"], {
    email: { type: "email", writer: "oauth" },
    address: {
      type: "reference",
      kind: `${GOOGLE}/emailaddress`,
      writer: "connector",
      displayName: "Address",
    },
    enabledGmail: { type: "bool", writer: "owner", displayName: "Gmail" },
    syncFrequency: {
      type: "enum",
      values: ["off", "hourly", "daily"],
      required: true,
      default: "daily",
      writer: "owner",
    },
    syncState: { type: "string", writer: "connector" },
    syncPaused: { type: "bool", writer: "owner", displayName: "Paused" },
    syncRequestedAt: {
      type: "datetime",
      writer: "owner",
      displayName: "Sync requested",
    },
    gmailBackfillResume: {
      type: "object",
      writer: "connector",
      displayName: "Gmail backfill resume",
      fields: { pageToken: { type: "string" } },
    },
  }),
  kind(CONFIG, ["oauth2"], {
    clientId: { type: "string" },
    clientSecret: { type: "secret" },
  }),
]

const STATUS: BundleStatus = {
  id: GOOGLE,
  name: "google",
  authority: "providers.substrate.reamde.dev",
  package: "google",
  installed: true,
  enabled: true,
  inputs: [{ name: "client", kind: CONFIG, record: "default", via: "default" }],
  accounts: 2,
  functions: 3,
  kinds: 12,
  liveRecords: 4210,
}

const CATALOG: CatalogItem = {
  id: GOOGLE,
  name: "google",
  authority: "providers.substrate.reamde.dev",
  package: "google",
  description: "",
  version: 17,
  tier: "provider",
  installed: true,
  closure: {
    kinds: null,
    traits: null,
    functions: null,
    agents: null,
    mappings: null,
    records: null,
    triggers: null,
  },
}

const CONSENT_URL = "https://accounts.google.com/o/oauth2/v2/auth?x=1"
const HOUR_AGO = new Date(Date.now() - 2 * 3600_000).toISOString()
const REQUESTED = new Date(Date.now() - 60_000).toISOString()

function account(
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: ACCOUNT,
    properties,
    labels: {},
    version: 3,
    createdAt: HOUR_AGO,
    updatedAt: HOUR_AGO,
  }
}

const WORK = account("george-work", {
  email: "george@example.com",
  tokenStatus: "connected",
  grantedScopes: ["https://www.googleapis.com/auth/gmail.readonly"],
  syncFrequency: "hourly",
  backfillDepth: "last30d",
  syncState: "erroring",
  syncMessage: "ok (256 pending: queue x118, rate limited)",
  syncError: "gmail: HTTP 403",
  lastSyncedAt: HOUR_AGO,
  syncRequestedAt: REQUESTED,
  syncStreams: {
    gmail: { state: "erroring", pending: 118 },
    contacts: { state: "ok", lastAt: HOUR_AGO, pending: 0 },
  },
})

const PERSONAL = account("george-home", {
  email: "home@example.com",
  tokenStatus: "pending",
  syncFrequency: "daily",
  backfillDepth: "all",
})

function trigger(id: string, when: string): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/core/trigger",
    properties: {
      enabled: true,
      source: { record: { kinds: [ACCOUNT], ops: ["create", "update"], when } },
      callable: `substrate.reamde.dev/core/function/${GOOGLE}/gmailsync`,
    },
    labels: {},
    version: 1,
    createdAt: HOUR_AGO,
    updatedAt: HOUR_AGO,
  }
}

const TRIGGERS = [
  trigger(
    "google-gmail-on-connect",
    '!("gmailLastSyncedAt" in record.properties)'
  ),
  trigger(
    "google-gmail-on-request",
    "record.properties.syncStreams.gmail.requestedAck != record.properties.syncRequestedAt"
  ),
]

const TRIGGER_STATUSES: TriggerStatus[] = [
  {
    id: "google-gmail-on-connect",
    kind: "record",
    callable: `${GOOGLE}/gmailsync`,
    enabled: true,
    cursor: 40,
    head: 42,
    lag: 2,
    parked: 1,
    pending: 0,
  },
  {
    id: "google-gmail-on-request",
    kind: "record",
    callable: `${GOOGLE}/gmailsync`,
    enabled: true,
    cursor: 42,
    head: 42,
    parked: 0,
    pending: 0,
  },
]

function renderPage(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <NuqsTestingAdapter>
      <QueryClientProvider client={client}>
        <Toaster>{ui}</Toaster>
      </QueryClientProvider>
    </NuqsTestingAdapter>
  )
}

/** What a records-route URL asks for, off its `filter`. */
function filterOf(path: string): { kinds?: string[]; implements?: string } {
  const url = new URL(path, "http://x")
  if (url.pathname !== "/api/v1/records") return {}
  return JSON.parse(url.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
    implements?: string
  }
}

describe("ConnectionsPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  function serve(status: BundleStatus = STATUS) {
    fetchMock.mockImplementation(async (url, init) => {
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      const path = String(url)
      if (path === STATUS_PATH) return jsonResponse(200, { items: [status] })
      if (path === CATALOG_PATH) return jsonResponse(200, { items: [CATALOG] })
      if (path === TRIGGER_STATUS_PATH)
        return jsonResponse(200, { items: TRIGGER_STATUSES })
      if (path === "/api/v1/sync/status")
        return jsonResponse(200, {
          items: [
            {
              kind: ACCOUNT,
              id: "george-work",
              state: "erroring",
              paused: false,
              parked: 1,
              triggers: [],
            },
            {
              kind: ACCOUNT,
              id: "george-home",
              state: "never",
              paused: false,
              parked: 0,
              triggers: [],
            },
          ],
        })
      const filter = filterOf(path)
      if (filter.kinds?.includes("substrate.reamde.dev/core/kind"))
        return jsonResponse(200, { kinds: KINDS })
      if (filter.implements === "substrate.reamde.dev/core/accountconfig")
        return jsonResponse(200, { records: [WORK, PERSONAL] })
      if (filter.kinds?.includes("substrate.reamde.dev/core/trigger"))
        return jsonResponse(200, { records: TRIGGERS })
      if (path.startsWith("/api/v1/changes")) {
        return jsonResponse(404, {
          error: { code: "not_found", message: "no watch in this test" },
        })
      }
      if (method === "PATCH") {
        const body = JSON.parse(String((init as RequestInit).body)) as {
          properties: Record<string, unknown>
        }
        return jsonResponse(200, {
          ...WORK,
          properties: { ...WORK.properties, ...body.properties },
        })
      }
      if (method === "POST" && path.endsWith("/wake"))
        return jsonResponse(200, { ran: 1 })
      if (method === "POST" && path === "/api/v1/records") {
        const body = JSON.parse(String((init as RequestInit).body)) as {
          properties: Record<string, unknown>
        }
        return jsonResponse(201, account("new-acct", body.properties))
      }
      if (method === "POST" && path.endsWith("/oauth/start"))
        return jsonResponse(200, { url: CONSENT_URL })
      if (method === "DELETE") return new Response(null, { status: 204 })
      return jsonResponse(200, {})
    })
  }

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    navigate.mockClear()
    localStorage.clear()
    serve()
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  function calls(method: string) {
    return fetchMock.mock.calls
      .filter(
        ([, init]) =>
          ((init as RequestInit | undefined)?.method ?? "GET") === method
      )
      .map(([url, init]) => ({
        url: String(url),
        body: (init as RequestInit | undefined)?.body
          ? (JSON.parse(String((init as RequestInit).body)) as Record<
              string,
              unknown
            >)
          : undefined,
      }))
  }

  async function rowOf(label: string) {
    const link = await screen.findByText(label)
    return link.closest("tr")!
  }

  async function openAddAccount() {
    const link = (await screen.findAllByText("google")).find(
      (el) => el.tagName === "A"
    )!
    const card = link.closest<HTMLElement>("div.rounded-md")!
    fireEvent.click(
      within(card).getAllByRole("button", { name: /Add account/ })[0]
    )
    return screen.findByRole("dialog")
  }

  it("says what to do next on the card: connect the account that is waiting", async () => {
    renderPage(<ConnectionsPage />)
    const link = (await screen.findAllByText("google")).find(
      (el) => el.tagName === "A"
    )!
    const card = link.closest<HTMLElement>("div.rounded-md")!
    expect(
      within(card).getByText(/home@example.com is not connected yet/)
    ).toBeTruthy()
  })

  it("Add account asks only what the owner decides, in plain words", async () => {
    renderPage(<ConnectionsPage />)
    const dialog = await openAddAccount()
    expect(within(dialog).getByText("Add a google account")).toBeTruthy()
    expect(
      within(dialog).getByText(/approve access to each item you turned on/)
    ).toBeTruthy()
    // The owner's toggle and cadence, under their headings.
    expect(within(dialog).getByText("What to sync")).toBeTruthy()
    expect(within(dialog).getByLabelText("Gmail")).toBeTruthy()
    expect(within(dialog).getByLabelText(/Sync frequency/)).toBeTruthy()
    // Not the facility's, the connector's, or the trait's two owner hands.
    expect(within(dialog).queryByText("Address")).toBeNull()
    expect(within(dialog).queryByText("Gmail backfill resume")).toBeNull()
    expect(within(dialog).queryByText("Paused")).toBeNull()
    expect(within(dialog).queryByText("Sync requested")).toBeNull()
    expect(
      within(dialog).getByRole("button", { name: "Create and connect" })
    ).toBeTruthy()
  })

  it("Create and connect creates the account, then opens the consent in the tab opened at the press", async () => {
    const tab = { location: { href: "" }, close: vi.fn() }
    const open = vi.fn().mockReturnValue(tab)
    vi.stubGlobal("open", open)
    renderPage(<ConnectionsPage />)
    const dialog = await openAddAccount()
    // Nothing turned on is refused before any request goes out.
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Create and connect" })
    )
    expect(
      await within(dialog).findByText("Turn on at least one thing to sync.")
    ).toBeTruthy()
    expect(calls("POST")).toHaveLength(0)

    fireEvent.click(within(dialog).getByLabelText("Gmail"))
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Create and connect" })
    )
    await waitFor(() =>
      expect(calls("POST").some((c) => c.url.endsWith("/oauth/start"))).toBe(
        true
      )
    )
    const posts = calls("POST")
    expect(posts[0].url).toBe("/api/v1/records")
    expect(posts[0].body).toMatchObject({
      kind: ACCOUNT,
      properties: { enabledGmail: true, syncFrequency: "daily" },
    })
    expect(posts[1].body).toEqual({ record: "new-acct" })
    expect(open).toHaveBeenCalledWith("about:blank", "_blank")
    await waitFor(() => expect(tab.location.href).toBe(CONSENT_URL))
  })

  it("with the credentials missing, Add account says so and offers the credentials form", async () => {
    serve({
      ...STATUS,
      setup: [
        {
          code: "oauth-client",
          input: "client",
          kind: CONFIG,
          record: "default",
          message: "set clientId and clientSecret",
        },
      ],
    })
    renderPage(<ConnectionsPage />)
    const dialog = await openAddAccount()
    expect(
      within(dialog).getByText(/credentials are not set up yet/)
    ).toBeTruthy()
    expect(within(dialog).getByRole("button", { name: "Create" })).toBeTruthy()
    expect(
      within(dialog).queryByRole("button", { name: "Create and connect" })
    ).toBeNull()
    fireEvent.click(
      within(dialog).getByRole("button", { name: /Set up credentials/ })
    )
    const credentials = await screen.findByRole("dialog")
    expect(
      await within(credentials).findByText(/google credentials/)
    ).toBeTruthy()
    expect(within(credentials).getByText("OAuth callback URL")).toBeTruthy()
  })

  it("lists the provider with its credentials and account counts", async () => {
    renderPage(<ConnectionsPage />)
    const link = (await screen.findAllByText("google")).find(
      (el) => el.tagName === "A"
    )!
    const card = link.closest<HTMLElement>("div.rounded-md")!
    expect(within(card).getByText("credentials set")).toBeTruthy()
    expect(within(card).getByText("1 connected, 1 pending")).toBeTruthy()
    expect(within(card).getByText(GOOGLE)).toBeTruthy()
    // The header counts what is on the page.
    expect(
      screen.getByText(/1 provider, 2 accounts, 1 needing a hand/)
    ).toBeTruthy()
  })

  it("renders one row per account with token, sync, last run and health", async () => {
    renderPage(<ConnectionsPage />)
    const work = await rowOf("george@example.com")
    expect(within(work).getByText("connected")).toBeTruthy()
    expect(within(work).getByText("erroring")).toBeTruthy()
    expect(
      within(work).getByText("ok (256 pending: queue x118, rate limited)")
    ).toBeTruthy()
    expect(within(work).getByText("2h ago")).toBeTruthy()
    expect(within(work).getByText("hourly")).toBeTruthy()
    expect(within(work).getByText("last30d")).toBeTruthy()
    expect(within(work).getByText("1 parked")).toBeTruthy()
    expect(within(work).getByText("requested")).toBeTruthy()
    expect(within(work).getByRole("img", { name: /broken/ })).toBeTruthy()

    const home = await rowOf("home@example.com")
    expect(within(home).getByText("pending")).toBeTruthy()
    // Each row's parked count is its own, off the status read.
    expect(within(home).getByText("none parked")).toBeTruthy()
    // The chip says `never` (no syncState yet) and so does the last-synced
    // cell.
    expect(within(home).getAllByText("never").length).toBe(2)
    expect(
      within(home).getByRole("img", { name: /needs attention/ })
    ).toBeTruthy()
    expect(within(home).getByRole("button", { name: "Connect" })).toBeTruthy()
    expect(within(work).getByRole("button", { name: "Reconnect" })).toBeTruthy()
  })

  it("Sync now stamps the request, then wakes the on-request trigger", async () => {
    renderPage(<ConnectionsPage />)
    const work = await rowOf("george@example.com")
    fireEvent.click(within(work).getByRole("button", { name: /Sync now/ }))
    await waitFor(() => expect(calls("POST").length).toBe(1))
    const [patch] = calls("PATCH")
    expect(patch.url).toBe(
      `/api/v1/providers.substrate.reamde.dev/google/account/george-work`
    )
    const props = (patch.body as { properties: Record<string, unknown> })
      .properties
    expect(typeof props.syncRequestedAt).toBe("string")
    expect(calls("POST")[0].url).toBe(
      "/api/v1/substrate.reamde.dev/core/trigger/google-gmail-on-request/wake"
    )
    await screen.findByText("Sync requested")
  })

  it("Pause patches syncPaused", async () => {
    renderPage(<ConnectionsPage />)
    const work = await rowOf("george@example.com")
    fireEvent.click(
      within(work).getByRole("button", {
        name: /More actions for george@example.com/,
      })
    )
    fireEvent.click(await screen.findByText("Pause sync"))
    await waitFor(() => expect(calls("PATCH").length).toBe(1))
    expect(calls("PATCH")[0].body).toEqual({
      properties: { syncPaused: true },
    })
  })

  it("Disconnect confirms, then deletes the record", async () => {
    renderPage(<ConnectionsPage />)
    const home = await rowOf("home@example.com")
    fireEvent.click(
      within(home).getByRole("button", {
        name: /More actions for home@example.com/,
      })
    )
    fireEvent.click(await screen.findByText("Disconnect"))
    const dialog = await screen.findByRole("dialog")
    fireEvent.click(within(dialog).getByRole("button", { name: "Disconnect" }))
    await waitFor(() => expect(calls("DELETE").length).toBe(1))
    expect(calls("DELETE")[0].url).toBe(
      "/api/v1/providers.substrate.reamde.dev/google/account/george-home"
    )
  })
})
