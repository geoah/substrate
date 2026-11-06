// @vitest-environment jsdom
/** Providers as every outside service the substrate spends a key on
 * (decision 0233): the three groups in order, the AI model cards with their
 * key form in place, whose key each card runs on, the search line, the
 * operator's allowance on the AI models header and as a line on the Tools
 * header, and the welcome where the deployment loaded an operator defaults
 * file. The operator read is answered by a mocked fetch; the catalog's
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
    others: [],
    kinds: [],
    chains: () => [],
    pendingUpgrades: [],
    pending: false,
    error: undefined,
    refetch: () => {},
  }),
}))

import { ProvidersPage } from "./providers"

const PROV = "providers.substrate.reamde.dev"
const LLM_PROVIDER = "substrate.reamde.dev/llm/provider"

/** A catalog provider as the page's hook hands it over. */
function provider(
  pkg: string,
  over: {
    installed?: boolean
    accountKind?: boolean
    kinds?: string[]
    functions?: string[]
    description?: string
  } = {}
): ProviderEntry {
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
      tier: "provider",
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
          }
        : undefined,
      catalog: {
        id,
        name: pkg,
        authority: PROV,
        package: pkg,
        description: over.description ?? "",
        version: 1,
        tier: "provider",
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
let answer: OperatorDefaults | { status: number } = { items: [] }
let rows: SubstrateRecord[] = []
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
        return Response.json({ ...row("x"), version: 4 })
      }
      if (url === "/.well-known/substrate/server.json")
        return Response.json({
          features: features.map((name) => ({ name, stability: "beta" })),
        })
      if (url === "/api/v1/operator/defaults") {
        if ("status" in answer)
          return Response.json(
            { error: { code: "not_found", message: "no route" } },
            { status: answer.status }
          )
        return Response.json(answer)
      }
      const kinds = kindsOf(url)
      if (kinds.includes(LLM_PROVIDER)) return Response.json({ records: rows })
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
  return render(
    <Preferences initial={initial} technical={technical}>
      <QueryClientProvider client={client}>
        <Toaster>
          <ProvidersPage />
        </Toaster>
      </QueryClientProvider>
    </Preferences>
  )
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
      // Installed, no account kind, a function offered to agents.
      provider("firecrawl", {
        installed: true,
        kinds: [],
        functions: [`${PROV}/firecrawl/websearch`],
      }),
      // Not installed: the closure declares no kinds and some functions.
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
    expect(within(openai).getByText("Using Crane’s key")).toBeTruthy()
    expect(
      within(openai).getByText(
        "Chat models for your agents: gpt-5 and gpt-5-mini."
      )
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
      within(openai).getByText(/no longer count toward Crane’s allowance/)
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
    expect(
      within(gemini).getByRole("button", { name: "Replace key" })
    ).toBeTruthy()
    fireEvent.click(
      within(gemini).getByRole("button", { name: "Use Crane’s again" })
    )
    const dialog = await screen.findByRole("dialog")
    expect(
      within(dialog).getByText("Use Crane’s Gemini key again?")
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

  it("adds a first key where none resolves", async () => {
    renderPage()
    const anthropic = await modelCard("Anthropic")
    fireEvent.click(within(anthropic).getByRole("button", { name: "Add key" }))
    expect(
      within(anthropic).getByLabelText("Your Anthropic API key")
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
      within(anthropic).getByText(/Add it back and Crane’s key covers it again/)
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
      within(await modelCard("OpenAI")).getByText("Using this server’s key")
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
    expect(
      within(google).getByText("Through Crane’s Google sign-in")
    ).toBeTruthy()
    expect(
      within(google).getByText(
        "Google shows a warning while this sign-in is in testing."
      )
    ).toBeTruthy()
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
      within(providerCard("Google")).queryByText(/sign-in details|sign-in$/)
    ).toBeNull()
  })
})

describe("the allowance", () => {
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

  it("puts one line on the Tools header where a tool runs on the operator's key", async () => {
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
    expect(
      within(tools).getByText("Counts toward today’s allowance from Crane")
    ).toBeTruthy()
    expect(
      within(providerCard("Firecrawl")).getByText("Using Crane’s key")
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
        name: "Your agents can start working now",
      })
    ).toBeTruthy()
    expect(
      screen.getByText(/Crane pays for chat and search by meaning/)
    ).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Done" }))
    expect(actions).toEqual([{ type: "welcome", dismissed: true }])
    expect(screen.queryByText("Welcome to Crane")).toBeNull()
    expect(
      screen.getByRole("heading", { level: 1, name: "Providers" })
    ).toBeTruthy()
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
