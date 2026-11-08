// @vitest-environment jsdom
/** Providers as every outside service the substrate uses a key or a sign-in
 * for (decision 0233): the three groups in order, the AI model cards with
 * their key form in place, whose key each card runs on, the search line, the
 * operator's allowance on the AI models header and as a line on the Tools
 * header, the welcome where the deployment loaded an operator defaults file,
 * a typed key kept nowhere once sent, a failed operator read that never says
 * Set up, the models each card offers and what they cost (decision record
 * 0242), and the counts beside Providers that agree with the page. The operator read is answered by a mocked fetch; the catalog's
 * providers come from a stubbed `useProviders`. The site is named "Crane"
 * so every operator word is seen to come from `operator.name`. The suite
 * runs in Europe/Athens (UTC+2 in November). */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { useCallback, useMemo, useState } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { ProviderEntry } from "@/components/providers/use-providers"
import { Toaster } from "@/components/ui/toast"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { providerInfo } from "@/lib/actor-identity"
import type {
  CatalogBrings,
  OperatorDefault,
  OperatorDefaults,
  SubstrateRecord,
} from "@/lib/api/types"
import {
  DEFAULT_SETTINGS,
  applyConsoleAction,
  type ConsoleAction,
  type ConsolePreferences,
} from "@/lib/console-preferences"

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({
    to,
    params,
    search,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    search?: Record<string, string>
    children: React.ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a
      data-to={to}
      data-params={JSON.stringify(params ?? {})}
      data-search={JSON.stringify(search ?? {})}
      {...rest}
    >
      {children}
    </a>
  ),
}))

const route = vi.hoisted(
  (): { search: { model?: string; connected?: string; error?: string } } => ({
    search: {},
  })
)
vi.mock("@/router", () => ({
  providersRoute: { useSearch: () => route.search },
}))

const catalog = vi.hoisted(() => ({ providers: [] as unknown[] }))
vi.mock("@/components/providers/use-providers", () => ({
  useProviders: () => ({
    providers: catalog.providers,
    listed: catalog.providers,
    addonsOf: () => [],
    others: [],
    kinds: [],
    chains: () => [],
    pendingUpgrades: [],
    pending: false,
    error: undefined,
    refetch: () => {},
  }),
}))

import { ProviderCount } from "@/components/app-sidebar"
import { OverviewCards } from "@/components/home/overview-cards"
import { llmProvidersQueryOptions } from "@/lib/api/agents"

import {
  expectNoTrace,
  mutationText,
} from "@/hooks/use-secret-mutation.test-fixtures"

import { ProvidersPage } from "./providers"

const PROV = "providers.substrate.reamde.dev"
const LLM_PROVIDER = "substrate.reamde.dev/llm/provider"
const LLM_MODEL = "substrate.reamde.dev/llm/model"

/** A catalog provider as the page's hook hands it over. */
function provider(
  pkg: string,
  over: {
    installed?: boolean
    accountKind?: boolean
    kinds?: string[]
    functions?: string[]
    description?: string
    /** What the catalog says it brings; a closure of functions and no kinds
     * brings tools, any other an account. */
    brings?: CatalogBrings[]
  } = {}
): ProviderEntry {
  const brings: CatalogBrings[] =
    over.brings ??
    (over.kinds?.length === 0 && over.functions?.length
      ? ["tools"]
      : ["account", "collections"])
  const id = `${PROV}/${pkg}`
  const installed = over.installed ?? false
  return {
    row: {
      key: id,
      id,
      name: pkg,
      authority: PROV,
      package: pkg,
      installed,
      tier: "published",
      status: installed
        ? {
            id,
            name: pkg,
            authority: PROV,
            package: pkg,
            installed: true,
            enabled: true,
            accounts: 0,
            functions: 0,
            kinds: 0,
            liveRecords: 0,
            provider: true,
          }
        : undefined,
      catalog: {
        id,
        name: pkg,
        authority: PROV,
        package: pkg,
        description: over.description ?? "",
        version: 1,
        tier: "published",
        provider: true,
        brings,
        installed,
        closure: {
          kinds: over.kinds ?? [`${id}/account`],
          functions: over.functions ?? null,
          traits: null,
          agents: null,
          mappings: null,
          triggers: null,
          records: null,
        },
      },
    },
    info: providerInfo(pkg),
    view: installed
      ? {
          accountKind: over.accountKind
            ? { identity: `${id}/account` }
            : undefined,
        }
      : undefined,
    steps: [],
    toggles: [],
    updateAvailable: false,
    standing: installed
      ? { tone: "on", pill: "On", line: "Connected", problems: [] }
      : { tone: "add", pill: "", line: "Not added", problems: [] },
  } as unknown as ProviderEntry
}

function row(id: string, properties: Record<string, unknown> = {}) {
  return {
    id,
    kind: LLM_PROVIDER,
    version: 3,
    properties: { label: id, wire: "openai", ...properties },
  } as unknown as SubstrateRecord
}

function item(
  over: Partial<OperatorDefault> & Pick<OperatorDefault, "id">
): OperatorDefault {
  return {
    slot: "llm",
    label: over.id,
    source: "operator",
    operatorOffers: true,
    readiness: "ready",
    ...over,
  }
}

const OPENAI = item({
  id: "llm/openai",
  label: "OpenAI",
  target: `${LLM_PROVIDER}/openai`,
  models: ["gpt-5", "gpt-5-mini"],
})
const ANTHROPIC_NONE = item({
  id: "llm/anthropic",
  label: "Anthropic",
  source: "none",
  operatorOffers: false,
  readiness: "not-set-up",
})
const GEMINI_OWN = item({
  id: "llm/gemini",
  label: "Gemini",
  source: "own",
  operatorOffers: false,
})
const EMBEDDINGS = item({
  id: "embeddings",
  slot: "embeddings",
  label: "Embeddings",
  target: `${LLM_PROVIDER}/openai`,
})
const RESETS_AT = "2026-11-07T00:00:00Z"

let features: string[] = []
let answer: OperatorDefaults | { status: number } | "network" = { items: [] }
let rows: SubstrateRecord[] = []
/** The llm/model records. */
let models: SubstrateRecord[] = []
/** The installed packages the bundle status read lists. */
let statuses: unknown[] = []
/** Runs as a write reaches the server, before it answers. */
let onWrite: (() => void) | undefined
/** Holds the operator read's answer until it settles. */
let holdDefaults: Promise<void> | undefined
let functions: SubstrateRecord[] = []
const writes: { method: string; url: string; body: unknown }[] = []
const actions: ConsoleAction[] = []

function kindsOf(url: string): string[] {
  const u = new URL(url, "http://x")
  if (u.pathname !== "/api/v1/records") return []
  const filter = JSON.parse(u.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
  }
  return filter.kinds ?? []
}

beforeEach(() => {
  features = ["operatorDefaults"]
  answer = {
    operator: { name: "Crane" },
    items: [OPENAI, ANTHROPIC_NONE, GEMINI_OWN],
  }
  rows = [row("openai"), row("anthropic"), row("gemini", { apiKey: "••••" })]
  models = []
  functions = []
  catalog.providers = [
    provider("google", {
      installed: true,
      accountKind: true,
      description: "Keeps a copy of your mail.",
    }),
    provider("linear"),
  ]
  route.search = {}
  statuses = []
  onWrite = undefined
  holdDefaults = undefined
  writes.length = 0
  actions.length = 0
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      const url = String(input)
      const method = init?.method ?? "GET"
      if (method !== "GET") {
        const body = init?.body ? JSON.parse(String(init.body)) : undefined
        writes.push({ method, url, body })
        onWrite?.()
        // A patch under a version the row no longer has is refused, as the
        // server refuses it.
        const id = decodeURIComponent(url.slice(url.lastIndexOf("/") + 1))
        const held = rows.find((r) => r.id === id)
        const asserted = (body as { ifVersion?: number } | undefined)?.ifVersion
        if (held && asserted !== undefined && asserted !== held.version)
          return Response.json(
            { error: { code: "conflict", message: "version mismatch" } },
            { status: 409 }
          )
        return Response.json({ ...row("x"), version: 4 })
      }
      if (url === "/.well-known/substrate/server.json")
        return Response.json({
          features: features.map((name) => ({ name, stability: "beta" })),
        })
      if (url === "/api/v1/substrate.reamde.dev/core/bundle/status")
        return Response.json({ items: statuses })
      if (url === "/api/v1/operator/defaults") {
        await holdDefaults
        if (answer === "network") throw new TypeError("Failed to fetch")
        if ("status" in answer)
          return Response.json(
            { error: { code: "not_found", message: "no route" } },
            { status: answer.status }
          )
        return Response.json(answer)
      }
      const kinds = kindsOf(url)
      if (kinds.includes(LLM_PROVIDER)) return Response.json({ records: rows })
      if (kinds.includes(LLM_MODEL)) return Response.json({ records: models })
      if (kinds.includes("substrate.reamde.dev/core/function"))
        return Response.json({ records: functions })
      return Response.json({ records: [], items: [] })
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function Preferences({
  initial,
  technical,
  children,
}: {
  initial: Partial<ConsolePreferences>
  technical: boolean
  children: React.ReactNode
}) {
  const [preferences, setPreferences] = useState<ConsolePreferences>({
    collapsed: [],
    favorites: [],
    sidebarOpen: true,
    views: [],
    ...DEFAULT_SETTINGS,
    technicalDetails: technical,
    ...initial,
  })
  const change = useCallback((action: ConsoleAction) => {
    actions.push(action)
    setPreferences((p) => applyConsoleAction(p, action))
  }, [])
  const value = useMemo<ConsolePreferencesContextValue>(
    () => ({ preferences, busy: false, change, set: () => {} }),
    [preferences, change]
  )
  return (
    <ConsolePreferencesContext.Provider value={value}>
      {children}
    </ConsolePreferencesContext.Provider>
  )
}

function renderPage({
  initial = { welcomeDismissed: true },
  technical = false,
}: { initial?: Partial<ConsolePreferences>; technical?: boolean } = {}) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <Preferences initial={initial} technical={technical}>
      <QueryClientProvider client={client}>
        <Toaster>
          <ProvidersPage />
        </Toaster>
      </QueryClientProvider>
    </Preferences>
  )
  return client
}

/** One group's section, by its heading. */
function group(title: string): HTMLElement {
  return screen
    .getByRole("heading", { level: 2, name: title })
    .closest<HTMLElement>("section")!
}

async function modelCard(name: string): Promise<HTMLElement> {
  const heading = await screen.findByRole("heading", { level: 3, name })
  return heading.closest<HTMLElement>('[data-slot="model-card"]')!
}

function providerCard(name: string): HTMLElement {
  return screen
    .getByText(name, { selector: "a" })
    .closest<HTMLElement>('[data-slot="provider-card"]')!
}

describe("the groups", () => {
  it("lists accounts, then tools, then AI models", async () => {
    catalog.providers = [
      ...catalog.providers,
      // Installed: the catalog says it brings tools and no account.
      provider("firecrawl", {
        installed: true,
        kinds: [],
        functions: [`${PROV}/firecrawl/websearch`],
      }),
      // Not installed: the catalog says the same before it is added.
      provider("scraper", {
        kinds: [],
        functions: [`${PROV}/scraper/fetch`],
      }),
    ]
    functions = [
      {
        id: `${PROV}/firecrawl/websearch`,
        kind: "substrate.reamde.dev/core/function",
        version: 1,
        properties: { offer: { agents: true } },
      } as unknown as SubstrateRecord,
    ]
    renderPage()
    await modelCard("OpenAI")
    const headings = screen
      .getAllByRole("heading", { level: 2 })
      .map((h) => h.textContent)
    expect(headings).toEqual([
      "Accounts you connect",
      "Tools for your agents",
      "AI models",
    ])
    const accounts = group("Accounts you connect")
    expect(within(accounts).getByText("Google", { selector: "a" })).toBeTruthy()
    expect(within(accounts).getByText("Linear", { selector: "a" })).toBeTruthy()
    const tools = group("Tools for your agents")
    await waitFor(() =>
      expect(
        within(tools).getByText("Firecrawl", { selector: "a" })
      ).toBeTruthy()
    )
    expect(within(tools).getByText("Scraper", { selector: "a" })).toBeTruthy()
    expect(within(tools).queryByText("Google", { selector: "a" })).toBeNull()
  })

  it("keeps an installed provider with an account kind among the accounts, whatever it offers", async () => {
    catalog.providers = [
      provider("slack", {
        installed: true,
        accountKind: true,
        functions: [`${PROV}/slack/post`],
        brings: ["account", "tools", "collections"],
      }),
    ]
    functions = [
      {
        id: `${PROV}/slack/post`,
        kind: "substrate.reamde.dev/core/function",
        version: 1,
        properties: { offer: { agents: true } },
      } as unknown as SubstrateRecord,
    ]
    renderPage()
    await modelCard("OpenAI")
    expect(
      within(group("Accounts you connect")).getByText("Slack", {
        selector: "a",
      })
    ).toBeTruthy()
    expect(
      screen.queryByRole("heading", { name: "Tools for your agents" })
    ).toBeNull()
  })

  it("shows one AI model card per row, titled by its label, a person's own row included", async () => {
    rows = [...rows, row("openrouter", { label: "OpenRouter", apiKey: "••••" })]
    renderPage()
    const models = group("AI models")
    await modelCard("OpenRouter")
    const titles = within(models)
      .getAllByRole("heading", { level: 3 })
      .map((h) => h.textContent)
    // The seeded rows first, in the seed's order, then the reader's own.
    expect(titles).toEqual(["OpenAI", "Anthropic", "Gemini", "OpenRouter"])
    expect(
      within(await modelCard("OpenRouter")).getByText("Using your key")
    ).toBeTruthy()
  })
})

describe("an AI model card's title", () => {
  it("names a well-known row by its vendor, and a person's own row by its label as written", async () => {
    rows = [
      ...rows,
      row("azure", { label: "azure", apiKey: "••••" }),
      row("openrouter", { label: "openrouter", apiKey: "••••" }),
      row("gateway", { label: "my gateway", apiKey: "••••" }),
    ]
    renderPage()
    await modelCard("OpenRouter")
    const titles = within(group("AI models"))
      .getAllByRole("heading", { level: 3 })
      .map((h) => h.textContent)
    expect(titles).toEqual([
      "OpenAI",
      "Anthropic",
      "Gemini",
      "Azure OpenAI",
      "my gateway",
      "OpenRouter",
    ])
  })
})

describe("an AI model card", () => {
  it("says On, Set up or Needs attention, and never Paused", async () => {
    answer = {
      operator: { name: "Crane" },
      items: [
        OPENAI,
        ANTHROPIC_NONE,
        {
          ...GEMINI_OWN,
          readiness: "failing",
          message: "the last call was refused: 401 invalid api key",
        },
      ],
    }
    renderPage()
    const openai = await modelCard("OpenAI")
    expect(within(openai).getByText("On")).toBeTruthy()
    expect(
      within(openai).getByText("Chat models for your agents.")
    ).toBeTruthy()
    expect(
      within(openai).getByText("Using Crane’s key, for gpt-5 and gpt-5-mini.")
    ).toBeTruthy()

    const anthropic = await modelCard("Anthropic")
    expect(within(anthropic).getByText("Set up")).toBeTruthy()
    // Nobody's key: no whose-key line.
    expect(within(anthropic).queryByText(/^Using /)).toBeNull()

    const gemini = await modelCard("Gemini")
    expect(within(gemini).getByText("Needs attention")).toBeTruthy()
    expect(within(gemini).getByText("Using your key")).toBeTruthy()
    expect(
      within(gemini).getByText("the last call was refused: 401 invalid api key")
    ).toBeTruthy()

    expect(within(group("AI models")).queryByText("Paused")).toBeNull()
  })

  it("Needs attention and says the refusal in plain words where search by meaning through it fails", async () => {
    answer = {
      operator: { name: "Crane" },
      items: [
        { ...OPENAI, source: "own", models: undefined },
        ANTHROPIC_NONE,
        GEMINI_OWN,
        {
          ...EMBEDDINGS,
          readiness: "failing",
          message: "OpenAI refused the key (401)",
        },
      ],
    }
    rows = [
      row("openai", { apiKey: "••••" }),
      row("anthropic"),
      row("gemini", { apiKey: "••••" }),
    ]
    renderPage()
    const openai = await modelCard("OpenAI")
    expect(within(openai).getByText("Needs attention")).toBeTruthy()
    expect(within(openai).queryByText("On")).toBeNull()
    expect(within(openai).getByText("Using your key")).toBeTruthy()
    expect(
      within(openai).getByText("Search by meaning: Crane’s key")
    ).toBeTruthy()
    expect(
      within(openai).getByText("OpenAI refused the key (401)")
    ).toBeTruthy()
    expect(within(await modelCard("Gemini")).getByText("On")).toBeTruthy()
  })

  it("says a refused key once where chat and search by meaning share it", async () => {
    const refused = "OpenAI refused the key (401)"
    answer = {
      operator: { name: "Crane" },
      items: [
        { ...OPENAI, readiness: "failing", message: refused },
        ANTHROPIC_NONE,
        GEMINI_OWN,
        { ...EMBEDDINGS, readiness: "failing", message: refused },
      ],
    }
    renderPage()
    const openai = await modelCard("OpenAI")
    expect(within(openai).getByText("Needs attention")).toBeTruthy()
    expect(within(openai).getAllByText(refused)).toHaveLength(1)
  })

  it("reads a row's own key where the server is older than the operator read", async () => {
    answer = { status: 404 }
    features = []
    renderPage()
    expect(within(await modelCard("Gemini")).getByText("On")).toBeTruthy()
    expect(within(await modelCard("OpenAI")).getByText("Set up")).toBeTruthy()
  })

  it("switches from the operator's key to the reader's own, in place", async () => {
    renderPage()
    const openai = await modelCard("OpenAI")
    fireEvent.click(
      within(openai).getByRole("button", { name: "Use your own key" })
    )
    expect(
      within(openai).getByText(
        "Your agents will call OpenAI with this key, billed to your OpenAI account, and those calls stop counting toward Crane’s allowance. The key is stored sealed and never shown again."
      )
    ).toBeTruthy()
    fireEvent.change(within(openai).getByLabelText("Your OpenAI API key"), {
      target: { value: " sk-mine " },
    })
    fireEvent.click(within(openai).getByRole("button", { name: "Save key" }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0]).toEqual({
      method: "PATCH",
      url: `/api/v1/${LLM_PROVIDER}/openai`,
      body: { properties: { apiKey: "sk-mine" }, ifVersion: 3 },
    })
  })

  it("replaces the reader's own key, and offers the operator's back only where it is offered", async () => {
    answer = {
      operator: { name: "Crane" },
      items: [OPENAI, ANTHROPIC_NONE, { ...GEMINI_OWN, operatorOffers: true }],
    }
    renderPage()
    const gemini = await modelCard("Gemini")
    const replace = within(gemini).getByRole("button", { name: "Replace key" })
    // An outline button beside a quiet one: the outline is the border.
    expect(replace.className).toContain("border-border")
    const again = within(gemini).getByRole("button", {
      name: "Use Crane’s key again",
    })
    expect(again.className).not.toContain("border-border")
    expect(again.className).not.toContain("bg-primary")
    fireEvent.click(again)
    const dialog = await screen.findByRole("dialog")
    expect(
      within(dialog).getByText("Use Crane’s Gemini key again?")
    ).toBeTruthy()
    expect(
      within(dialog).getByText(
        "This deletes your Gemini key. Your agents then call Gemini with Crane’s key, which counts toward today’s allowance. You can add your own key again at any time."
      )
    ).toBeTruthy()
    expect(writes).toHaveLength(0)
    fireEvent.click(
      within(dialog).getByRole("button", { name: "Use Crane’s key" })
    )
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0]).toEqual({
      method: "PATCH",
      url: `/api/v1/${LLM_PROVIDER}/gemini`,
      body: { properties: { apiKey: null }, ifVersion: 3 },
    })
  })

  it("does not offer the operator's key back where the operator does not offer it", async () => {
    renderPage()
    const gemini = await modelCard("Gemini")
    expect(
      within(gemini).getByRole("button", { name: "Replace key" })
    ).toBeTruthy()
    expect(within(gemini).queryByRole("button", { name: /again/ })).toBeNull()
  })

  it("says a card with no key needs one, in plain words and never the server's", async () => {
    const message = "no apiKey: add your own key to the row to use it"
    answer = {
      operator: { name: "Crane" },
      items: [
        OPENAI,
        { ...ANTHROPIC_NONE, message },
        { ...GEMINI_OWN, source: "none", readiness: "not-set-up", message },
      ],
    }
    rows = [row("openai"), row("anthropic"), row("gemini")]
    renderPage()
    const gemini = await modelCard("Gemini")
    expect(within(gemini).getByText("Set up")).toBeTruthy()
    const status = within(gemini).getByText("Needs a key")
    expect(status.className).not.toMatch(/destructive/)
    expect(within(gemini).getByRole("button", { name: "Add key" })).toBeTruthy()
    expect(within(group("AI models")).queryByText(message)).toBeNull()
  })

  it("adds a first key where none resolves", async () => {
    renderPage()
    const anthropic = await modelCard("Anthropic")
    fireEvent.click(within(anthropic).getByRole("button", { name: "Add key" }))
    expect(
      within(anthropic).getByLabelText("Your Anthropic API key")
    ).toBeTruthy()
    expect(
      within(anthropic).getByText(
        "Your agents will call Anthropic with this key, billed to your Anthropic account. It is stored sealed and never shown again."
      )
    ).toBeTruthy()
  })

  it("brings a deleted seeded row back from the seed, with no key", async () => {
    rows = [row("openai"), row("gemini", { apiKey: "••••" })]
    answer = {
      operator: { name: "Crane" },
      items: [OPENAI, { ...ANTHROPIC_NONE, operatorOffers: true }, GEMINI_OWN],
    }
    renderPage()
    const anthropic = await modelCard("Anthropic")
    expect(
      within(anthropic).getByText(
        "Chat models for your agents. This one was removed. Add it back and Crane’s key covers it again."
      )
    ).toBeTruthy()
    fireEvent.click(within(anthropic).getByRole("button", { name: "Add" }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].method).toBe("PUT")
    expect(writes[0].url).toBe(`/api/v1/${LLM_PROVIDER}/anthropic`)
    const body = writes[0].body as { properties: Record<string, unknown> }
    expect(body.properties.wire).toBe("anthropic")
    expect(body.properties.baseURL).toBe("https://api.anthropic.com/")
    expect(body.properties).not.toHaveProperty("apiKey")
  })

  it("shows the search line on the card the embeddings item names, and nowhere else", async () => {
    answer = {
      operator: { name: "Crane" },
      items: [OPENAI, ANTHROPIC_NONE, GEMINI_OWN, EMBEDDINGS],
    }
    renderPage()
    const openai = await modelCard("OpenAI")
    expect(
      within(openai).getByText("Search by meaning: Crane’s key")
    ).toBeTruthy()
    expect(screen.getAllByText(/^Search by meaning:/)).toHaveLength(1)
  })

  it("says search by meaning runs on the reader's key where they claimed it", async () => {
    answer = {
      operator: { name: "Crane" },
      items: [
        OPENAI,
        ANTHROPIC_NONE,
        GEMINI_OWN,
        { ...EMBEDDINGS, source: "own", target: `${LLM_PROVIDER}/gemini` },
      ],
    }
    renderPage()
    expect(
      within(await modelCard("Gemini")).getByText("Search by meaning: your key")
    ).toBeTruthy()
    expect(
      within(await modelCard("OpenAI")).queryByText(/^Search by meaning/)
    ).toBeNull()
  })

  it("links the row's record only with Technical details on", async () => {
    renderPage()
    expect(
      within(await modelCard("OpenAI")).queryByText("Open record")
    ).toBeNull()
    cleanup()
    renderPage({ technical: true })
    const link = within(await modelCard("OpenAI")).getByText("Open record")
    expect(link.getAttribute("data-to")).toBe("/data/$authority/$pkg/$name/$id")
    expect(JSON.parse(link.getAttribute("data-params")!).id).toBe("openai")
  })

  it("opens the key form on the card an agent's link names, where it has no key", async () => {
    route.search = { model: "anthropic" }
    renderPage()
    const anthropic = await modelCard("Anthropic")
    expect(anthropic.dataset.focused).toBe("true")
    expect(
      within(anthropic).getByLabelText("Your Anthropic API key")
    ).toBeTruthy()
    expect(
      within(await modelCard("OpenAI")).queryByLabelText(/API key/)
    ).toBeNull()
  })

  it("names the operator as this server where the site has no name", async () => {
    answer = { operator: {}, items: [OPENAI, ANTHROPIC_NONE, GEMINI_OWN] }
    renderPage()
    expect(
      within(await modelCard("OpenAI")).getByText(
        "Using this server’s key, for gpt-5 and gpt-5-mini."
      )
    ).toBeTruthy()
  })
})

function model(
  id: string,
  rowId: string,
  modelId: string,
  properties: Record<string, unknown> = {}
) {
  return {
    id,
    kind: LLM_MODEL,
    version: 1,
    properties: {
      provider: { ref: `${LLM_PROVIDER}/${rowId}` },
      model: modelId,
      ...properties,
    },
  } as unknown as SubstrateRecord
}

/** The models a card lists, opened from under their count. */
async function offers(card: HTMLElement, count: RegExp) {
  fireEvent.click(await within(card).findByRole("button", { name: count }))
  return within(card)
    .getAllByText((_, el) => el?.getAttribute("data-slot") === "model-offer")
    .map((el) => el.textContent)
}

describe("the models a card offers", () => {
  beforeEach(() => {
    models = [
      model("g:flash", "gemini", "gemini-2.5-flash", {
        label: "Gemini 2.5 Flash",
        pricing: { inputPer1M: "0.15", outputPer1M: "0.60" },
        contextWindow: 1048576,
      }),
      model("g:pro", "gemini", "gemini-2.5-pro", { label: "Gemini 2.5 Pro" }),
      model("o:gpt-5", "openai", "gpt-5", {
        label: "GPT-5",
        pricing: { inputPer1M: "1.25", outputPer1M: "10" },
        contextWindow: 400000,
      }),
    ]
  })

  it("lists each with its price, its window and where the price comes from", async () => {
    renderPage()
    const gemini = await modelCard("Gemini")
    expect(await offers(gemini, /^2 models it offers$/)).toEqual([
      "Gemini 2.5 Flashgemini-2.5-flashPrice per 1M tokens$0.15 input · $0.60 outputContext window1M tokensPrice fromModel record",
      "Gemini 2.5 Progemini-2.5-proPrice per 1M tokensNot priced: a spend cap doesn’t count its runs, and an app’s approval doesn’t offer itContext windowNot stated",
    ])
  })

  it("lists only the operator's models on a row it serves, without its prices", async () => {
    rows = [
      row("openai", {
        pricing: [{ model: "gpt-4o", inputPer1M: "1", outputPer1M: "1" }],
      }),
      row("anthropic"),
      row("gemini", { apiKey: "••••" }),
    ]
    renderPage()
    const openai = await modelCard("OpenAI")
    expect(await offers(openai, /^2 models on Crane’s key$/)).toEqual([
      "GPT-5gpt-5",
      "gpt-5-mini",
    ])
    expect(within(openai).getByText("Crane sets what they cost.")).toBeTruthy()
  })

  it("lets the row's own price win, and drops it for the record's in one PATCH", async () => {
    rows = [
      row("openai"),
      row("anthropic"),
      row("gemini", {
        apiKey: "••••",
        pricing: [
          {
            model: "gemini-2.5-flash",
            inputPer1M: "0.30",
            outputPer1M: "2.50",
            contextWindow: 32000,
          },
          { model: "gemini-1.5", inputPer1M: "1", outputPer1M: "2" },
        ],
      }),
    ]
    renderPage()
    const gemini = await modelCard("Gemini")
    const listed = await offers(gemini, /^3 models it offers$/)
    expect(listed[0]).toBe(
      "gemini-1.5Price per 1M tokens$1 input · $2 outputContext windowNot statedPrice fromThis row’s own price list"
    )
    expect(listed[1]).toContain(
      "Price per 1M tokens$0.30 input · $2.50 outputContext window32k tokensPrice fromThis row’s own price list"
    )
    expect(listed[1]).toContain(
      "Its model record says $0.15 input · $0.60 output; this row’s own price list wins over it."
    )
    fireEvent.click(
      within(gemini).getByRole("button", {
        name: "Use the model record’s price",
      })
    )
    const dialog = await screen.findByRole("dialog")
    expect(
      within(dialog).getByText(
        "This removes gemini-2.5-flash from Gemini’s own price list. Its runs are then priced by its model record: $0.15 input · $0.60 output per 1M tokens."
      )
    ).toBeTruthy()
    expect(writes).toHaveLength(0)
    fireEvent.click(
      within(dialog).getByRole("button", {
        name: "Use the model record’s price",
      })
    )
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0]).toEqual({
      method: "PATCH",
      url: `/api/v1/${LLM_PROVIDER}/gemini`,
      body: {
        properties: {
          pricing: [{ model: "gemini-1.5", inputPer1M: "1", outputPer1M: "2" }],
        },
        ifVersion: 3,
      },
    })
  })

  it("says a row no record names offers no model yet", async () => {
    models = []
    renderPage()
    const gemini = await modelCard("Gemini")
    expect(
      await within(gemini).findByText(
        "It offers no model yet: no model record names it."
      )
    ).toBeTruthy()
  })
})

describe("a key typed into a card", () => {
  it("leaves nothing of the key in the mutation cache, while it is sent or after it saved", async () => {
    let sending = ""
    const client = renderPage()
    onWrite = () => {
      sending = mutationText(client)
    }
    const openai = await modelCard("OpenAI")
    fireEvent.click(
      within(openai).getByRole("button", { name: "Use your own key" })
    )
    fireEvent.change(within(openai).getByLabelText("Your OpenAI API key"), {
      target: { value: "sk-typed-secret" },
    })
    fireEvent.click(within(openai).getByRole("button", { name: "Save key" }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({
      properties: { apiKey: "sk-typed-secret" },
      ifVersion: 3,
    })
    await waitFor(() =>
      expect(within(openai).queryByLabelText(/API key/)).toBeNull()
    )
    // The write was in the cache, pending, as the request left.
    expect(sending).toContain('"record-patch"')
    expect(sending).toContain('"pending"')
    expect(sending).not.toContain("sk-typed-secret")
    expect(mutationText(client)).not.toContain("sk-typed-secret")
    expect(client.getMutationCache().getAll()).toHaveLength(0)
  })

  it("clears the key and leaves nothing of it in the mutation cache after a refused save, or after Cancel", async () => {
    const client = renderPage()
    const gemini = await modelCard("Gemini")
    fireEvent.click(within(gemini).getByRole("button", { name: "Replace key" }))
    // Someone else's write lands on the server while the form is open.
    rows = rows.map((r) => (r.id === "gemini" ? { ...r, version: 4 } : r))
    const input = within(gemini).getByLabelText<HTMLInputElement>(
      "Your Gemini API key"
    )
    fireEvent.change(input, { target: { value: "sk-refused-secret" } })
    fireEvent.click(within(gemini).getByRole("button", { name: "Save key" }))
    expect(await within(gemini).findByRole("alert")).toBeTruthy()
    expect(input.value).toBe("")
    expect(mutationText(client)).not.toContain("sk-refused-secret")
    fireEvent.click(within(gemini).getByRole("button", { name: "Cancel" }))
    expect(within(gemini).queryByLabelText(/API key/)).toBeNull()
    expect(mutationText(client)).not.toContain("sk-refused-secret")
    expect(client.getMutationCache().getAll()).toHaveLength(0)
  })

  it("leaves nothing in the mutation cache and an empty field after Cancel on a key never sent", async () => {
    const client = renderPage()
    const openai = await modelCard("OpenAI")
    const open = () =>
      fireEvent.click(
        within(openai).getByRole("button", { name: "Use your own key" })
      )
    open()
    fireEvent.change(within(openai).getByLabelText("Your OpenAI API key"), {
      target: { value: "sk-never-sent" },
    })
    fireEvent.click(within(openai).getByRole("button", { name: "Cancel" }))
    expect(within(openai).queryByLabelText(/API key/)).toBeNull()
    expect(writes).toHaveLength(0)
    expectNoTrace(client, "sk-never-sent")
    open()
    expect(
      within(openai).getByLabelText<HTMLInputElement>("Your OpenAI API key")
        .value
    ).toBe("")
  })

  it("keeps no key in the mutation cache after Use the operator's key again clears the reader's own", async () => {
    answer = {
      operator: { name: "Crane" },
      items: [OPENAI, ANTHROPIC_NONE, { ...GEMINI_OWN, operatorOffers: true }],
    }
    const client = renderPage()
    const gemini = await modelCard("Gemini")
    fireEvent.click(within(gemini).getByRole("button", { name: "Replace key" }))
    fireEvent.change(within(gemini).getByLabelText("Your Gemini API key"), {
      target: { value: "sk-own-replaced" },
    })
    fireEvent.click(within(gemini).getByRole("button", { name: "Save key" }))
    await waitFor(() =>
      expect(within(gemini).queryByLabelText(/API key/)).toBeNull()
    )
    fireEvent.click(
      within(gemini).getByRole("button", { name: "Use Crane’s key again" })
    )
    fireEvent.click(
      within(await screen.findByRole("dialog")).getByRole("button", {
        name: "Use Crane’s key",
      })
    )
    await waitFor(() => expect(writes).toHaveLength(2))
    expect(writes[1].body).toEqual({
      properties: { apiKey: null },
      ifVersion: 3,
    })
    await waitFor(() =>
      expect(client.getMutationCache().getAll()[0]?.state.status).toBe(
        "success"
      )
    )
    // The clear is the one write left, and it carries no value at all.
    const cached = client.getMutationCache().getAll()
    expect(cached).toHaveLength(1)
    expect(cached[0].state.variables).toBeUndefined()
    expect(mutationText(client)).not.toContain("sk-own-replaced")
    expect(mutationText(client)).not.toMatch(/"apiKey":"/)
  })

  it("is refused, not written over, where the row changed while the form was open", async () => {
    const client = renderPage()
    const gemini = await modelCard("Gemini")
    fireEvent.click(within(gemini).getByRole("button", { name: "Replace key" }))
    // Another tab replaces the key; the page reads the row again.
    rows = rows.map((r) => (r.id === "gemini" ? { ...r, version: 4 } : r))
    await client.invalidateQueries({
      queryKey: llmProvidersQueryOptions().queryKey,
    })
    await waitFor(() =>
      expect(
        client
          .getQueryData(llmProvidersQueryOptions().queryKey)
          ?.records.find((r) => r.id === "gemini")?.version
      ).toBe(4)
    )
    fireEvent.change(within(gemini).getByLabelText("Your Gemini API key"), {
      target: { value: "sk-late" },
    })
    fireEvent.click(within(gemini).getByRole("button", { name: "Save key" }))
    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({
      properties: { apiKey: "sk-late" },
      ifVersion: 3,
    })
    expect((await within(gemini).findByRole("alert")).textContent).toMatch(
      /changed since you opened it/
    )
    expect(within(gemini).getByLabelText("Your Gemini API key")).toBeTruthy()
  })
})

describe("where the operator read fails", () => {
  it.each([
    ["answers 500", { status: 500 } as const],
    ["cannot be reached", "network" as const],
  ])(
    "says each card couldn't be checked, never Set up, while the read %s",
    async (_, failure) => {
      answer = failure
      renderPage()
      const models = group("AI models")
      const alert = await within(models).findByRole("alert")
      expect(alert.textContent).toMatch(
        /^Couldn’t check whose key each AI model uses/
      )
      const openai = await modelCard("OpenAI")
      expect(within(openai).getByText("Couldn’t check")).toBeTruthy()
      expect(within(openai).queryByText(/^Using /)).toBeNull()
      const gemini = await modelCard("Gemini")
      expect(within(gemini).getByText("Couldn’t check")).toBeTruthy()
      // Its own key is its own whatever the operator offers.
      expect(within(gemini).getByText("Using your key")).toBeTruthy()
      expect(within(models).queryByText("Set up")).toBeNull()

      answer = {
        operator: { name: "Crane" },
        items: [OPENAI, ANTHROPIC_NONE, GEMINI_OWN],
      }
      fireEvent.click(within(alert).getByRole("button", { name: "Try again" }))
      await waitFor(() => expect(within(openai).getByText("On")).toBeTruthy())
      expect(within(models).queryByRole("alert")).toBeNull()
    }
  )

  it("opens the key form an agent's link names once the read says it has no key", async () => {
    let release = () => {}
    holdDefaults = new Promise((r) => {
      release = r
    })
    route.search = { model: "anthropic" }
    renderPage()
    const anthropic = await modelCard("Anthropic")
    expect(within(anthropic).getByText("Checking")).toBeTruthy()
    expect(within(anthropic).queryByLabelText(/API key/)).toBeNull()
    expect(within(group("AI models")).queryByText("Set up")).toBeNull()
    release()
    expect(
      await within(anthropic).findByLabelText("Your Anthropic API key")
    ).toBeTruthy()
  })
})

describe("the counts beside Providers", () => {
  it("agree with the page, the sidebar's and Home's, though server.json lists no operatorDefaults", async () => {
    features = []
    // No defaults file: the server still answers, and refuses Gemini's own
    // key, so the page shows it as Set up.
    answer = {
      items: [
        {
          ...OPENAI,
          source: "none",
          operatorOffers: false,
          readiness: "not-set-up",
          models: undefined,
        },
        ANTHROPIC_NONE,
        {
          ...GEMINI_OWN,
          readiness: "not-set-up",
          message: "gemini: baseURL is not an https URL",
        },
      ],
    }
    statuses = [provider("google", { installed: true }).row.status]
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <Preferences initial={{ welcomeDismissed: true }} technical={false}>
        <QueryClientProvider client={client}>
          <Toaster>
            <ProvidersPage />
            <div data-testid="sidebar">
              <ProviderCount />
            </div>
            <div data-testid="home">
              <OverviewCards groups={[]} />
            </div>
          </Toaster>
        </QueryClientProvider>
      </Preferences>
    )
    const gemini = await modelCard("Gemini")
    await waitFor(() => expect(within(gemini).getByText("Set up")).toBeTruthy())
    // On the page: Google is the one thing set up.
    const on = within(group("AI models")).queryAllByText("On")
    expect(on).toHaveLength(0)
    await waitFor(() =>
      expect(screen.getByTestId("sidebar").textContent).toBe("1")
    )
    expect(
      within(screen.getByTestId("home")).getByText("1 in use")
    ).toBeTruthy()
  })
})

describe("whose key a provider runs on", () => {
  it("says the operator's sign-in, with its notice, and nothing where nobody supplies one", async () => {
    answer = {
      operator: { name: "Crane" },
      items: [
        OPENAI,
        item({
          id: `oauth/${PROV}/google`,
          slot: "oauth",
          label: "Google",
          target: `${PROV}/google`,
          readiness: "needs-connect",
          notice: "Google shows a warning while this sign-in is in testing.",
        }),
      ],
    }
    renderPage()
    await modelCard("OpenAI")
    const google = providerCard("Google")
    const line = within(google).getByText("Through Crane’s Google sign-in")
    const notice = within(google).getByText(
      "Google shows a warning while this sign-in is in testing."
    )
    // Quiet text right under the line: the step line stays the one coloured
    // status.
    expect(line.nextElementSibling).toBe(notice)
    expect(notice.className).toContain("text-faint")
    expect(notice.className).not.toMatch(/bg-warn|text-warning/)
    const linear = providerCard("Linear")
    expect(within(linear).queryByText(/^Using |^Through /)).toBeNull()
  })

  it("says the reader's own sign-in details where they replaced the operator's", async () => {
    answer = {
      operator: { name: "Crane" },
      items: [
        OPENAI,
        item({
          id: `oauth/${PROV}/google`,
          slot: "oauth",
          label: "Google",
          source: "own",
        }),
      ],
    }
    renderPage()
    await modelCard("OpenAI")
    expect(
      within(providerCard("Google")).getByText("Using your sign-in details")
    ).toBeTruthy()
  })

  it("says on an account's card and a tool's card why the operator couldn't add it", async () => {
    catalog.providers = [
      ...catalog.providers,
      provider("firecrawl", {
        kinds: [],
        functions: [`${PROV}/firecrawl/websearch`],
      }),
    ]
    answer = {
      operator: { name: "Crane" },
      items: [
        OPENAI,
        item({
          id: `oauth/${PROV}/google`,
          slot: "oauth",
          label: "Google",
          target: `${PROV}/google`,
          readiness: "install-failed",
          message: "the catalog is unreachable",
        }),
        item({
          id: `function/${PROV}/firecrawl`,
          slot: "function",
          label: "Firecrawl",
          target: `${PROV}/firecrawl`,
          source: "none",
          readiness: "install-failed",
          message: "the catalog timed out",
        }),
      ],
    }
    renderPage()
    await modelCard("OpenAI")
    expect(
      within(providerCard("Google")).getByText(
        "Crane couldn’t add it for you: the catalog is unreachable"
      )
    ).toBeTruthy()
    const firecrawl = providerCard("Firecrawl")
    expect(group("Tools for your agents").contains(firecrawl)).toBe(true)
    expect(
      within(firecrawl).getByText(
        "Crane couldn’t add it for you: the catalog timed out"
      )
    ).toBeTruthy()
  })

  it("says nothing on an account card where the operator offers no sign-in", async () => {
    answer = {
      items: [
        OPENAI,
        item({
          id: `oauth/${PROV}/google`,
          slot: "oauth",
          label: "Google",
          source: "own",
          operatorOffers: false,
        }),
      ],
    }
    renderPage()
    await modelCard("OpenAI")
    expect(
      within(providerCard("Google")).queryByText(
        /^(?:Using your sign-in details|Through .+ sign-in)$/
      )
    ).toBeNull()
  })
})

describe("the allowance", () => {
  // Only Date is faked: the reset reads as "in N hours" against the clock, so
  // a real one would turn RESETS_AT into "now" once the day passes.
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] })
    vi.setSystemTime(new Date("2026-11-06T15:00:00Z"))
  })
  afterEach(() => {
    vi.useRealTimers()
  })

  const allowance = {
    centsPerDay: 200,
    usedCents: 37,
    resetsAt: RESETS_AT,
    bySlot: [
      { slot: "llm", cents: 31 },
      { slot: "embeddings", cents: 6 },
    ],
  }

  it("sits on the AI models header, split by slot, resetting in local time", async () => {
    answer = {
      operator: { name: "Crane", allowance },
      items: [OPENAI, ANTHROPIC_NONE, GEMINI_OWN],
    }
    renderPage()
    await modelCard("OpenAI")
    const bar = await screen.findByRole("meter", {
      name: "Today’s allowance from Crane used",
    })
    expect(group("AI models").contains(bar)).toBe(true)
    expect(group("Accounts you connect").contains(bar)).toBe(false)
    expect(screen.getAllByRole("meter")).toHaveLength(1)
    const box = bar.closest<HTMLElement>('[data-slot="allowance"]')!
    expect(within(box).getByText("$0.37 of $2.00 used")).toBeTruthy()
    expect(within(box).getByText("chat")).toBeTruthy()
    expect(within(box).getByText("search by meaning")).toBeTruthy()
    // Midnight UTC is 02:00 in Athens in November.
    expect(within(box).getByText(/^Resets at 02:00, in /)).toBeTruthy()
    expect(
      within(box).getByText(
        "When it runs out, anything on Crane’s key pauses until it resets. Anything on your own key keeps working and is not counted."
      )
    ).toBeTruthy()
  })

  it("is not shown where the operator pays for nothing now", async () => {
    answer = {
      operator: { name: "Crane", allowance },
      items: [{ ...OPENAI, source: "own" }, ANTHROPIC_NONE, GEMINI_OWN],
    }
    renderPage()
    await modelCard("OpenAI")
    expect(screen.queryByRole("meter")).toBeNull()
  })

  it("says on the tool's own row, not the Tools header, that it runs on the operator's key", async () => {
    catalog.providers = [
      provider("firecrawl", {
        kinds: [],
        functions: [`${PROV}/firecrawl/websearch`],
      }),
    ]
    answer = {
      operator: { name: "Crane", allowance },
      items: [
        OPENAI,
        item({
          id: `function/${PROV}/firecrawl`,
          slot: "function",
          label: "Firecrawl",
          target: `${PROV}/firecrawl`,
        }),
      ],
    }
    renderPage()
    await modelCard("OpenAI")
    const tools = group("Tools for your agents")
    // A tool on the reader's own account counts toward nothing, so the
    // header names no allowance.
    expect(within(tools).queryByText(/allowance/)).toBeNull()
    const firecrawl = providerCard("Firecrawl")
    expect(within(firecrawl).getByText("Using Crane’s key")).toBeTruthy()
    // Named by what it does, as the welcome names it.
    expect(
      within(firecrawl).getByText("Web search for your agents.")
    ).toBeTruthy()
  })
})

describe("the welcome", () => {
  it("opens the page where the operator supplies keys, until Done", async () => {
    answer = {
      operator: { name: "Crane", allowance: undefined },
      items: [OPENAI, EMBEDDINGS, ANTHROPIC_NONE, GEMINI_OWN],
    }
    renderPage({ initial: {} })
    expect(await screen.findByText("Welcome to Crane")).toBeTruthy()
    expect(
      screen.getByRole("heading", {
        level: 1,
        name: "Your agents are ready to go",
      })
    ).toBeTruthy()
    expect(
      screen.getByText(/^Chat and search by meaning are already set up/)
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Done" }))
    expect(actions).toEqual([{ type: "welcome", dismissed: true }])
    expect(screen.queryByText("Welcome to Crane")).toBeNull()
    expect(
      screen.getByRole("heading", { level: 1, name: "Providers" })
    ).toBeTruthy()
  })

  it("names what works, a tool by what it does, and the day's limit, never who pays", async () => {
    catalog.providers = [
      ...catalog.providers,
      provider("firecrawl", {
        kinds: [],
        functions: [`${PROV}/firecrawl/websearch`],
      }),
    ]
    answer = {
      operator: {
        name: "Crane",
        allowance: { centsPerDay: 200, usedCents: 0, resetsAt: RESETS_AT },
      },
      items: [
        OPENAI,
        EMBEDDINGS,
        item({
          id: `function/${PROV}/firecrawl`,
          slot: "function",
          label: "Firecrawl",
          target: `${PROV}/firecrawl`,
        }),
        ANTHROPIC_NONE,
        GEMINI_OWN,
      ],
    }
    renderPage({ initial: {} })
    const welcome = (await screen.findByText("Welcome to Crane")).closest(
      "section"
    )!
    expect(
      within(welcome).getByText(
        "Chat, search by meaning and web search are already set up, so you can try them right away, within a daily allowance of $2.00. Connect accounts like Google or WHOOP to bring in your email, calendar and other data. If you’d rather use your own AI accounts, you can switch at any time under AI models, further down this page."
      )
    ).toBeTruthy()
    expect(within(welcome).queryByText(/pays|Paid by/)).toBeNull()
    expect(
      within(welcome).getByText("Chat, search by meaning and web search")
    ).toBeTruthy()
    expect(within(welcome).queryByText(/Firecrawl/)).toBeNull()
    expect(
      within(welcome).getByText(
        "Have an OpenAI, Anthropic or Gemini account? Add it under AI models, and your agents use it instead."
      )
    ).toBeTruthy()
  })

  it("names the accounts waiting on the reader by their brand, as the server labels them by package word", async () => {
    answer = {
      operator: { name: "Crane" },
      items: [
        OPENAI,
        ANTHROPIC_NONE,
        GEMINI_OWN,
        item({
          id: `oauth/${PROV}/google`,
          slot: "oauth",
          label: "google",
          target: `${PROV}/google`,
          readiness: "needs-connect",
        }),
      ],
    }
    renderPage({ initial: {} })
    const welcome = (await screen.findByText("Welcome to Crane")).closest(
      "section"
    )!
    expect(within(welcome).getByText("Connect Google")).toBeTruthy()
    expect(within(welcome).queryByText(/google/)).toBeNull()
  })

  it("is not shown once dismissed", async () => {
    renderPage({ initial: { welcomeDismissed: true } })
    await modelCard("OpenAI")
    expect(screen.queryByText("Welcome to Crane")).toBeNull()
  })

  it("is not shown where server.json lists no operatorDefaults", async () => {
    features = []
    answer = { items: [OPENAI, ANTHROPIC_NONE, GEMINI_OWN] }
    renderPage({ initial: {} })
    await modelCard("OpenAI")
    expect(screen.queryByText(/^Welcome/)).toBeNull()
    expect(
      screen.getByRole("heading", { level: 1, name: "Providers" })
    ).toBeTruthy()
  })
})
