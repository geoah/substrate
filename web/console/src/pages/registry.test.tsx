// @vitest-environment jsdom
/** The Registry page as the reader meets it on a FRESH repository: core alone
 * is here, so every row is an invitation and the only questions that matter
 * are what it adds, where it will land, and what has to arrive with it.
 *
 * What is asserted here: the TWO SECTIONS and the door each one offers
 * (Install for a provider, Import for a sample, decision record 0048), the
 * identity a sample previews before it is imported, the disclosure as
 * READABLE SECTIONS (kinds, traits, functions, triggers, each name with the
 * prose its declaration carries), the REQUIREMENT CHAIN (walked past the
 * direct `requires` the wire carries, shown nested, and taken by the row's
 * own button leaves first), and the refusal path (a server problem rides the
 * toast verbatim, naming the bundle in the chain that refused). */

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
  ShippedUpgrade,
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

import { RegistryPage } from "./registry"

const CATALOG_PATH = "/api/v1/catalog"
const SHIPPED_PATH = "/api/v1/vocabulary/upgrade"
const STATUS_PATH = "/api/v1/substrate.reamde.dev/core/bundle/status"
const REPOSITORY_PATH = "/api/v1/substrate.reamde.dev/core/repository"

/** The authority this repository owns, where every imported sample lands. */
const HOME = "ada.example.com"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
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
    closure: {
      traits: null,
      triggers: null,
      kinds: null,
      functions: null,
      agents: null,
      mappings: null,
      records: null,
    },
    installed: false,
    ...over,
  }
}

/** The shipped catalog, trimmed to what these assertions need: two samples,
 * one declaring against the other, and one provider that declares against
 * three packages a fresh repository does not have. */
const PEOPLE = bundle({
  id: "samples.substrate.reamde.dev/people",
  name: "people",
  authority: "samples.substrate.reamde.dev",
  package: "people",
  description: "The shipped vocabulary for humans.",
  tier: "sample",
  closure: {
    traits: null,
    triggers: null,
    functions: null,
    agents: null,
    mappings: null,
    records: null,
    kinds: [
      "samples.substrate.reamde.dev/people/person",
      "samples.substrate.reamde.dev/people/personmerge",
    ],
    kindDescriptions: {
      "samples.substrate.reamde.dev/people/person": "one human being",
    },
  },
  suggestedMappings: [
    {
      id: "samples.substrate.reamde.dev/people/fromgoogle",
      from: "providers.substrate.reamde.dev/google/contact",
      to: "samples.substrate.reamde.dev/people/person",
      package: "providers.substrate.reamde.dev/google",
      state: "waiting",
    },
  ],
})

/** A traits-only sample: it declares no kind at all, and used to render as an
 * empty row that looked like it did nothing. */
const SCHEDULING = bundle({
  id: "samples.substrate.reamde.dev/scheduling",
  name: "scheduling",
  authority: "samples.substrate.reamde.dev",
  package: "scheduling",
  description: "Two shared contracts the repeating kinds bind.",
  tier: "sample",
  closure: {
    kinds: null,
    functions: null,
    agents: null,
    mappings: null,
    records: null,
    triggers: null,
    traits: ["samples.substrate.reamde.dev/scheduling/recurring"],
    traitDescriptions: {
      "samples.substrate.reamde.dev/scheduling/recurring":
        "a repeat rule stored and never expanded",
    },
  },
})

/** A sample whose requirement itself requires two more: the chain the wire
 * does not carry. */
const PEBBLE = bundle({
  id: "samples.substrate.reamde.dev/pebble",
  name: "pebble",
  authority: "samples.substrate.reamde.dev",
  package: "pebble",
  description: "A ring that writes things down.",
  tier: "sample",
  requires: ["samples.substrate.reamde.dev/tasks"],
  closure: {
    kinds: null,
    traits: null,
    agents: null,
    mappings: null,
    kindDescriptions: undefined,
    functions: ["samples.substrate.reamde.dev/pebble/ingest"],
    functionDescriptions: {
      "samples.substrate.reamde.dev/pebble/ingest":
        "turns a capture into a note",
    },
    triggers: ["pebble-webhook"],
    triggerCallables: {
      "pebble-webhook":
        "substrate.reamde.dev/core/function/samples.substrate.reamde.dev/pebble/ingest",
    },
    records: [
      { kind: "substrate.reamde.dev/core/trigger", id: "pebble-webhook" },
    ],
  },
})

/** The firecrawl shape: a sample whose closure ships one secret and one
 * setting as ordinary records under its own id (decision record 0076). */
const FIRECRAWL = bundle({
  id: "samples.substrate.reamde.dev/firecrawl",
  name: "firecrawl",
  authority: "samples.substrate.reamde.dev",
  package: "firecrawl",
  description: "Search the web and read a page.",
  tier: "sample",
  closure: {
    kinds: null,
    traits: null,
    agents: null,
    mappings: null,
    triggers: null,
    functions: ["samples.substrate.reamde.dev/firecrawl/websearch"],
    functionDescriptions: {
      "samples.substrate.reamde.dev/firecrawl/websearch": "search the web",
    },
    records: [
      {
        kind: "substrate.reamde.dev/core/secret",
        id: "samples.substrate.reamde.dev/firecrawl/apiKey",
      },
      {
        kind: "substrate.reamde.dev/core/setting",
        id: "samples.substrate.reamde.dev/firecrawl/baseUrl",
      },
    ],
  },
})

const TASKS = bundle({
  id: "samples.substrate.reamde.dev/tasks",
  name: "tasks",
  authority: "samples.substrate.reamde.dev",
  package: "tasks",
  description: "What is owed.",
  tier: "sample",
  requires: [
    "samples.substrate.reamde.dev/people",
    "samples.substrate.reamde.dev/scheduling",
  ],
  closure: {
    traits: null,
    triggers: null,
    functions: null,
    agents: null,
    mappings: null,
    records: null,
    kinds: ["samples.substrate.reamde.dev/tasks/task"],
  },
})

const GOOGLE = bundle({
  id: "providers.substrate.reamde.dev/google",
  name: "google",
  authority: "providers.substrate.reamde.dev",
  package: "google",
  description: "Connects a Google account — contacts, gmail and calendar.",
  tier: "provider",
  inputs: {
    client: {
      kind: "providers.substrate.reamde.dev/google/config",
      description: "The OAuth client record.",
    },
  },
  requires: [
    "samples.substrate.reamde.dev/people",
    "samples.substrate.reamde.dev/messaging",
    "samples.substrate.reamde.dev/calendar",
  ],
  closure: {
    traits: null,
    triggers: null,
    agents: null,
    mappings: null,
    kinds: [
      "providers.substrate.reamde.dev/google/config",
      "providers.substrate.reamde.dev/google/account",
      "providers.substrate.reamde.dev/google/contact",
    ],
    functions: ["providers.substrate.reamde.dev/google/syncgoogle"],
    records: [
      { kind: "substrate.reamde.dev/core/trigger", id: "ongooglesync" },
    ],
  },
})

const CORE_KIND: KindInfo = {
  identity: "substrate.reamde.dev/core/bundle",
  name: "bundle",
  authority: "substrate.reamde.dev",
  package: "core",
  version: 1,
  source: "builtin",
  description: "",
  definition: {},
}

/** The person kind as it exists AFTER the import: under this repository's own
 * authority, since that is what the import wrote. */
const PERSON_KIND: KindInfo = {
  identity: `${HOME}/people/person`,
  name: "person",
  authority: HOME,
  package: "people",
  version: 1,
  source: "installed",
  description: "",
  definition: {},
}

/** The status of the people sample once imported: its id is the LANDED one. */
function peopleStatus(): BundleStatus {
  return {
    accounts: 0,
    functions: 0,
    id: `${HOME}/people`,
    name: "people",
    authority: HOME,
    package: "people",
    installed: true,
    enabled: true,
    kinds: 2,
    liveRecords: 0,
  }
}

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

interface Wire {
  statuses?: BundleStatus[]
  kinds?: KindInfo[]
  catalog?: CatalogItem[]
  /** The boot upgrade's preview, one entry per shipped package. */
  shipped?: ShippedUpgrade[]
  /** The repository's own authority; "" models a repository that names none. */
  authority?: string
  /** The entry the server answers with when ONE id is read again, which is
   * what a chain step confirms against. */
  fresh?: (id: string) => CatalogItem | undefined
  take?: (id: string) => Response
}

describe("RegistryPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  function serve(wire: Wire = {}) {
    fetchMock.mockImplementation(async (url, init) => {
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      const path = String(url)
      if (path === STATUS_PATH) {
        return jsonResponse(200, { items: wire.statuses ?? [] })
      }
      if (path.startsWith(REPOSITORY_PATH)) {
        return jsonResponse(200, {
          records: [
            {
              id: "r_1",
              kind: "substrate.reamde.dev/core/repository",
              properties: { name: "ada", authority: wire.authority ?? HOME },
            },
          ],
        })
      }
      if (path.startsWith("/api/v1/substrate.reamde.dev/core/kind")) {
        return jsonResponse(200, { kinds: wire.kinds ?? [CORE_KIND] })
      }
      if (path === CATALOG_PATH) {
        return jsonResponse(200, {
          items: wire.catalog ?? [
            PEOPLE,
            SCHEDULING,
            TASKS,
            PEBBLE,
            FIRECRAWL,
            GOOGLE,
          ],
        })
      }
      if (path === SHIPPED_PATH) {
        return jsonResponse(200, { items: wire.shipped ?? [] })
      }
      // Every chain step re-reads its own entry before it takes it: a
      // confirmation names one plan at one changelog head.
      if (path.startsWith(`${CATALOG_PATH}/`) && method === "GET") {
        const id = decodeURIComponent(path.slice(CATALOG_PATH.length + 1))
        const items = wire.catalog ?? [
          PEOPLE,
          SCHEDULING,
          TASKS,
          PEBBLE,
          FIRECRAWL,
          GOOGLE,
        ]
        const found = (wire.fresh?.(id) ?? items.find((i) => i.id === id))!
        return found
          ? jsonResponse(200, found)
          : jsonResponse(404, {
              error: { code: "not_found", message: `no bundle ${id}` },
            })
      }
      if (
        (path.endsWith("/install") || path.endsWith("/import")) &&
        method === "POST"
      ) {
        const verb = path.endsWith("/install") ? "/install" : "/import"
        const id = decodeURIComponent(
          path.slice(CATALOG_PATH.length + 1, -verb.length)
        )
        return (
          wire.take?.(id) ??
          jsonResponse(200, {
            id: verb === "/import" ? `${HOME}/people` : id,
            name: "people",
            authority: HOME,
            package: "people",
            installed: true,
            enabled: true,
          })
        )
      }
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

  /** The import calls made, with the confirmation each body carried. */
  function importCalls(): { id: string; confirm?: unknown }[] {
    return fetchMock.mock.calls
      .filter(
        ([url, init]) =>
          String(url).endsWith("/import") &&
          (init as RequestInit | undefined)?.method === "POST"
      )
      .map(([url, init]) => {
        const raw = (init as RequestInit | undefined)?.body
        const body = raw
          ? (JSON.parse(String(raw)) as { confirm?: unknown })
          : {}
        return {
          id: decodeURIComponent(
            String(url).slice(CATALOG_PATH.length + 1, -"/import".length)
          ),
          confirm: body.confirm,
        }
      })
  }

  /** The catalog ids the page imported, in the order it sent them. */
  function importedIDs(): string[] {
    return fetchMock.mock.calls
      .filter(
        ([url, init]) =>
          String(url).endsWith("/import") &&
          (init as RequestInit | undefined)?.method === "POST"
      )
      .map(([url]) =>
        decodeURIComponent(
          String(url).slice(CATALOG_PATH.length + 1, -"/import".length)
        )
      )
  }

  async function rowOf(name: string): Promise<HTMLElement> {
    const cell = await screen.findByText(name)
    return cell.closest("tr") as HTMLElement
  }

  function expand(row: HTMLElement): HTMLElement {
    fireEvent.click(within(row).getByLabelText("Toggle row details"))
    return row.nextElementSibling as HTMLElement
  }

  it("says a new repository ships core alone and takes the rest from here", async () => {
    renderPage(<RegistryPage />)
    await screen.findByText("people")
    expect(screen.getByText(/A new repository holds/)).toBeTruthy()
  })

  it("lists the two tiers in their own sections", async () => {
    renderPage(<RegistryPage />)
    await screen.findByText("people")
    const providers = screen
      .getByRole("heading", { name: "Providers" })
      .closest("section") as HTMLElement
    const samples = screen
      .getByRole("heading", { name: "Samples" })
      .closest("section") as HTMLElement
    expect(within(providers).getByText("google")).toBeTruthy()
    expect(within(providers).queryByText("people")).toBeNull()
    expect(within(samples).getByText("people")).toBeTruthy()
    expect(within(samples).getByText("tasks")).toBeTruthy()
    expect(within(samples).queryByText("google")).toBeNull()
    // The section says where an import lands, in its own copy.
    expect(
      within(samples).getByText(
        `Kinds to copy. Importing one lands them under ${HOME}, yours to edit.`
      )
    ).toBeTruthy()
  })

  it("a sample offers Import and previews the id it lands under", async () => {
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    expect(within(people).getByRole("button", { name: "Import" })).toBeTruthy()
    expect(within(people).getByText(`lands as ${HOME}/people`)).toBeTruthy()
  })

  it("counts kinds and functions, and nothing else", async () => {
    renderPage(<RegistryPage />)
    await screen.findByText("people")
    const samples = screen
      .getByRole("heading", { name: "Samples" })
      .closest("section") as HTMLElement
    const headers = within(samples)
      .getAllByRole("columnheader")
      .map((h) => h.textContent)
    expect(headers).toContain("kinds")
    expect(headers).toContain("functions")
    // An account count is zero for everything nobody has connected, and
    // nobody knows what a live row count of a registry entry means.
    expect(headers.some((h) => h?.includes("accounts"))).toBe(false)
    expect(headers.some((h) => h?.includes("live rows"))).toBe(false)
  })

  it("a provider offers Install, under the authority that publishes it", async () => {
    renderPage(<RegistryPage />)
    const google = await rowOf("google")
    expect(within(google).getByRole("button", { name: /Install/ })).toBeTruthy()
    expect(within(google).queryByText(/lands as/)).toBeNull()
  })

  it("shows the setup chip beside the lifecycle badge, never instead of it", async () => {
    serve({
      statuses: [
        {
          accounts: 0,
          functions: 0,
          kinds: 0,
          liveRecords: 0,
          id: GOOGLE.id,
          name: "google",
          authority: "providers.substrate.reamde.dev",
          package: "google",
          installed: true,
          enabled: true,
          inputs: [
            {
              name: "client",
              kind: "providers.substrate.reamde.dev/google/config",
            },
          ],
          setup: [
            {
              code: "missing",
              input: "client",
              kind: "providers.substrate.reamde.dev/google/config",
              message: "no config record exists yet",
            },
          ],
        },
        peopleStatus(),
      ],
    })
    renderPage(<RegistryPage />)
    const google = await rowOf("google")
    expect(within(google).getByText("enabled")).toBeTruthy()
    expect(within(google).getByText("1 setup step")).toBeTruthy()
    // A bundle with no setup shows the lifecycle badge alone.
    const people = await rowOf("people")
    expect(within(people).getByText("enabled")).toBeTruthy()
    expect(within(people).queryByText(/setup step/)).toBeNull()
  })

  it("says what it adds, in sections, each name with what it is", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("google"))
    expect(within(detail).getByText("3 kinds")).toBeTruthy()
    for (const name of ["config", "account", "contact"]) {
      expect(within(detail).getByText(name)).toBeTruthy()
    }
    expect(within(detail).getByText("1 function")).toBeTruthy()
    expect(within(detail).getByText("syncgoogle")).toBeTruthy()
    // A provider declares no mapping at all (decision record 0049), so its
    // row says nothing about one.
    expect(within(detail).queryByText("Links")).toBeNull()
    expect(within(detail).queryByText(/waiting|blocked|ready/)).toBeNull()
  })

  it("says what a traits-only sample adds, which is its traits", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("scheduling"))
    expect(within(detail).getByText("1 trait")).toBeTruthy()
    expect(within(detail).getByText("recurring")).toBeTruthy()
    expect(
      within(detail).getByText("a repeat rule stored and never expanded")
    ).toBeTruthy()
  })

  it("names each trigger with what it runs", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("pebble"))
    expect(within(detail).getByText("1 trigger")).toBeTruthy()
    expect(within(detail).getByText("pebble-webhook")).toBeTruthy()
    expect(within(detail).getByText("runs ingest")).toBeTruthy()
  })

  it("names the settings and secrets it ships, and says nothing of their values", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("firecrawl"))
    const settings = within(detail)
      .getByText("Settings and secrets")
      .closest("section") as HTMLElement
    expect(within(settings).getByText("apiKey")).toBeTruthy()
    expect(within(settings).getByText("a secret")).toBeTruthy()
    expect(within(settings).getByText("baseUrl")).toBeTruthy()
    expect(
      within(settings).getByText(/values are yours to fill in/)
    ).toBeTruthy()
    // A setting is a record, and it is counted once: it does not turn up
    // again in the trailing line.
    expect(within(detail).queryByText(/Also ships/)).toBeNull()
  })

  it("hands a fresh import straight to its Setup when a setting is empty", async () => {
    serve({
      take: () =>
        jsonResponse(200, {
          id: `${HOME}/people`,
          name: "people",
          authority: HOME,
          package: "people",
          installed: true,
          enabled: true,
          setup: [
            {
              code: "setting",
              kind: "substrate.reamde.dev/core/secret",
              record: `${HOME}/people/apiKey`,
              message: "API key is not set",
            },
          ],
        }),
    })
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    fireEvent.click(within(people).getByRole("button", { name: "Import" }))
    // The LANDED id, not the shipped one the click named.
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/registry/$id",
        params: { id: `${HOME}/people` },
        hash: "setup",
      })
    )
  })

  it("leaves the reader on the list when the import needs nothing", async () => {
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    fireEvent.click(within(people).getByRole("button", { name: "Import" }))
    expect(
      await screen.findByText(`people imported as ${HOME}/people.`)
    ).toBeTruthy()
    expect(navigate).not.toHaveBeenCalled()
  })

  it("says in one sentence what a sample's links do, with no state word", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("people"))
    expect(
      within(detail).getByText(
        "Links google contact records onto person. " +
          "Each link lands when that provider is installed and this sample is imported again."
      )
    ).toBeTruthy()
  })

  it("drops the version line, the tier sentence and the empty placeholder", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("people"))
    expect(within(detail).queryByText(/a published package/i)).toBeNull()
    expect(within(detail).queryByText(/Importing lands it as/)).toBeNull()
    expect(within(detail).queryByText("none")).toBeNull()
  })

  it("shows a kind's own description, and does not link it before it is here", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("people"))
    const person = within(detail).getByText("person")
    expect(person.tagName).toBe("SPAN")
    expect(within(detail).getByText("one human being")).toBeTruthy()
  })

  // A provider whose `requires:` names a sample package cannot be installed
  // while samples import under this repository's authority: the import lands
  // `<home>/people`, and google asks for
  // `samples.substrate.reamde.dev/people`. The button is still the button:
  // nothing here is disabled, and the server's refusal is the one that
  // speaks.
  it("names the whole missing chain on the row, and offers to take it", async () => {
    renderPage(<RegistryPage />)
    const google = await rowOf("google")
    const button = within(google).getByRole("button", { name: "Install all" })
    expect(button.hasAttribute("disabled")).toBe(false)
    expect(
      within(google).getByText(/needs samples\.substrate\.reamde\.dev\/people/)
    ).toBeTruthy()
  })

  it("walks the chain the wire does not carry, and nests it", async () => {
    renderPage(<RegistryPage />)
    const pebble = await rowOf("pebble")
    // pebble requires tasks; tasks requires people and scheduling. The wire
    // says only the first.
    expect(
      within(pebble).getByText(
        `needs ${HOME}/people, ${HOME}/scheduling, ${HOME}/tasks`
      )
    ).toBeTruthy()
    const detail = expand(pebble)
    expect(within(detail).getByText(`${HOME}/tasks`)).toBeTruthy()
    expect(within(detail).getByText(`${HOME}/people`)).toBeTruthy()
    expect(within(detail).getByText(`${HOME}/scheduling`)).toBeTruthy()
  })

  it("imports the whole chain leaves first, one call each", async () => {
    renderPage(<RegistryPage />)
    const pebble = await rowOf("pebble")
    fireEvent.click(within(pebble).getByRole("button", { name: "Import all" }))
    await waitFor(() => expect(importedIDs()).toHaveLength(4))
    expect(importedIDs()).toEqual([
      "samples.substrate.reamde.dev/people",
      "samples.substrate.reamde.dev/scheduling",
      "samples.substrate.reamde.dev/tasks",
      "samples.substrate.reamde.dev/pebble",
    ])
  })

  it("stops at the first refusal, names it, and leaves what landed showing as here", async () => {
    const wire: Wire = { statuses: [] }
    wire.take = (id) => {
      if (id === "samples.substrate.reamde.dev/scheduling") {
        return jsonResponse(422, {
          error: {
            code: "validation",
            message: "validation error",
            problems: ["scheduling declares a retired name"],
          },
        })
      }
      // The server has it now, so the next status read says so.
      wire.statuses = [...(wire.statuses ?? []), peopleStatus()]
      return jsonResponse(200, peopleStatus())
    }
    serve(wire)
    renderPage(<RegistryPage />)
    const pebble = await rowOf("pebble")
    fireEvent.click(within(pebble).getByRole("button", { name: "Import all" }))
    expect(await screen.findByText("Importing scheduling failed")).toBeTruthy()
    expect(
      await screen.findByText("scheduling declares a retired name")
    ).toBeTruthy()
    // The chain stopped where it refused: pebble itself was never sent.
    expect(importedIDs()).toEqual([
      "samples.substrate.reamde.dev/people",
      "samples.substrate.reamde.dev/scheduling",
    ])
    // …and people, which DID land, reads as here rather than still offering
    // an import.
    const people = await rowOf("people")
    await waitFor(() =>
      expect(within(people).getByText("enabled")).toBeTruthy()
    )
  })

  it("confirms each step against the preview read just before it", async () => {
    // The copy of people here was edited, so importing it again replaces
    // those edits and the door wants the confirmation its CURRENT preview
    // hands out. The one the page loaded with is a plan at an older head.
    const edited = (planHash: string, changelogSeq: number): CatalogItem => ({
      ...PEOPLE,
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
      catalog: [edited("stale", 1), SCHEDULING, TASKS, PEBBLE],
      fresh: (id) => (id === PEOPLE.id ? edited("fresh", 9) : undefined),
    })
    renderPage(<RegistryPage />)
    const pebble = await rowOf("pebble")
    fireEvent.click(within(pebble).getByRole("button", { name: "Import all" }))
    // It asks before replacing anybody's edits, and only then runs.
    const dialog = await screen.findByRole("dialog")
    expect(
      within(dialog).getByText(/Replace your edits to people\?/)
    ).toBeTruthy()
    fireEvent.click(within(dialog).getByRole("button", { name: "Import all" }))
    await waitFor(() => expect(importCalls().length).toBeGreaterThan(0))
    expect(importCalls()[0].confirm).toEqual({
      planHash: "fresh",
      changelogSeq: 9,
    })
  })

  it("refuses a chain it cannot order, and imports nothing", async () => {
    const a = bundle({
      id: "samples.substrate.reamde.dev/aaa",
      name: "aaa",
      authority: "samples.substrate.reamde.dev",
      package: "aaa",
      tier: "sample",
      requires: ["samples.substrate.reamde.dev/bbb"],
    })
    const b = bundle({
      id: "samples.substrate.reamde.dev/bbb",
      name: "bbb",
      authority: "samples.substrate.reamde.dev",
      package: "bbb",
      tier: "sample",
      requires: ["samples.substrate.reamde.dev/aaa"],
    })
    serve({ catalog: [a, b] })
    renderPage(<RegistryPage />)
    const row = await rowOf("aaa")
    fireEvent.click(within(row).getByRole("button", { name: "Import all" }))
    expect(
      await screen.findByText("aaa cannot be imported from here")
    ).toBeTruthy()
    expect(
      await screen.findByText(/require each other, so there is no order/)
    ).toBeTruthy()
    expect(importedIDs()).toEqual([])
  })

  it("imports a sample through the import door, with the SHIPPED id", async () => {
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    const button = within(people).getByRole("button", { name: "Import" })
    expect(button.hasAttribute("disabled")).toBe(false)
    fireEvent.click(button)
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) =>
            String(url) ===
              `${CATALOG_PATH}/samples.substrate.reamde.dev%2Fpeople/import` &&
            (init as RequestInit).method === "POST"
        )
      ).toBe(true)
    )
    // The toast names where it landed, which is not what the reader clicked.
    expect(
      await screen.findByText(`people imported as ${HOME}/people.`)
    ).toBeTruthy()
  })

  it("hands a fresh import straight to its Setup when a setting is empty", async () => {
    serve({
      take: () =>
        jsonResponse(200, {
          id: `${HOME}/people`,
          name: "people",
          authority: HOME,
          package: "people",
          installed: true,
          enabled: true,
          setup: [
            {
              code: "setting",
              kind: "substrate.reamde.dev/core/secret",
              record: `${HOME}/people/apiKey`,
              message: "API key is not set",
            },
          ],
        }),
    })
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    fireEvent.click(within(people).getByRole("button", { name: /^Import$/ }))
    // The LANDED id, not the shipped one the click named.
    await waitFor(() =>
      expect(navigate).toHaveBeenCalledWith({
        to: "/registry/$id",
        params: { id: `${HOME}/people` },
        hash: "setup",
      })
    )
  })

  it("leaves the reader on the list when the import needs nothing", async () => {
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    fireEvent.click(within(people).getByRole("button", { name: /^Import$/ }))
    expect(
      await screen.findByText(`people imported as ${HOME}/people.`)
    ).toBeTruthy()
    expect(navigate).not.toHaveBeenCalled()
  })

  it("surfaces a server refusal in the server's own words", async () => {
    const problem =
      "bundle samples.substrate.reamde.dev/people: data.requires names substrate.reamde.dev/core, which this repository does not have — import that package first"
    serve({
      take: () =>
        jsonResponse(422, {
          error: {
            code: "validation",
            message: "validation error",
            problems: [problem],
          },
        }),
    })
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    fireEvent.click(within(people).getByRole("button", { name: "Import" }))
    expect(await screen.findByText(problem)).toBeTruthy()
  })

  describe("once a sample is imported", () => {
    beforeEach(() =>
      serve({
        statuses: [peopleStatus()],
        kinds: [CORE_KIND, PERSON_KIND],
        catalog: [PEOPLE, TASKS, GOOGLE],
      })
    )

    it("folds the landed status onto the shipped closure's row", async () => {
      renderPage(<RegistryPage />)
      const people = await rowOf("people")
      expect(within(people).getByText("enabled")).toBeTruthy()
      expect(
        within(people).queryByRole("button", { name: "Import" })
      ).toBeNull()
    })

    it("links the imported closure's kind to its collection under this repository", async () => {
      renderPage(<RegistryPage />)
      const detail = expand(await rowOf("people"))
      const person = within(detail).getByText("person")
      expect(person.tagName).toBe("A")
      expect(person.getAttribute("data-to")).toBe("/data/$authority/$pkg/$name")
      expect(JSON.parse(person.getAttribute("data-params")!)).toEqual({
        authority: HOME,
        pkg: "people",
        name: "person",
      })
    })

    it("marks the rehomed requirement satisfied and asks only for the rest", async () => {
      renderPage(<RegistryPage />)
      const tasks = await rowOf("tasks")
      // people is here now, under this repository's own authority, so the
      // chain is scheduling alone.
      expect(within(tasks).getByText(`needs ${HOME}/scheduling`)).toBeTruthy()
      const detail = expand(tasks)
      expect(within(detail).getByText(`${HOME}/people`)).toBeTruthy()
      expect(within(detail).getByText("here")).toBeTruthy()
    })
  })

  describe("upgrades: the shipped closure moved past the stored one", () => {
    const MOVED = {
      ...GOOGLE,
      installed: true,
      upgrade: {
        available: true,
        from: 1,
        to: 2,
        work: 0,
        lossy: false,
        changes: [
          {
            kind: "kind",
            id: "providers.substrate.reamde.dev/google/contact",
            from: 1,
            to: 2,
          },
        ],
      },
    }

    function googleStatus(): BundleStatus {
      return {
        accounts: 0,
        functions: 0,
        kinds: 0,
        liveRecords: 0,
        id: GOOGLE.id,
        name: "google",
        authority: "providers.substrate.reamde.dev",
        package: "google",
        installed: true,
        enabled: true,
      }
    }

    it("offers Upgrade on the moved provider and rides the install verb", async () => {
      serve({ statuses: [googleStatus()], catalog: [MOVED, PEOPLE] })
      renderPage(<RegistryPage />)
      const google = await rowOf("google")
      expect(within(google).getByText("update 1 → 2")).toBeTruthy()
      fireEvent.click(within(google).getByRole("button", { name: /Upgrade/ }))
      await waitFor(() => {
        expect(
          fetchMock.mock.calls.some(
            ([url, init]) =>
              String(url).endsWith("/install") &&
              (init as RequestInit | undefined)?.method === "POST" &&
              decodeURIComponent(String(url)).includes(GOOGLE.id)
          )
        ).toBe(true)
      })
    })

    /** The install bodies the page sent, decoded, in order. */
    function installBodies(): { confirm?: { planHash: string } }[] {
      return fetchMock.mock.calls
        .filter(
          ([url, init]) =>
            String(url).endsWith("/install") &&
            (init as RequestInit | undefined)?.method === "POST"
        )
        .map(([, init]) => {
          const raw = (init as RequestInit | undefined)?.body
          return raw ? JSON.parse(String(raw)) : {}
        })
    }

    it("a lossy upgrade asks first and confirms the previewed hash", async () => {
      const lossy = {
        ...MOVED,
        upgrade: {
          ...MOVED.upgrade,
          work: 3,
          lossy: true,
          planHash: "cafe",
          changelogSeq: 41,
          steps: [
            {
              step: "null" as const,
              kind: "providers.substrate.reamde.dev/google/contact",
              property: "middleName",
              records: 3,
              lossy: true,
            },
          ],
        },
      }
      serve({ statuses: [googleStatus()], catalog: [lossy, PEOPLE] })
      renderPage(<RegistryPage />)
      const google = await rowOf("google")
      fireEvent.click(within(google).getByRole("button", { name: /Upgrade/ }))
      // Nothing was sent: the dialog lists the loss and asks.
      expect(installBodies()).toEqual([])
      const dialog = await screen.findByRole("dialog")
      expect(
        within(dialog).getByText(/drops middleName on .*3 live records/)
      ).toBeTruthy()
      fireEvent.click(
        within(dialog).getByRole("button", { name: /remove values/ })
      )
      await waitFor(() => expect(installBodies()).toHaveLength(1))
      expect(installBodies()[0].confirm).toEqual({
        planHash: "cafe",
        changelogSeq: 41,
      })
    })

    it("a stale confirmation re-reads the preview and confirms the fresh plan", async () => {
      const step = {
        step: "null" as const,
        kind: "providers.substrate.reamde.dev/google/contact",
        property: "middleName",
        records: 3,
        lossy: true,
      }
      const stale = {
        ...MOVED,
        upgrade: {
          ...MOVED.upgrade,
          work: 3,
          lossy: true,
          planHash: "cafe",
          changelogSeq: 41,
          steps: [step],
        },
      }
      // A record landed in between: the head moved and one more record
      // carries the property, so the plan reads differently.
      const fresh = {
        ...stale,
        upgrade: {
          ...stale.upgrade,
          work: 4,
          planHash: "f00d",
          changelogSeq: 42,
          steps: [{ ...step, records: 4 }],
        },
      }
      const wire: Wire = {
        statuses: [googleStatus()],
        catalog: [stale, PEOPLE],
      }
      let installs = 0
      wire.take = () => {
        installs++
        if (installs === 1) {
          wire.catalog = [fresh, PEOPLE]
          return jsonResponse(409, {
            error: {
              code: "conflict",
              message: "the changelog moved since the plan was previewed",
            },
          })
        }
        return jsonResponse(200, googleStatus())
      }
      serve(wire)
      renderPage(<RegistryPage />)
      const google = await rowOf("google")
      fireEvent.click(within(google).getByRole("button", { name: /Upgrade/ }))
      // The toast that announces the stale preview is a dialog too, so the
      // loss dialog is found by its title.
      const lossDialog = () =>
        screen.getByRole("dialog", { name: /and remove values/ })
      await screen.findByRole("dialog", { name: /and remove values/ })
      fireEvent.click(
        within(lossDialog()).getByRole("button", { name: /remove values/ })
      )
      await waitFor(() => expect(installs).toBe(1))
      expect(installBodies()[0].confirm?.planHash).toBe("cafe")
      // The preview is read again (a second catalog GET), the dialog stays
      // open on the re-read plan, which now names four records, and the next
      // click confirms the fresh hash.
      const catalogReads = () =>
        fetchMock.mock.calls.filter(([url]) => String(url) === CATALOG_PATH)
          .length
      await waitFor(() => expect(catalogReads()).toBeGreaterThanOrEqual(2))
      expect(
        within(lossDialog()).getByText(/Records changed since this plan/)
      ).toBeTruthy()
      await within(lossDialog()).findByText(
        /drops middleName on .*4 live records/,
        {},
        { timeout: 3000 }
      )
      fireEvent.click(
        within(lossDialog()).getByRole("button", { name: /remove values/ })
      )
      await waitFor(() => expect(installs).toBe(2))
      expect(installBodies()[1].confirm).toEqual({
        planHash: "f00d",
        changelogSeq: 42,
      })
    })

    /** A held SAMPLE copy: imported at 4, stamped with its origin, and the
     * binary now ships the sample at 5 (decision record 0070). */
    function heldPeople(upgrade: CatalogItem["upgrade"]): CatalogItem {
      return {
        ...PEOPLE,
        version: 5,
        installed: true,
        origin: PEOPLE.id,
        originVersion: 4,
        upgrade,
      }
    }

    /** peopleStatus with the stamp and version the copy carries. */
    function heldPeopleStatus(): BundleStatus {
      return {
        ...peopleStatus(),
        version: 4,
        origin: PEOPLE.id,
        originVersion: 4,
      }
    }

    /** The import requests made so far, each with the confirmation its body
     * carried (undefined for a bare POST). */
    function importCalls(): { id: string; confirm?: unknown }[] {
      return fetchMock.mock.calls
        .filter(
          ([url, init]) =>
            String(url).endsWith("/import") &&
            (init as RequestInit | undefined)?.method === "POST"
        )
        .map(([url, init]) => {
          const raw = (init as RequestInit | undefined)?.body
          const body = raw
            ? (JSON.parse(String(raw)) as { confirm?: unknown })
            : {}
          return {
            id: decodeURIComponent(
              String(url).slice(CATALOG_PATH.length + 1, -"/import".length)
            ),
            confirm: body.confirm,
          }
        })
    }

    it("offers Upgrade on a moved sample copy and rides the import verb", async () => {
      serve({
        statuses: [heldPeopleStatus()],
        catalog: [
          heldPeople({
            available: true,
            from: 4,
            to: 5,
            work: 0,
            lossy: false,
          }),
        ],
      })
      renderPage(<RegistryPage />)
      const people = await rowOf("people")
      expect(within(people).getByText("update 4 → 5")).toBeTruthy()
      fireEvent.click(within(people).getByRole("button", { name: /Upgrade/ }))
      await waitFor(() =>
        expect(importCalls()).toEqual([{ id: PEOPLE.id, confirm: undefined }])
      )
    })

    it("an edited sample copy asks before its edits are replaced, and confirms the previewed plan", async () => {
      serve({
        statuses: [{ ...heldPeopleStatus(), modified: true }],
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
        ],
      })
      renderPage(<RegistryPage />)
      const people = await rowOf("people")
      fireEvent.click(within(people).getByRole("button", { name: /Upgrade/ }))
      const dialog = await screen.findByRole("dialog")
      expect(
        within(dialog).getByText(/Upgrade people and replace your edits\?/)
      ).toBeTruthy()
      expect(dialog.textContent).toContain(`You edited ${HOME}/people`)
      expect(importCalls()).toEqual([])
      fireEvent.click(
        within(dialog).getByRole("button", {
          name: /Upgrade and replace my edits/,
        })
      )
      await waitFor(() =>
        expect(importCalls()).toEqual([
          { id: PEOPLE.id, confirm: { planHash: "d15c", changelogSeq: 9 } },
        ])
      )
    })

    it("a lossy refusal at the same head re-reads the preview, and a lossless re-read closes the dialog", async () => {
      const step = {
        step: "null" as const,
        kind: "providers.substrate.reamde.dev/google/contact",
        property: "middleName",
        records: 3,
        lossy: true,
      }
      const lossy = {
        ...MOVED,
        upgrade: {
          ...MOVED.upgrade,
          work: 3,
          lossy: true,
          planHash: "cafe",
          changelogSeq: 41,
          steps: [step],
        },
      }
      // A second lossy provider in the same section: the dialog it opens
      // afterwards must not inherit the first one's "changed" notice.
      const linear = {
        ...lossy,
        id: "providers.substrate.reamde.dev/linear",
        name: "linear",
        package: "linear",
      }
      const linearStatus = {
        ...googleStatus(),
        id: linear.id,
        name: "linear",
        package: "linear",
      }
      const wire: Wire = {
        statuses: [googleStatus(), linearStatus],
        catalog: [lossy, linear, PEOPLE],
      }
      let installs = 0
      wire.take = () => {
        installs++
        // The server changed under the same records: the plan reads
        // differently and, re-read, removes nothing.
        wire.catalog = [MOVED, linear, PEOPLE]
        return jsonResponse(403, {
          error: {
            code: "lossy",
            message: "the confirmation is for another plan",
          },
        })
      }
      serve(wire)
      renderPage(<RegistryPage />)
      const google = await rowOf("google")
      fireEvent.click(within(google).getByRole("button", { name: /Upgrade/ }))
      const dialog = await screen.findByRole("dialog", {
        name: /and remove values/,
      })
      fireEvent.click(
        within(dialog).getByRole("button", { name: /remove values/ })
      )
      await waitFor(() => expect(installs).toBe(1))
      await waitFor(() =>
        expect(
          screen.queryByRole("dialog", { name: /and remove values/ })
        ).toBeNull()
      )
      expect(
        within(google).getByRole("button", { name: /Upgrade/ })
      ).toBeTruthy()

      const linearRow = await rowOf("linear")
      fireEvent.click(
        within(linearRow).getByRole("button", { name: /Upgrade/ })
      )
      const next = await screen.findByRole("dialog", {
        name: /Upgrade linear and remove values/,
      })
      expect(
        within(next).queryByText(/Records changed since this plan/)
      ).toBeNull()
    })

    it("a blocked upgrade is stated, never offered", async () => {
      serve({
        statuses: [googleStatus()],
        catalog: [
          {
            ...MOVED,
            upgrade: {
              ...MOVED.upgrade,
              blockers: [
                'kind providers.substrate.reamde.dev/google/contact: property "middleName" dropped while 3 live records still carry it — null it on them first',
              ],
            },
          },
          PEOPLE,
        ],
      })
      renderPage(<RegistryPage />)
      const google = await rowOf("google")
      expect(within(google).getByText("upgrade blocked")).toBeTruthy()
      expect(
        within(google).queryByRole("button", { name: /Upgrade/ })
      ).toBeNull()
      const detail = expand(google)
      expect(within(detail).getByText(/middleName/)).toBeTruthy()
    })

    it("a preview the server could not run is stated as blocked", async () => {
      // The server attaches the failure as one fixed blocker line and no
      // motion (api catalogItemFor). It reads as a blocked upgrade, never as
      // an entry with nothing to say.
      serve({
        statuses: [googleStatus()],
        catalog: [
          {
            ...GOOGLE,
            installed: true,
            upgrade: {
              available: false,
              work: 0,
              lossy: false,
              blockers: ["the upgrade preview failed; see the server log"],
            },
          },
          PEOPLE,
        ],
      })
      renderPage(<RegistryPage />)
      const google = await rowOf("google")
      expect(within(google).getByText("upgrade blocked")).toBeTruthy()
      expect(
        within(google).queryByRole("button", { name: /Upgrade/ })
      ).toBeNull()
      const detail = expand(google)
      expect(
        within(detail).getByText(
          "the upgrade preview failed; see the server log"
        )
      ).toBeTruthy()
      // The lead says the preview failed, not that live records block it.
      expect(within(detail).getByText(/could not be previewed/)).toBeTruthy()
      expect(within(detail).queryByText(/live records still hold/)).toBeNull()
    })

    it("a refused core boot upgrade is stated above the sections", async () => {
      const guard =
        'type substrate.reamde.dev/core/llmprovider: property "label" dropped while 1 live records still carry it — null it on them first'
      serve({
        shipped: [
          {
            package: "substrate.reamde.dev/core",
            upgrade: {
              available: true,
              from: 16,
              to: 17,
              blockers: [guard],
              work: 2,
              lossy: false,
              planHash: "cafe",
              changelogSeq: 41,
              steps: [
                {
                  step: "rename" as const,
                  kind: "substrate.reamde.dev/core/llmprovider",
                  property: "protocol",
                  from: "wire",
                  to: "protocol",
                  records: 2,
                },
              ],
            },
          },
        ],
      })
      renderPage(<RegistryPage />)
      await screen.findByText("people")
      const notice = screen.getByRole("alert")
      expect(within(notice).getByText("substrate.reamde.dev/core")).toBeTruthy()
      expect(within(notice).getByText("16 → 17")).toBeTruthy()
      expect(within(notice).getByText(guard)).toBeTruthy()
      // The rename the upgrade would perform is stated with its count, so the
      // operator knows the boot rewrites records, not only declarations.
      expect(
        within(notice).getByText(
          "renames wire to protocol on substrate.reamde.dev/core/llmprovider: 2 live records rewritten"
        )
      ).toBeTruthy()
    })

    it("an admitted core upgrade says a restart lands it", async () => {
      // The owner migrated the last blocking record: the preview has no
      // blockers, but the store is still old until the server starts again,
      // and the notice has to say so instead of vanishing.
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
      renderPage(<RegistryPage />)
      await screen.findByText("people")
      const notice = screen.getByRole("alert")
      expect(within(notice).getByText("substrate.reamde.dev/core")).toBeTruthy()
      expect(within(notice).getByText("16 → 17")).toBeTruthy()
      expect(
        within(notice).getByText(/lands when the server starts again/)
      ).toBeTruthy()
      expect(within(notice).queryByText(/was refused/)).toBeNull()
    })

    it("a core package at the shipped version states nothing", async () => {
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
      renderPage(<RegistryPage />)
      await screen.findByText("people")
      expect(screen.queryByRole("alert")).toBeNull()
    })

    it("a current bundle offers nothing", async () => {
      serve({
        statuses: [googleStatus()],
        catalog: [{ ...GOOGLE, installed: true }, PEOPLE],
      })
      renderPage(<RegistryPage />)
      const google = await rowOf("google")
      expect(
        within(google).queryByRole("button", { name: /Upgrade/ })
      ).toBeNull()
      expect(within(google).queryByText(/update 1/)).toBeNull()
    })
  })
})
