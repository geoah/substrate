/** The Registry page as the reader meets it on a FRESH repository: core alone
 * is held, so every row is an invitation and the only questions that matter are
 * what it will do, where it will land, and whether it can happen at all.
 *
 * What is asserted here: the TWO SECTIONS and the door each one offers
 * (Install for a provider, Import as yours for a sample, decision record
 * 0048), the identity a sample previews before it is imported, the disclosure
 * (a row opens onto its closure: kinds, functions, triggers, requirements),
 * the GATE (the button is refused client-side while a `requires:` package is
 * missing, in the same words the server would use, and a sample's
 * requirements are read REHOMED), and the refusal path (a server problem rides
 * the toast verbatim, never flattened into "the import failed"). */

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
import type { BundleStatus, CatalogItem, KindInfo } from "@/lib/api/types"

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
    closure: {},
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
    kinds: [
      "samples.substrate.reamde.dev/people/person",
      "samples.substrate.reamde.dev/people/personmerge",
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
  requires: ["samples.substrate.reamde.dev/people"],
  closure: { kinds: ["samples.substrate.reamde.dev/tasks/task"] },
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
  plural: "bundles",
  source: "builtin",
}

/** The person kind as it exists AFTER the import: under this repository's own
 * authority, since that is what the import wrote. */
const PERSON_KIND: KindInfo = {
  identity: `${HOME}/people/person`,
  name: "person",
  authority: HOME,
  package: "people",
  version: 1,
  plural: "persons",
  source: "installed",
}

/** The status of the people sample once imported: its id is the LANDED one. */
function peopleStatus(): BundleStatus {
  return {
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
  /** The repository's own authority; "" models a repository that names none. */
  authority?: string
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
          items: wire.catalog ?? [PEOPLE, TASKS, GOOGLE],
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
    expect(screen.getByText(/A new repository ships/)).toBeTruthy()
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
        `Vocabulary to copy: importing one lands it under ${HOME}, yours to edit.`
      )
    ).toBeTruthy()
  })

  it("a sample offers Import as yours and previews the id it lands under", async () => {
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    expect(
      within(people).getByRole("button", { name: /Import as yours/ })
    ).toBeTruthy()
    expect(within(people).getByText(`lands as ${HOME}/people`)).toBeTruthy()
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

  it("discloses the closure in place — kinds, functions, triggers, requirements", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("google"))
    // What it adds.
    for (const name of ["config", "account", "contact"]) {
      expect(within(detail).getByText(name)).toBeTruthy()
    }
    expect(within(detail).getByText("syncgoogle")).toBeTruthy()
    expect(within(detail).getByText("ongooglesync")).toBeTruthy()
    // What it is, and what it declares against.
    expect(within(detail).getByText(/a published package/i)).toBeTruthy()
    expect(
      within(detail).getByTitle(
        "samples.substrate.reamde.dev/people is not imported — the import is refused until it is"
      )
    ).toBeTruthy()
  })

  it("a sample's disclosure says it lands as this repository's own", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("people"))
    expect(
      within(detail).getByText((text) =>
        text.includes(`Importing lands it as ${HOME}/people`)
      )
    ).toBeTruthy()
  })

  it("shows a closure's declared kinds even before it is imported (no route yet)", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("people"))
    const person = within(detail).getByText("person")
    // Not imported: the kind exists on paper only, so it does not pretend to
    // link anywhere, and it is previewed under the authority it WILL have.
    expect(person.tagName).toBe("SPAN")
    expect(person.getAttribute("title")).toBe(`${HOME}/people/person`)
  })

  // THE TIER GAP, PINNED. A provider whose `requires:` names a sample package
  // cannot be installed from the console while samples import under this
  // repository's authority: the import lands `<home>/people`, and google asks
  // for `samples.substrate.reamde.dev/people`. Phase 4 of
  // docs/plans/providers-and-samples.md drops those requirements; until then
  // the API's install door is the only way, and this test says so out loud so
  // the state is deliberate rather than discovered.
  it("refuses the install while a required package is missing, naming it", async () => {
    renderPage(<RegistryPage />)
    const google = await rowOf("google")
    const button = within(google).getByRole("button", { name: /Install/ })
    expect(button.hasAttribute("disabled")).toBe(true)
    expect(
      within(google).getByText(
        "Import samples.substrate.reamde.dev/people, samples.substrate.reamde.dev/messaging and samples.substrate.reamde.dev/calendar first — this bundle declares against them."
      )
    ).toBeTruthy()
    // …and the row itself says what is missing, without opening anything.
    expect(
      within(google).getByText(/needs samples\.substrate\.reamde\.dev\/people/)
    ).toBeTruthy()
  })

  it("reads a SAMPLE's requirements rehomed: the packages the server will look for", async () => {
    renderPage(<RegistryPage />)
    const tasks = await rowOf("tasks")
    // tasks declares against samples.substrate.reamde.dev/people, but what the
    // import will need is this repository's own people package.
    expect(within(tasks).getByText(`needs ${HOME}/people`)).toBeTruthy()
    expect(
      within(tasks)
        .getByRole("button", { name: /Import as yours/ })
        .hasAttribute("disabled")
    ).toBe(true)
  })

  it("imports a sample through the import door, with the SHIPPED id", async () => {
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    const button = within(people).getByRole("button", {
      name: /Import as yours/,
    })
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
    fireEvent.click(
      within(people).getByRole("button", { name: /Import as yours/ })
    )
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
        within(people).queryByRole("button", { name: /Import as yours/ })
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

    it("marks the rehomed requirement satisfied and lets the import through", async () => {
      renderPage(<RegistryPage />)
      const tasks = await rowOf("tasks")
      expect(
        within(tasks)
          .getByRole("button", { name: /Import as yours/ })
          .hasAttribute("disabled")
      ).toBe(false)
      expect(within(tasks).queryByText(/needs /)).toBeNull()
      const detail = expand(tasks)
      expect(
        within(detail).getByTitle(`${HOME}/people is imported`)
      ).toBeTruthy()
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
