// @vitest-environment jsdom
/** The Providers list: one card per shipped provider, each saying in one
 * pill where it stands or offering to add it. Adding takes the whole
 * requirement chain, leaves first, through each package's own door, stops at
 * the first refusal and names it, and asks before replacing a package the
 * reader edited. Technical mode adds the other packages this repository
 * holds, with their updates, and states a shipped upgrade that has not
 * landed. */

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
  ShippedUpgrade,
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

let search: { connected?: string; error?: string } = {}
vi.mock("@/router", () => ({
  providersRoute: { useSearch: () => search },
}))

import { ProvidersPage } from "./providers"

const CATALOG_PATH = "/api/v1/catalog"
const SHIPPED_PATH = "/api/v1/vocabulary/upgrade"
const STATUS_PATH = "/api/v1/substrate.reamde.dev/core/bundle/status"
const HOME = "ada.example.com"
const GOOGLE_ID = "providers.substrate.reamde.dev/google"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

const EMPTY = {
  traits: null,
  triggers: null,
  kinds: null,
  functions: null,
  agents: null,
  mappings: null,
  records: null,
}

function bundle(over: Partial<CatalogItem>): CatalogItem {
  return {
    id: "x.example.com/x",
    name: "x",
    authority: "x.example.com",
    package: "x",
    description: "",
    version: 1,
    tier: "provider",
    closure: EMPTY,
    installed: false,
    ...over,
  }
}

function sample(pkg: string, requires?: string[]): CatalogItem {
  return bundle({
    id: `samples.substrate.reamde.dev/${pkg}`,
    name: pkg,
    authority: "samples.substrate.reamde.dev",
    package: pkg,
    tier: "sample",
    requires,
  })
}

const PEOPLE = sample("people")

/** Two providers GOOGLE declares against, one needing the other: the chain
 * the wire does not carry whole. */
const SHARED = bundle({
  id: "providers.substrate.reamde.dev/shared",
  name: "shared",
  authority: "providers.substrate.reamde.dev",
  package: "shared",
  requires: ["providers.substrate.reamde.dev/base"],
})
const BASE = bundle({
  id: "providers.substrate.reamde.dev/base",
  name: "base",
  authority: "providers.substrate.reamde.dev",
  package: "base",
})

const GOOGLE = bundle({
  id: GOOGLE_ID,
  name: "google",
  authority: "providers.substrate.reamde.dev",
  package: "google",
  description: "Keeps a copy of your Google contacts. Read-only.",
  inputs: {
    client: { kind: `${GOOGLE_ID}/config` },
  },
  requires: ["providers.substrate.reamde.dev/shared"],
})

const LINEAR = bundle({
  id: "providers.substrate.reamde.dev/linear",
  name: "linear",
  authority: "providers.substrate.reamde.dev",
  package: "linear",
  description: "Issues and your team.",
})

function status(over: Partial<BundleStatus>): BundleStatus {
  return {
    id: "x",
    name: "x",
    authority: "x",
    package: "x",
    installed: true,
    enabled: true,
    accounts: 0,
    functions: 0,
    kinds: 0,
    liveRecords: 0,
    ...over,
  }
}

function googleStatus(over: Partial<BundleStatus> = {}): BundleStatus {
  return status({
    id: GOOGLE_ID,
    name: "google",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    inputs: [{ name: "client", kind: `${GOOGLE_ID}/config` }],
    setup: [
      {
        code: "missing",
        input: "client",
        kind: `${GOOGLE_ID}/config`,
        message: "no config record exists yet",
      },
    ],
    ...over,
  })
}

function peopleStatus(over: Partial<BundleStatus> = {}): BundleStatus {
  return status({
    id: `${HOME}/people`,
    name: "people",
    authority: HOME,
    package: "people",
    version: 4,
    origin: PEOPLE.id,
    ...over,
  })
}

function kind(identity: string, traits: string[]): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "published",
    description: "",
    definition: { traits, properties: {}, names: { singular: name } },
  }
}

const KINDS = [
  kind(`${GOOGLE_ID}/config`, ["substrate.reamde.dev/core/oauth2"]),
  kind(`${GOOGLE_ID}/account`, [
    "substrate.reamde.dev/core/accountconfig",
    "substrate.reamde.dev/core/sync",
  ]),
]

interface Wire {
  statuses?: BundleStatus[]
  catalog?: CatalogItem[]
  shipped?: ShippedUpgrade[]
  fresh?: (id: string) => CatalogItem | undefined
  take?: (id: string, verb: string) => Response
  triggerStatuses?: TriggerStatus[]
}

function listedKinds(path: string): string[] {
  const url = new URL(path, "http://x")
  if (url.pathname !== "/api/v1/records") return []
  const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
  }
  return filter.kinds ?? []
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

describe("ProvidersPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  function serve(wire: Wire = {}) {
    fetchMock.mockImplementation(async (url, init) => {
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      const path = String(url)
      const catalog = wire.catalog ?? [PEOPLE, BASE, SHARED, GOOGLE, LINEAR]
      if (path === STATUS_PATH) {
        return jsonResponse(200, { items: wire.statuses ?? [] })
      }
      if (listedKinds(path).includes("substrate.reamde.dev/core/repository")) {
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
      if (listedKinds(path).includes("substrate.reamde.dev/core/kind")) {
        return jsonResponse(200, { kinds: KINDS })
      }
      if (path === CATALOG_PATH) return jsonResponse(200, { items: catalog })
      if (path === "/api/v1/substrate.reamde.dev/core/trigger/status") {
        return jsonResponse(200, { items: wire.triggerStatuses ?? [] })
      }
      if (path === SHIPPED_PATH) {
        return jsonResponse(200, { items: wire.shipped ?? [] })
      }
      if (path.startsWith(`${CATALOG_PATH}/`) && method === "GET") {
        const id = decodeURIComponent(path.slice(CATALOG_PATH.length + 1))
        const found = wire.fresh?.(id) ?? catalog.find((i) => i.id === id)
        return found
          ? jsonResponse(200, found)
          : jsonResponse(404, { error: { code: "not_found", message: id } })
      }
      if (
        (path.endsWith("/install") || path.endsWith("/import")) &&
        method === "POST"
      ) {
        const verb = path.endsWith("/install") ? "install" : "import"
        const id = decodeURIComponent(
          path.slice(CATALOG_PATH.length + 1, -(verb.length + 1))
        )
        const pkg = id.split("/").pop()!
        return (
          wire.take?.(id, verb) ??
          jsonResponse(
            200,
            status({
              id: verb === "import" ? `${HOME}/${pkg}` : id,
              name: pkg,
              package: pkg,
            })
          )
        )
      }
      if (path.startsWith("/api/v1/changes")) {
        return jsonResponse(404, { error: { code: "not_found", message: "" } })
      }
      return jsonResponse(200, { records: [], items: [] })
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

  /** The catalog doors the page went through, in order. */
  function doors(): string[] {
    return fetchMock.mock.calls
      .filter(
        ([url, init]) =>
          /\/(install|import)$/.test(String(url)) &&
          (init as RequestInit | undefined)?.method === "POST"
      )
      .map(([url]) => {
        const u = String(url)
        const verb = u.endsWith("/install") ? "install" : "import"
        return `${verb} ${decodeURIComponent(
          u.slice(CATALOG_PATH.length + 1, -(verb.length + 1))
        )}`
      })
  }

  function bodies(): { confirm?: unknown }[] {
    return fetchMock.mock.calls
      .filter(
        ([url, init]) =>
          /\/(install|import)$/.test(String(url)) &&
          (init as RequestInit | undefined)?.method === "POST"
      )
      .map(([, init]) => {
        const raw = (init as RequestInit | undefined)?.body
        return raw ? JSON.parse(String(raw)) : {}
      })
  }

  async function card(name: string): Promise<HTMLElement> {
    const link = await screen.findByText(name, { selector: "a" })
    return link.closest<HTMLElement>('[data-slot="provider-card"]')!
  }

  it("says what the page is for", async () => {
    renderPage(<ProvidersPage />)
    expect(
      await screen.findByText(/Services that bring your data in/)
    ).toBeTruthy()
  })

  it("says how an account's connect came back when it lands here", async () => {
    search = { error: "c-4812" }
    renderPage(<ProvidersPage />)
    const note = await screen.findByRole("status")
    expect(note.textContent).toContain("Connecting the account failed")
    expect(note.textContent).toContain("c-4812")
    fireEvent.click(within(note).getByRole("button", { name: "Dismiss" }))
    expect(navigate).toHaveBeenCalledWith(
      expect.objectContaining({ to: "/providers", search: {} })
    )
    search = {}
  })

  it("shows one card per provider, never a sample", async () => {
    renderPage(<ProvidersPage />)
    await card("Google")
    expect(screen.getByText("Linear", { selector: "a" })).toBeTruthy()
    expect(screen.queryByText("People", { selector: "a" })).toBeNull()
  })

  it("offers to add a provider that is not here, and links its page", async () => {
    renderPage(<ProvidersPage />)
    const linear = await card("Linear")
    expect(within(linear).getByRole("button", { name: "Add" })).toBeTruthy()
    expect(within(linear).getByText("Not added")).toBeTruthy()
    expect(within(linear).getByText("Issues and your team.")).toBeTruthy()
    const link = within(linear).getByText("Linear", { selector: "a" })
    expect(link.getAttribute("data-to")).toBe("/providers/$authority/$pkg")
    expect(JSON.parse(link.getAttribute("data-params")!)).toEqual({
      authority: "providers.substrate.reamde.dev",
      pkg: "linear",
    })
  })

  it("names the step a provider that is here has reached", async () => {
    serve({ statuses: [googleStatus()] })
    renderPage(<ProvidersPage />)
    const google = await card("Google")
    expect(within(google).getByText("Set up")).toBeTruthy()
    expect(within(google).getByText("Almost there · step 2 of 4")).toBeTruthy()
    // The first sentence of the catalog's description, and no more.
    expect(
      within(google).getByText("Keeps a copy of your Google contacts.")
    ).toBeTruthy()
    expect(within(google).queryByRole("button", { name: "Add" })).toBeNull()
  })

  it("says Paused for a provider that is switched off", async () => {
    serve({ statuses: [googleStatus({ enabled: false })] })
    renderPage(<ProvidersPage />)
    const google = await card("Google")
    expect(within(google).getByText("Paused")).toBeTruthy()
  })

  it("says a provider that failed to load needs attention", async () => {
    serve({
      statuses: [
        googleStatus({
          installed: false,
          quarantined: true,
          quarantineReason: "bad closure",
        }),
      ],
    })
    renderPage(<ProvidersPage />)
    const google = await card("Google")
    expect(within(google).getByText("Needs attention")).toBeTruthy()
    expect(within(google).getByText("It failed to load")).toBeTruthy()
  })

  it("says on the card why a provider needs attention", async () => {
    serve({
      statuses: [googleStatus()],
      triggerStatuses: [
        {
          id: "google-gmail-scheduled",
          kind: "schedule",
          callable: `${GOOGLE_ID}/syncgmail`,
          enabled: true,
          head: 3,
          parked: 2,
          pending: 0,
        },
        {
          id: "linear-issues-scheduled",
          kind: "schedule",
          callable: "providers.substrate.reamde.dev/linear/syncissues",
          enabled: true,
          head: 3,
          parked: 5,
          pending: 0,
        },
      ],
    })
    renderPage(<ProvidersPage />)
    const google = await card("Google")
    expect(within(google).getByText("Needs attention")).toBeTruthy()
    // Only its own triggers count.
    expect(
      await within(google).findByText(
        "2 runs failed and are waiting to be retried"
      )
    ).toBeTruthy()
  })

  it("shows the bundle id on the card in technical mode only", async () => {
    renderPage(<ProvidersPage />, true)
    const google = await card("Google")
    expect(within(google).getByText(GOOGLE_ID)).toBeTruthy()
    cleanup()
    renderPage(<ProvidersPage />)
    expect(within(await card("Google")).queryByText(GOOGLE_ID)).toBeNull()
  })

  it("adds the whole chain leaves first, each through its own door, then opens the provider", async () => {
    renderPage(<ProvidersPage />)
    const google = await card("Google")
    fireEvent.click(within(google).getByRole("button", { name: "Add" }))
    await waitFor(() => expect(doors()).toHaveLength(3))
    expect(doors()).toEqual([
      `install ${BASE.id}`,
      `install ${SHARED.id}`,
      `install ${GOOGLE_ID}`,
    ])
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/providers/$authority/$pkg",
        params: { authority: "providers.substrate.reamde.dev", pkg: "google" },
        hash: undefined,
      })
    )
  })

  it("stops at the first refusal, names it, and lists every problem on its own line", async () => {
    const problems = [
      "shared declares a retired name",
      "shared names a kind this repository does not have",
    ]
    serve({
      take: (id) =>
        id.endsWith("/shared")
          ? jsonResponse(422, {
              error: { code: "validation", message: "validation", problems },
            })
          : jsonResponse(200, peopleStatus()),
    })
    renderPage(<ProvidersPage />)
    fireEvent.click(
      within(await card("Google")).getByRole("button", { name: "Add" })
    )
    expect(await screen.findByText("Adding shared failed")).toBeTruthy()
    const first = await screen.findByText(problems[0])
    const second = await screen.findByText(problems[1])
    expect(first.tagName).toBe("LI")
    expect(first.parentElement).toBe(second.parentElement)
    expect(doors()).toEqual([`install ${BASE.id}`, `install ${SHARED.id}`])
    expect(navigate).not.toHaveBeenCalled()
  })

  it("asks before replacing an edited package, and confirms the plan read just before it", async () => {
    const edited = (planHash: string, changelogSeq: number): CatalogItem => ({
      ...BASE,
      upgrade: {
        available: false,
        work: 0,
        lossy: false,
        discardsEdits: true,
        planHash,
        changelogSeq,
      },
    })
    serve({
      catalog: [edited("stale", 1), SHARED, GOOGLE],
      fresh: (id) => (id === BASE.id ? edited("fresh", 9) : undefined),
    })
    renderPage(<ProvidersPage />)
    fireEvent.click(
      within(await card("Google")).getByRole("button", { name: "Add" })
    )
    const dialog = await screen.findByRole("dialog")
    expect(
      within(dialog).getByText(/Replace your edits to base\?/)
    ).toBeTruthy()
    expect(doors()).toEqual([])
    fireEvent.click(within(dialog).getByRole("button", { name: "Add" }))
    await waitFor(() => expect(bodies().length).toBeGreaterThan(0))
    expect(bodies()[0].confirm).toEqual({ planHash: "fresh", changelogSeq: 9 })
  })

  it("refuses a chain it cannot order, and adds nothing", async () => {
    const a = bundle({
      id: "samples.substrate.reamde.dev/aaa",
      name: "aaa",
      package: "aaa",
      tier: "sample",
      requires: ["samples.substrate.reamde.dev/bbb"],
    })
    const b = bundle({
      id: "samples.substrate.reamde.dev/bbb",
      name: "bbb",
      package: "bbb",
      tier: "sample",
      requires: ["samples.substrate.reamde.dev/aaa"],
    })
    serve({ catalog: [a, b, { ...LINEAR, requires: [a.id] }] })
    renderPage(<ProvidersPage />)
    fireEvent.click(
      within(await card("Linear")).getByRole("button", { name: "Add" })
    )
    expect(
      await screen.findByText("Linear cannot be added from here")
    ).toBeTruthy()
    expect(doors()).toEqual([])
  })

  it("says an update is available on the card", async () => {
    serve({
      statuses: [googleStatus()],
      catalog: [
        {
          ...GOOGLE,
          installed: true,
          upgrade: { available: true, from: 1, to: 2, work: 0, lossy: false },
        },
      ],
    })
    renderPage(<ProvidersPage />)
    expect(
      within(await card("Google")).getByText("Update available")
    ).toBeTruthy()
  })

  describe("other packages (technical)", () => {
    function heldPeople(upgrade?: CatalogItem["upgrade"]): CatalogItem {
      return { ...PEOPLE, installed: true, upgrade }
    }

    it("is not shown in everyday mode", async () => {
      serve({ statuses: [peopleStatus()], catalog: [heldPeople(), GOOGLE] })
      renderPage(<ProvidersPage />)
      await card("Google")
      expect(screen.queryByText("Other packages")).toBeNull()
    })

    it("lists every held package that is not a provider, with where it came from", async () => {
      serve({
        statuses: [
          peopleStatus({ modified: true }),
          status({
            id: `${HOME}/broken`,
            name: "broken",
            package: "broken",
            installed: false,
            quarantined: true,
            quarantineReason: "kind x: unknown key",
          }),
        ],
        catalog: [heldPeople(), GOOGLE],
      })
      renderPage(<ProvidersPage />, true)
      expect(await screen.findByText("Other packages")).toBeTruthy()
      const rows = screen
        .getAllByRole("row")
        .filter((r) => r.getAttribute("data-slot") === "other-package")
      expect(rows).toHaveLength(2)
      const people = rows.find((r) => r.textContent?.includes("People"))!
      expect(within(people).getByText(`${HOME}/people`)).toBeTruthy()
      expect(within(people).getByText("Sample")).toBeTruthy()
      expect(within(people).getByText(PEOPLE.id)).toBeTruthy()
      expect(within(people).getByText(/edited since/)).toBeTruthy()
      expect(within(people).getByText("4")).toBeTruthy()
      const broken = rows.find((r) => r.textContent?.includes("Broken"))!
      expect(within(broken).getByText("Failed to load")).toBeTruthy()
      expect(within(broken).getByText("kind x: unknown key")).toBeTruthy()
    })

    it("updates a moved sample copy through the import door", async () => {
      serve({
        statuses: [peopleStatus()],
        catalog: [
          heldPeople({
            available: true,
            from: 4,
            to: 5,
            work: 0,
            lossy: false,
          }),
          GOOGLE,
        ],
      })
      renderPage(<ProvidersPage />, true)
      fireEvent.click(await screen.findByRole("button", { name: /Update/ }))
      await waitFor(() => expect(doors()).toEqual([`import ${PEOPLE.id}`]))
      expect(bodies()[0].confirm).toBeUndefined()
    })

    it("asks before an update replaces an edited copy, and confirms the previewed plan", async () => {
      serve({
        statuses: [peopleStatus({ modified: true })],
        catalog: [
          heldPeople({
            available: true,
            from: 4,
            to: 5,
            work: 0,
            lossy: false,
            discardsEdits: true,
            planHash: "d15c",
            changelogSeq: 9,
          }),
          GOOGLE,
        ],
      })
      renderPage(<ProvidersPage />, true)
      fireEvent.click(await screen.findByRole("button", { name: /Update/ }))
      const dialog = await screen.findByRole("dialog")
      expect(
        within(dialog).getByText(/Update People and replace your edits\?/)
      ).toBeTruthy()
      expect(doors()).toEqual([])
      fireEvent.click(
        within(dialog).getByRole("button", {
          name: /Update and replace my edits/,
        })
      )
      await waitFor(() =>
        expect(bodies()).toEqual([
          { confirm: { planHash: "d15c", changelogSeq: 9 } },
        ])
      )
    })

    it("states a blocked update instead of offering it", async () => {
      serve({
        statuses: [peopleStatus()],
        catalog: [
          heldPeople({
            available: true,
            from: 4,
            to: 5,
            work: 0,
            lossy: false,
            blockers: ["property x dropped while 3 live records carry it"],
          }),
          GOOGLE,
        ],
      })
      renderPage(<ProvidersPage />, true)
      expect(await screen.findByText("Update blocked")).toBeTruthy()
      expect(screen.queryByRole("button", { name: /^Update$/ })).toBeNull()
    })

    it("states a refused core boot upgrade with the server's lines", async () => {
      const guard = 'property "label" dropped while 1 live records carry it'
      serve({
        shipped: [
          {
            package: "substrate.reamde.dev/core",
            upgrade: {
              available: true,
              from: 16,
              to: 17,
              blockers: [guard],
              work: 0,
              lossy: false,
            },
          },
        ],
      })
      renderPage(<ProvidersPage />, true)
      const notice = await screen.findByRole("alert")
      expect(within(notice).getByText("substrate.reamde.dev/core")).toBeTruthy()
      expect(within(notice).getByText(guard)).toBeTruthy()
      expect(within(notice).getByText(/was refused/)).toBeTruthy()
    })

    it("says a restart lands an admitted core upgrade, and nothing when current", async () => {
      serve({
        shipped: [
          {
            package: "substrate.reamde.dev/core",
            upgrade: {
              available: true,
              from: 16,
              to: 17,
              work: 0,
              lossy: false,
            },
          },
        ],
      })
      renderPage(<ProvidersPage />, true)
      expect(
        within(await screen.findByRole("alert")).getByText(
          /lands when the server starts again/
        )
      ).toBeTruthy()
      cleanup()
      serve({
        shipped: [
          {
            package: "substrate.reamde.dev/core",
            upgrade: {
              available: false,
              from: 17,
              to: 17,
              work: 0,
              lossy: false,
            },
          },
        ],
      })
      renderPage(<ProvidersPage />, true)
      await screen.findByText("Other packages")
      expect(screen.queryByRole("alert")).toBeNull()
    })
  })
})
