// @vitest-environment jsdom
/** One provider's page, over Google with two accounts: the four set-up
 * steps say which one is current and do it (sign-in details, then an
 * account, created and connected in one press); the accounts say how their
 * sync is doing in plain words and take the existing verbs (Sync now is a
 * PATCH of `syncRequestedAt` then a wake of the on-request trigger, Pause a
 * PATCH of `syncPaused`, Disconnect a DELETE after a confirmation); the
 * header pauses and removes the provider, each confirmed; and the same
 * address serves any other package this repository holds. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
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
import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import type {
  BundleStatus,
  CatalogItem,
  KindInfo,
  SubstrateRecord,
  TriggerStatus,
} from "@/lib/api/types"

const navigate = vi.fn().mockResolvedValue(undefined)
let params = { authority: "providers.substrate.reamde.dev", pkg: "google" }
let search: { account?: string; connected?: string; error?: string } = {}

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => navigate,
  Link: ({
    to,
    params: linkParams,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(linkParams ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

vi.mock("@/router", () => ({
  providerRoute: { useParams: () => params, useSearch: () => search },
}))

import { ProviderPage } from "./provider"

const GOOGLE = "providers.substrate.reamde.dev/google"
const ACCOUNT = `${GOOGLE}/account`
const CONFIG = `${GOOGLE}/config`
const CONTACT = `${GOOGLE}/contact`
const HOME = "ada.example.com"
const STATUS_PATH = "/api/v1/substrate.reamde.dev/core/bundle/status"
const BUNDLE_PATH = "/api/v1/substrate.reamde.dev/core/bundle"
const CATALOG_PATH = "/api/v1/catalog"
const CONSENT_URL = "https://accounts.google.com/o/oauth2/v2/auth?x=1"
const HOUR_AGO = new Date(Date.now() - 2 * 3600_000).toISOString()

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

function kind(
  identity: string,
  traits: string[],
  properties: Record<string, unknown> = {},
  purpose?: string
): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "published",
    description: `the ${name} kind`,
    definition: {
      traits,
      properties,
      names: { singular: name },
      ...(purpose && { purpose }),
    },
  }
}

const KINDS: KindInfo[] = [
  kind(
    ACCOUNT,
    [
      "substrate.reamde.dev/core/accountconfig",
      "substrate.reamde.dev/core/sync",
    ],
    {
      email: { type: "email", writer: "oauth" },
      enabledGmail: { type: "bool", writer: "owner", displayName: "Gmail" },
      enabledContacts: {
        type: "bool",
        writer: "owner",
        displayName: "Contacts",
      },
      syncFrequency: {
        type: "enum",
        values: [
          { value: "hourly", label: "Every hour" },
          { value: "daily", label: "Every day" },
        ],
        default: "daily",
        writer: "owner",
      },
      syncState: { type: "string", writer: "connector" },
      syncPaused: { type: "bool", writer: "owner", displayName: "Paused" },
      syncRequestedAt: { type: "datetime", writer: "owner" },
    },
    "internal"
  ),
  kind(
    CONFIG,
    ["substrate.reamde.dev/core/oauth2"],
    {
      clientId: { type: "string", displayName: "Client ID" },
      clientSecret: { type: "secret", displayName: "Client secret" },
    },
    "internal"
  ),
  kind(CONTACT, [], {}),
  kind(`${GOOGLE}/contactgroup`, [], {}, "supporting"),
  kind(`${HOME}/people/person`, [], {}),
]

function status(over: Partial<BundleStatus> = {}): BundleStatus {
  return {
    id: GOOGLE,
    name: "google",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    installed: true,
    enabled: true,
    version: 34,
    inputs: [
      { name: "client", kind: CONFIG, record: "default", via: "default" },
    ],
    accounts: 2,
    functions: 1,
    kinds: 4,
    liveRecords: 4210,
    ...over,
  }
}

const MISSING_CLIENT: Partial<BundleStatus> = {
  inputs: [{ name: "client", kind: CONFIG }],
  setup: [
    {
      code: "missing",
      input: "client",
      kind: CONFIG,
      message: "no config record exists yet",
    },
  ],
}

const CATALOG: CatalogItem = {
  id: GOOGLE,
  name: "google",
  authority: "providers.substrate.reamde.dev",
  package: "google",
  description: "Keeps a copy of your Google contacts.",
  version: 34,
  tier: "provider",
  installed: true,
  closure: {
    kinds: [ACCOUNT, CONFIG, CONTACT, `${GOOGLE}/contactgroup`],
    traits: null,
    functions: [`${GOOGLE}/synccontacts`],
    agents: null,
    mappings: null,
    records: null,
    triggers: null,
  },
}

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
  enabledGmail: true,
  syncFrequency: "hourly",
  syncState: "erroring",
  syncError: "gmail: HTTP 403",
  lastSyncedAt: HOUR_AGO,
})

const PERSONAL = account("george-home", {
  email: "home@example.com",
  tokenStatus: "pending",
  syncFrequency: "daily",
})

function trigger(id: string, source: Record<string, unknown>): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/core/trigger",
    properties: {
      enabled: true,
      source,
      callable: {
        ref: `substrate.reamde.dev/core/function/${GOOGLE}/synccontacts`,
      },
    },
    labels: {},
    version: 1,
    createdAt: HOUR_AGO,
    updatedAt: HOUR_AGO,
  }
}

const TRIGGERS = [
  trigger("google-contacts-scheduled", {
    schedule: { recurrence: "FREQ=HOURLY" },
  }),
  trigger("google-contacts-on-request", {
    record: {
      kinds: [ACCOUNT],
      ops: ["update"],
      when: "record.properties.syncRequestedAt != ''",
    },
  }),
]

const TRIGGER_STATUSES: TriggerStatus[] = [
  {
    id: "google-contacts-scheduled",
    kind: "schedule",
    callable: `${GOOGLE}/synccontacts`,
    enabled: true,
    head: 42,
    lastFire: HOUR_AGO,
    parked: 0,
    pending: 0,
  },
]

const MAPPING: SubstrateRecord = {
  id: `${HOME}/people/googlecontactperson`,
  kind: "substrate.reamde.dev/core/recordmapping",
  properties: {
    from: { ref: `substrate.reamde.dev/core/kind/${CONTACT}` },
    to: { ref: `substrate.reamde.dev/core/kind/${HOME}/people/person` },
  },
  labels: {},
  version: 1,
  createdAt: HOUR_AGO,
  updatedAt: HOUR_AGO,
}

interface Wire {
  statuses?: BundleStatus[]
  catalog?: CatalogItem[]
  accounts?: SubstrateRecord[]
  settings?: SubstrateRecord[]
  triggerStatuses?: TriggerStatus[]
}

function filterOf(path: string): { kinds?: string[]; implements?: string } {
  const url = new URL(path, "http://x")
  if (url.pathname !== "/api/v1/records") return {}
  return JSON.parse(url.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
    implements?: string
  }
}

function renderPage(ui: ReactElement, technical = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: technical,
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <Toaster>{ui}</Toaster>
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
}

describe("ProviderPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  function serve(wire: Wire = {}) {
    fetchMock.mockImplementation(async (url, init) => {
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      const path = String(url)
      const filter = filterOf(path)
      if (path === STATUS_PATH) {
        return jsonResponse(200, { items: wire.statuses ?? [status()] })
      }
      if (path === CATALOG_PATH) {
        return jsonResponse(200, { items: wire.catalog ?? [CATALOG] })
      }
      if (path === "/api/v1/vocabulary/upgrade") {
        return jsonResponse(200, { items: [] })
      }
      if (path === "/api/v1/substrate.reamde.dev/core/trigger/status") {
        return jsonResponse(200, {
          items: wire.triggerStatuses ?? TRIGGER_STATUSES,
        })
      }
      if (path === "/api/v1/sync/status") {
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
          ],
        })
      }
      if (filter.kinds?.includes("substrate.reamde.dev/core/repository")) {
        return jsonResponse(200, {
          records: [
            {
              id: "r_1",
              kind: "substrate.reamde.dev/core/repository",
              properties: { name: "ada", authority: HOME },
            },
          ],
        })
      }
      if (filter.kinds?.includes("substrate.reamde.dev/core/kind"))
        return jsonResponse(200, { kinds: KINDS })
      if (filter.implements === "substrate.reamde.dev/core/accountconfig")
        return jsonResponse(200, {
          records: wire.accounts ?? [WORK, PERSONAL],
        })
      if (filter.kinds?.includes("substrate.reamde.dev/core/trigger"))
        return jsonResponse(200, { records: TRIGGERS })
      if (filter.kinds?.includes("substrate.reamde.dev/core/recordmapping"))
        return jsonResponse(200, { records: [MAPPING] })
      if (filter.kinds?.includes("substrate.reamde.dev/core/setting"))
        return jsonResponse(200, { records: wire.settings ?? [] })
      if (path.startsWith("/api/v1/changes")) {
        return jsonResponse(404, { error: { code: "not_found", message: "" } })
      }
      if (method === "PATCH" && path.startsWith(BUNDLE_PATH)) {
        const body = JSON.parse(String((init as RequestInit).body)) as {
          properties: Record<string, unknown>
        }
        if ("disabled" in body.properties)
          return jsonResponse(
            200,
            status({ enabled: !body.properties.disabled })
          )
        if ("purging" in body.properties)
          return jsonResponse(200, { purged: 4210 })
        return jsonResponse(200, { uninstalled: true })
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
      if (method === "GET" && path.endsWith("/parked"))
        return jsonResponse(200, {
          items: [
            {
              id: 7,
              trigger: "google-contacts-scheduled",
              attempts: 5,
              lastError: "HTTP 500",
              parkedAt: HOUR_AGO,
            },
            {
              id: 8,
              trigger: "google-contacts-scheduled",
              attempts: 5,
              lastError: "HTTP 500",
              parkedAt: HOUR_AGO,
            },
          ],
        })
      if (method === "POST" && path.endsWith("/retry"))
        return jsonResponse(200, { ran: 1 })
      if (method === "POST" && path.endsWith("/wake"))
        return jsonResponse(200, { ran: 1 })
      if (method === "POST" && path.endsWith("/bind"))
        return jsonResponse(200, status())
      if (method === "POST" && path === "/api/v1/records") {
        const body = JSON.parse(String((init as RequestInit).body)) as {
          properties: Record<string, unknown>
        }
        return jsonResponse(201, account("new-acct", body.properties))
      }
      if (method === "POST" && path.endsWith("/oauth/start"))
        return jsonResponse(200, { url: CONSENT_URL })
      if (method === "POST" && path.endsWith("/install"))
        return jsonResponse(200, status())
      if (method === "DELETE") return new Response(null, { status: 204 })
      return jsonResponse(200, { records: [], items: [] })
    })
  }

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    navigate.mockClear()
    params = { authority: "providers.substrate.reamde.dev", pkg: "google" }
    search = {}
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

  async function step(title: string | RegExp): Promise<HTMLElement> {
    const el = await screen.findByText(title, {
      selector: '[data-slot="setup-steps"] li div div',
    })
    return el.closest("li") as HTMLElement
  }

  async function row(label: string): Promise<HTMLElement> {
    const el = await screen.findByText(label, {
      selector: '[data-slot="account-row"] div',
    })
    return el.closest("tr") as HTMLElement
  }

  describe("set-up", () => {
    it("makes the sign-in details the current step while the client is missing", async () => {
      serve({ statuses: [status(MISSING_CLIENT)], accounts: [] })
      renderPage(<ProviderPage />)
      const details = await step(/Sign-in details/)
      expect(details.getAttribute("aria-current")).toBe("step")
      expect((await step(/Add Google/)).getAttribute("data-state")).toBe("done")
      expect(
        (await step(/Connect your account/)).getAttribute("data-state")
      ).toBe("todo")
      // The redirect address to register sits in the step, with a copy.
      expect(
        within(details).getByText("Redirect address to register")
      ).toBeTruthy()
      expect(
        within(details).getByText(/\/api\/v1\/oauth\/callback$/)
      ).toBeTruthy()
      expect(screen.getByText("Step 2 of 4")).toBeTruthy()
    })

    it("opens the sign-in details form from the current step", async () => {
      serve({ statuses: [status(MISSING_CLIENT)], accounts: [] })
      renderPage(<ProviderPage />)
      fireEvent.click(
        within(await step(/Sign-in details/)).getByRole("button", {
          name: "Add details",
        })
      )
      const dialog = await screen.findByRole("dialog")
      expect(within(dialog).getByText("Google sign-in details")).toBeTruthy()
      expect(
        within(dialog).getByText("Redirect address to register")
      ).toBeTruthy()
      expect(within(dialog).getByText("not saved yet")).toBeTruthy()
    })

    it("offers to add a provider that is not here as the first step", async () => {
      serve({ statuses: [], catalog: [{ ...CATALOG, installed: false }] })
      renderPage(<ProviderPage />)
      const add = await step(/Add Google/)
      expect(add.getAttribute("aria-current")).toBe("step")
      fireEvent.click(within(add).getByRole("button", { name: "Add Google" }))
      await waitFor(() =>
        expect(
          calls("POST").some((c) =>
            c.url.endsWith(`${encodeURIComponent(GOOGLE)}/install`)
          )
        ).toBe(true)
      )
    })

    it("connects an account in one press: create, then consent in the tab opened at the press", async () => {
      const tab = { location: { href: "" }, close: vi.fn() }
      const open = vi.fn().mockReturnValue(tab)
      vi.stubGlobal("open", open)
      serve({ accounts: [] })
      renderPage(<ProviderPage />)
      const connect = await step(/Connect your account/)
      expect(connect.getAttribute("aria-current")).toBe("step")
      fireEvent.click(
        within(connect).getByRole("button", { name: "Connect an account" })
      )
      const dialog = await screen.findByRole("dialog")
      expect(within(dialog).getByText("Add a Google account")).toBeTruthy()
      expect(within(dialog).getByText("What to bring in")).toBeTruthy()
      fireEvent.click(
        within(dialog).getByRole("button", { name: "Create and connect" })
      )
      expect(
        await within(dialog).findByText(
          "Turn on at least one thing to bring in."
        )
      ).toBeTruthy()
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
      expect(posts[0].body).toMatchObject({
        kind: ACCOUNT,
        properties: { enabledGmail: true, syncFrequency: "daily" },
      })
      expect(posts[1].body).toEqual({ record: `${ACCOUNT}/new-acct` })
      expect(open).toHaveBeenCalledWith("about:blank", "_blank")
      await waitFor(() => expect(tab.location.href).toBe(CONSENT_URL))
    })

    it("says what a connected account brings in, and which is off", async () => {
      renderPage(<ProviderPage />)
      const choose = await step(/Choose what to bring in/)
      expect(choose.getAttribute("data-state")).toBe("done")
      expect(
        within(choose).getByText("george@example.com: Gmail. Contacts is off.")
      ).toBeTruthy()
    })
  })

  describe("accounts", () => {
    it("says how each account is doing in plain words", async () => {
      renderPage(<ProviderPage />)
      const work = await row("george@example.com")
      expect(
        within(work).getByText("Having trouble: gmail: HTTP 403")
      ).toBeTruthy()
      expect(within(work).getByText("2h ago")).toBeTruthy()
      expect(within(work).getByText("Every hour")).toBeTruthy()
      const home = await row("home@example.com")
      expect(
        within(home).getByText("Waiting for you to approve it at Google")
      ).toBeTruthy()
      expect(within(home).getByText("Never")).toBeTruthy()
      expect(within(home).getByRole("button", { name: "Connect" })).toBeTruthy()
      // Deliveries are technical.
      expect(within(work).queryByText("1 parked")).toBeNull()
    })

    it("shows each account's parked deliveries in technical mode", async () => {
      renderPage(<ProviderPage />, true)
      const work = await row("george@example.com")
      expect(await within(work).findByText("1 parked")).toBeTruthy()
      expect(within(work).getByText("george-work")).toBeTruthy()
    })

    it("Sync now stamps the request, then wakes the on-request trigger", async () => {
      renderPage(<ProviderPage />)
      // The trigger records say which one answers a request.
      await screen.findByText(/When you press Sync now/)
      const work = await row("george@example.com")
      fireEvent.click(within(work).getByRole("button", { name: /Sync now/ }))
      await waitFor(() => expect(calls("POST").length).toBe(1))
      const [patch] = calls("PATCH")
      expect(patch.url).toBe(`/api/v1/${ACCOUNT}/george-work`)
      expect(
        typeof (patch.body as { properties: Record<string, unknown> })
          .properties.syncRequestedAt
      ).toBe("string")
      expect(calls("POST")[0].url).toBe(
        "/api/v1/substrate.reamde.dev/core/trigger/google-contacts-on-request/wake"
      )
    })

    it("Pause patches syncPaused", async () => {
      renderPage(<ProviderPage />)
      const work = await row("george@example.com")
      fireEvent.click(within(work).getByRole("button", { name: "Pause" }))
      await waitFor(() => expect(calls("PATCH").length).toBe(1))
      expect(calls("PATCH")[0].body).toEqual({
        properties: { syncPaused: true },
      })
    })

    it("Disconnect confirms, then deletes the record", async () => {
      renderPage(<ProviderPage />)
      const home = await row("home@example.com")
      fireEvent.click(
        within(home).getByRole("button", { name: /More for home@example.com/ })
      )
      fireEvent.click(await screen.findByText("Disconnect…"))
      const dialog = await screen.findByRole("dialog")
      expect(
        within(dialog).getByText(/stops bringing anything in/)
      ).toBeTruthy()
      expect(calls("DELETE")).toHaveLength(0)
      fireEvent.click(
        within(dialog).getByRole("button", { name: "Disconnect" })
      )
      await waitFor(() => expect(calls("DELETE").length).toBe(1))
      expect(calls("DELETE")[0].url).toBe(`/api/v1/${ACCOUNT}/george-home`)
    })

    it("says the account the address names was connected", async () => {
      search = {
        account: "george-home",
        connected: `${ACCOUNT}/george-home`,
      }
      renderPage(<ProviderPage />)
      const note = await screen.findByRole("status")
      expect(note.textContent).toContain("Account connected")
      expect(note.textContent).toContain("george-home")
    })

    it("highlights the account the address names", async () => {
      search = { account: "george-home" }
      renderPage(<ProviderPage />)
      const home = await row("home@example.com")
      expect(home.getAttribute("data-highlighted")).toBe("true")
      expect(
        (await row("george@example.com")).getAttribute("data-highlighted")
      ).toBeNull()
    })
  })

  describe("what it adds", () => {
    it("lists its collections with what they fill in of yours, and counts the rest", async () => {
      renderPage(<ProviderPage />)
      expect(
        await screen.findByText(
          "1 collection you’ll see; 3 more hold supporting details"
        )
      ).toBeTruthy()
      await screen.findByText("Also fills in your")
      const rows = document.querySelectorAll('[data-slot="brings-in-row"]')
      expect(rows).toHaveLength(1)
      expect(rows[0].textContent).toContain("Contacts")
      expect(rows[0].textContent).toContain("Also fills in your")
      expect(rows[0].textContent).toContain("People")
    })

    it("lists every kind with its purpose in technical mode", async () => {
      renderPage(<ProviderPage />, true)
      await screen.findByText(/collection you’ll see/)
      expect(
        document.querySelectorAll('[data-slot="brings-in-row"]')
      ).toHaveLength(4)
      expect(screen.getAllByText("Internal")).toHaveLength(2)
      expect(screen.getByText("Supporting")).toBeTruthy()
    })

    it("links each tool to its page with when it runs", async () => {
      renderPage(<ProviderPage />)
      const tool = await screen.findByText("Google Contacts sync")
      expect(tool.getAttribute("data-to")).toBe("/tools/$authority/$pkg/$name")
      expect(JSON.parse(tool.getAttribute("data-params")!)).toEqual({
        authority: "providers.substrate.reamde.dev",
        pkg: "google",
        name: "synccontacts",
      })
      const row = tool.closest('[data-slot="tool-row"]') as HTMLElement
      expect(
        await within(row).findByText("Every hour · When you press Sync now")
      ).toBeTruthy()
      expect(within(row).getByText("Ran 2h ago")).toBeTruthy()
    })
  })

  describe("why it needs attention", () => {
    const problems = async () =>
      screen.findByRole("region", { name: "Why it needs attention" })

    it("names the account whose sync fails, its error, and the fixes", async () => {
      renderPage(<ProviderPage />)
      const callout = await problems()
      expect(
        within(callout).getByText("Syncing george@example.com is failing")
      ).toBeTruthy()
      expect(within(callout).getByText("gmail: HTTP 403")).toBeTruthy()
      expect(
        within(callout).getByRole("button", { name: "Reconnect" })
      ).toBeTruthy()
      fireEvent.click(within(callout).getByRole("button", { name: /Sync now/ }))
      await waitFor(() =>
        expect(
          fetchMock.mock.calls.some(
            ([url, init]) =>
              String(url).endsWith(`${ACCOUNT}/george-work`) &&
              (init as RequestInit | undefined)?.method === "PATCH" &&
              String((init as RequestInit).body).includes("syncRequestedAt")
          )
        ).toBe(true)
      )
    })

    it("asks to reconnect an account whose sign-in stopped working", async () => {
      serve({
        accounts: [
          account("george-work", {
            email: "george@example.com",
            tokenStatus: "erroring",
          }),
        ],
      })
      renderPage(<ProviderPage />)
      const callout = await problems()
      expect(
        within(callout).getByText(
          "The sign-in for george@example.com stopped working"
        )
      ).toBeTruthy()
      expect(
        within(callout).getByRole("button", { name: "Reconnect" })
      ).toBeTruthy()
      expect(
        within(callout).queryByRole("button", { name: /Sync now/ })
      ).toBeNull()
    })

    it("says a provider that failed to load, and why", async () => {
      serve({
        statuses: [
          status({
            installed: false,
            quarantined: true,
            quarantineReason: "kind contact: unknown key",
          }),
        ],
      })
      renderPage(<ProviderPage />)
      const callout = await problems()
      expect(within(callout).getByText("It failed to load")).toBeTruthy()
      expect(
        within(callout).getByText("kind contact: unknown key")
      ).toBeTruthy()
      expect(
        within(callout).getByRole("button", { name: "Add again" })
      ).toBeTruthy()
    })

    it("counts the parked runs and retries each of them", async () => {
      serve({
        accounts: [PERSONAL],
        triggerStatuses: [{ ...TRIGGER_STATUSES[0], parked: 2 }],
      })
      renderPage(<ProviderPage />)
      const callout = await problems()
      expect(
        within(callout).getByText("2 runs failed and are waiting to be retried")
      ).toBeTruthy()
      const retry = within(callout).getByRole("button", { name: "Retry" })
      await waitFor(() =>
        expect((retry as HTMLButtonElement).disabled).toBe(false)
      )
      fireEvent.click(retry)
      await waitFor(() => {
        const retried = fetchMock.mock.calls
          .map(([url]) => String(url))
          .filter((u) => u.endsWith("/retry"))
        expect(retried).toEqual([
          "/api/v1/substrate.reamde.dev/core/trigger/google-contacts-scheduled/parked/7/retry",
          "/api/v1/substrate.reamde.dev/core/trigger/google-contacts-scheduled/parked/8/retry",
        ])
      })
    })

    it("names what blocks an update", async () => {
      serve({
        accounts: [PERSONAL],
        catalog: [
          {
            ...CATALOG,
            upgrade: {
              available: true,
              from: 34,
              to: 35,
              blockers: ["google/contact: 3 records still hold nickname"],
            },
          } as CatalogItem,
        ],
      })
      renderPage(<ProviderPage />)
      const callout = await problems()
      expect(
        within(callout).getByText("An update is waiting on your records")
      ).toBeTruthy()
      expect(
        within(callout).getByText(
          "google/contact: 3 records still hold nickname"
        )
      ).toBeTruthy()
    })

    it("says nothing when nothing needs attention", async () => {
      serve({ accounts: [PERSONAL] })
      renderPage(<ProviderPage />)
      await screen.findByText("home@example.com")
      expect(
        screen.queryByRole("region", { name: "Why it needs attention" })
      ).toBeNull()
    })
  })

  describe("the header", () => {
    it("pauses the provider after saying what stops", async () => {
      renderPage(<ProviderPage />)
      await screen.findByText("Google", { selector: "h1" })
      const header = document.querySelector(
        '[data-slot="page-header"]'
      ) as HTMLElement
      fireEvent.click(within(header).getByRole("button", { name: "Pause" }))
      const dialog = await screen.findByRole("dialog")
      expect(within(dialog).getByText(/stops syncing/)).toBeTruthy()
      fireEvent.click(within(dialog).getByRole("button", { name: "Pause" }))
      await waitFor(() => expect(calls("PATCH").length).toBe(1))
      expect(calls("PATCH")[0]).toEqual({
        url: `${BUNDLE_PATH}/${encodeURIComponent(GOOGLE)}`,
        body: { properties: { disabled: true } },
      })
    })

    it("removes the provider after naming what goes, walking the ladder in order", async () => {
      renderPage(<ProviderPage />)
      await screen.findByText("Google", { selector: "h1" })
      fireEvent.click(screen.getByRole("button", { name: "Remove…" }))
      const dialog = await screen.findByRole("dialog")
      expect(
        within(dialog).getByText(/deletes the 4,210 records Google brought in/)
      ).toBeTruthy()
      expect(within(dialog).getByText(/cannot be undone/)).toBeTruthy()
      expect(calls("PATCH")).toHaveLength(0)
      fireEvent.click(
        within(dialog).getByRole("button", { name: "Remove Google" })
      )
      await waitFor(() => expect(calls("PATCH").length).toBe(3))
      expect(calls("PATCH").map((c) => c.body)).toEqual([
        { properties: { disabled: true } },
        { properties: { purging: true } },
        { properties: { uninstalled: true } },
      ])
      await waitFor(() =>
        expect(navigate).toHaveBeenCalledWith({ to: "/providers" })
      )
    })

    it("offers an update through the install door, asking first where it loses values", async () => {
      serve({
        catalog: [
          {
            ...CATALOG,
            upgrade: {
              available: true,
              from: 34,
              to: 35,
              work: 3,
              lossy: true,
              planHash: "cafe",
              changelogSeq: 41,
              steps: [
                {
                  step: "null",
                  kind: CONTACT,
                  property: "middleName",
                  records: 3,
                  lossy: true,
                },
              ],
            },
          },
        ],
      })
      renderPage(<ProviderPage />)
      expect(await screen.findByText("Update available")).toBeTruthy()
      fireEvent.click(screen.getByRole("button", { name: /^Update$/ }))
      const dialog = await screen.findByRole("dialog")
      expect(
        within(dialog).getByText(/Update Google and remove values\?/)
      ).toBeTruthy()
      fireEvent.click(
        within(dialog).getByRole("button", { name: /remove values/ })
      )
      await waitFor(() =>
        expect(
          calls("POST").find((c) => c.url.endsWith("/install"))?.body
        ).toEqual({ confirm: { planHash: "cafe", changelogSeq: 41 } })
      )
    })
  })

  describe("settings", () => {
    const SETTING = {
      id: `${GOOGLE}/pageSize`,
      kind: "substrate.reamde.dev/core/setting",
      properties: { displayName: "Page size", type: "int", value: "50" },
      labels: {},
      version: 1,
      createdAt: HOUR_AGO,
      updatedAt: HOUR_AGO,
    }

    it("renders the provider's own settings as a form", async () => {
      serve({ settings: [SETTING] })
      renderPage(<ProviderPage />)
      expect(await screen.findByLabelText(/Page size/)).toBeTruthy()
    })

    it("shows which record each need uses, and binds another, in technical mode only", async () => {
      serve({
        statuses: [
          status({
            inputs: [
              {
                name: "client",
                kind: CONFIG,
                record: "default",
                via: "default",
              },
              { name: "extra", kind: CONTACT, description: "a contact" },
            ],
          }),
        ],
      })
      renderPage(<ProviderPage />, true)
      const card = (await screen.findByText("extra")).closest(
        '[data-slot="input-card"]'
      ) as HTMLElement
      expect(card).toBeTruthy()
      // The credentials input is step 2's, not listed here.
      expect(
        document.querySelectorAll('[data-slot="input-card"]')
      ).toHaveLength(1)
    })
  })

  describe("another package on the same address", () => {
    const PEOPLE_STATUS: BundleStatus = {
      id: `${HOME}/people`,
      name: "people",
      authority: HOME,
      package: "people",
      installed: true,
      enabled: true,
      version: 7,
      origin: "samples.substrate.reamde.dev/people",
      accounts: 0,
      functions: 0,
      kinds: 1,
      liveRecords: 12,
    }
    const PEOPLE: CatalogItem = {
      ...CATALOG,
      id: "samples.substrate.reamde.dev/people",
      name: "people",
      authority: "samples.substrate.reamde.dev",
      package: "people",
      description: "One record per human.",
      tier: "sample",
      closure: { ...CATALOG.closure, kinds: null, functions: null },
      suggestedMappings: [
        {
          id: "samples.substrate.reamde.dev/people/googlecontactperson",
          from: CONTACT,
          to: "samples.substrate.reamde.dev/people/person",
          package: GOOGLE,
          state: "ready",
        },
      ],
    }

    it("says what it is, and offers to land its waiting links after asking", async () => {
      params = { authority: HOME, pkg: "people" }
      serve({ statuses: [status(), PEOPLE_STATUS], catalog: [CATALOG, PEOPLE] })
      renderPage(<ProviderPage />)
      expect(await screen.findByText("People", { selector: "h1" })).toBeTruthy()
      expect(screen.getByText("A sample you imported")).toBeTruthy()
      expect(screen.queryByText("Set up")).toBeNull()
      fireEvent.click(screen.getByRole("button", { name: "Import again" }))
      const dialog = await screen.findByRole("dialog")
      expect(within(dialog).getByText(/REPLACES/)).toBeTruthy()
      fireEvent.click(
        within(dialog).getByRole("button", { name: "Import again" })
      )
      await waitFor(() =>
        expect(
          calls("POST").some((c) =>
            c.url.endsWith(
              `${encodeURIComponent("samples.substrate.reamde.dev/people")}/import`
            )
          )
        ).toBe(true)
      )
    })

    it("says so when nothing is called that", async () => {
      params = { authority: HOME, pkg: "nothing" }
      renderPage(<ProviderPage />)
      expect(await screen.findByText("Nothing here")).toBeTruthy()
    })
  })
})
