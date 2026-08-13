/** The Registry page as the reader meets it on a FRESH repository: core alone
 * is imported, so every row is an invitation and the only question that matters
 * is what the import will do and whether it can happen at all.
 *
 * What is asserted here: the disclosure (a row opens onto its closure — kinds,
 * functions, triggers, requirements — before anything is imported), the GATE
 * (Import is refused client-side while a `requires:` authority is missing, in
 * the same words the server would use), the two catalog facets (Vocabulary vs
 * Integration), and the refusal path (a server problem rides the toast
 * verbatim, never flattened into "the import failed"). */

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
import type { BundleStatus, CatalogBundle, KindInfo } from "@/lib/api/types"

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

const CATALOG_PATH = "/api/v1/core.substrate.reamde.dev/catalog"
const STATUS_PATH = "/api/v1/core.substrate.reamde.dev/bundles/status"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

function bundle(over: Partial<CatalogBundle>): CatalogBundle {
  return {
    id: "x.bundles.substrate.reamde.dev/x",
    name: "x",
    authority: "x.bundles.substrate.reamde.dev",
    description: "",
    version: "v1",
    resources: {},
    installed: false,
    ...over,
  }
}

/** The shipped registry, trimmed to what these assertions need: one vocabulary
 * bundle that declares against nothing, and one integration that declares
 * against three authorities a fresh repository does not have. */
const PEOPLE = bundle({
  id: "people.substrate.reamde.dev/people",
  name: "people",
  authority: "people.substrate.reamde.dev",
  description: "The shipped vocabulary for humans.",
  version: "v1alpha1",
  vocabulary: true,
  resources: {
    kinds: ["people.substrate.reamde.dev/person", "people.substrate.reamde.dev/personmerge"],
  },
})

const GOOGLE = bundle({
  id: "google.bundles.substrate.reamde.dev/google",
  name: "google",
  authority: "google.bundles.substrate.reamde.dev",
  description: "Connects a Google account — contacts, gmail and calendar.",
  inputs: {
    client: {
      kind: "google.bundles.substrate.reamde.dev/config",
      description: "The OAuth client record.",
    },
  },
  integration: true,
  requires: ["people.substrate.reamde.dev", "messaging.substrate.reamde.dev", "calendar.substrate.reamde.dev"],
  resources: {
    kinds: [
      "google.bundles.substrate.reamde.dev/config",
      "google.bundles.substrate.reamde.dev/account",
      "google.bundles.substrate.reamde.dev/contact",
    ],
    functions: ["google.bundles.substrate.reamde.dev/syncgoogle"],
    triggers: ["google.bundles.substrate.reamde.dev/ongooglesync"],
  },
})

const CORE_KIND: KindInfo = {
  identity: "core.substrate.reamde.dev/bundle",
  name: "bundle",
  authority: "core.substrate.reamde.dev",
  version: "v1",
  plural: "bundles",
  source: "builtin",
}

const PERSON_KIND: KindInfo = {
  identity: "people.substrate.reamde.dev/person",
  name: "person",
  authority: "people.substrate.reamde.dev",
  version: "v1alpha1",
  plural: "persons",
  source: "builtin",
}

function peopleStatus(): BundleStatus {
  return {
    id: PEOPLE.id,
    name: "people",
    authority: "people.substrate.reamde.dev",
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
  catalog?: CatalogBundle[]
  install?: (id: string) => Response
}

describe("RegistryPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  function serve(wire: Wire = {}) {
    fetchMock.mockImplementation(async (url, init) => {
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      const path = String(url)
      if (path === STATUS_PATH) {
        return jsonResponse(200, { bundles: wire.statuses ?? [] })
      }
      if (path.startsWith("/api/v1/core.substrate.reamde.dev/kinds")) {
        return jsonResponse(200, { kinds: wire.kinds ?? [CORE_KIND] })
      }
      if (path === CATALOG_PATH) {
        return jsonResponse(200, { catalog: wire.catalog ?? [PEOPLE, GOOGLE] })
      }
      if (path.endsWith("/install") && method === "POST") {
        const id = decodeURIComponent(
          path.slice(CATALOG_PATH.length + 1, -"/install".length)
        )
        return (
          wire.install?.(id) ??
          jsonResponse(200, {
            id,
            name: "people",
            authority: "people.substrate.reamde.dev",
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

  it("says a new repository ships core alone and imports the rest", async () => {
    renderPage(<RegistryPage />)
    await screen.findByText("people")
    expect(screen.getByText(/A new repository ships/)).toBeTruthy()
  })

  it("shows the setup chip beside the lifecycle badge, never instead of it", async () => {
    serve({
      statuses: [
        {
          id: GOOGLE.id,
          name: "google",
          authority: "google.bundles.substrate.reamde.dev",
          installed: true,
          enabled: true,
          inputs: [
            { name: "client", kind: "google.bundles.substrate.reamde.dev/config" },
          ],
          setup: [
            {
              code: "missing",
              input: "client",
              kind: "google.bundles.substrate.reamde.dev/config",
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

  it("tells vocabulary and integration apart on the row", async () => {
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    const google = await rowOf("google")
    expect(within(people).getByText("Vocabulary")).toBeTruthy()
    expect(within(people).queryByText("Integration")).toBeNull()
    expect(within(google).getByText("Integration")).toBeTruthy()
    expect(within(google).queryByText("Vocabulary")).toBeNull()
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
    expect(within(detail).getByText(/connects an external provider/i)).toBeTruthy()
    expect(
      within(detail).getByTitle(
        "people.substrate.reamde.dev is not imported — the import is refused until it is"
      )
    ).toBeTruthy()
  })

  it("shows a closure's declared kinds even before it is imported (no route yet)", async () => {
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("people"))
    const person = within(detail).getByText("person")
    // Not imported: the kind exists on paper only, so it does not pretend to
    // link anywhere.
    expect(person.tagName).toBe("SPAN")
    expect(person.getAttribute("title")).toBe("people.substrate.reamde.dev/person")
  })

  it("refuses the import while a required authority is missing, naming it", async () => {
    renderPage(<RegistryPage />)
    const google = await rowOf("google")
    const button = within(google).getByRole("button", { name: /Import/ })
    expect(button.hasAttribute("disabled")).toBe(true)
    expect(
      within(google).getByText(
        "Import people.substrate.reamde.dev, messaging.substrate.reamde.dev and calendar.substrate.reamde.dev first — this bundle declares against them."
      )
    ).toBeTruthy()
    // …and the row itself says what is missing, without opening anything.
    expect(within(google).getByText(/needs people.substrate.reamde.dev/)).toBeTruthy()
  })

  it("imports a closure that declares against nothing", async () => {
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    const button = within(people).getByRole("button", { name: /Import/ })
    expect(button.hasAttribute("disabled")).toBe(false)
    fireEvent.click(button)
    await waitFor(() =>
      expect(
        fetchMock.mock.calls.some(
          ([url, init]) =>
            String(url) ===
              `${CATALOG_PATH}/people.substrate.reamde.dev%2Fpeople/install` &&
            (init as RequestInit).method === "POST"
        )
      ).toBe(true)
    )
  })

  it("surfaces a server refusal in the server's own words", async () => {
    const problem =
      "bundle people.substrate.reamde.dev: data.requires names core.substrate.reamde.dev, which this repository does not have — import that authority's bundle first"
    serve({
      install: () =>
        jsonResponse(422, {
          error: { code: "validation", message: "validation error", problems: [problem] },
        }),
    })
    renderPage(<RegistryPage />)
    const people = await rowOf("people")
    fireEvent.click(within(people).getByRole("button", { name: /Import/ }))
    expect(await screen.findByText(problem)).toBeTruthy()
  })

  describe("once the vocabulary is imported", () => {
    beforeEach(() =>
      serve({
        statuses: [peopleStatus()],
        kinds: [CORE_KIND, PERSON_KIND],
        catalog: [
          { ...PEOPLE, installed: true },
          GOOGLE,
          bundle({
            id: "messaging.substrate.reamde.dev/messaging",
            name: "messaging",
            authority: "messaging.substrate.reamde.dev",
            vocabulary: true,
            installed: true,
          }),
          bundle({
            id: "calendar.substrate.reamde.dev/calendar",
            name: "calendar",
            authority: "calendar.substrate.reamde.dev",
            vocabulary: true,
            installed: true,
          }),
        ],
      })
    )

    it("links an imported closure's kind to its collection", async () => {
      renderPage(<RegistryPage />)
      const detail = expand(await rowOf("people"))
      const person = within(detail).getByText("person")
      expect(person.tagName).toBe("A")
      expect(person.getAttribute("data-to")).toBe("/data/$authority/$plural")
      expect(JSON.parse(person.getAttribute("data-params")!)).toEqual({
        authority: "people.substrate.reamde.dev",
        plural: "persons",
      })
    })

    it("marks a satisfied requirement and lets the import through", async () => {
      renderPage(<RegistryPage />)
      const google = await rowOf("google")
      expect(
        within(google).getByRole("button", { name: /Import/ }).hasAttribute(
          "disabled"
        )
      ).toBe(false)
      expect(within(google).queryByText(/needs /)).toBeNull()
      const detail = expand(google)
      expect(
        within(detail).getByTitle("people.substrate.reamde.dev is imported")
      ).toBeTruthy()
    })
  })
})
