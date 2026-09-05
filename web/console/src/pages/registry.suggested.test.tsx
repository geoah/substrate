/** The Registry's SUGGESTED MAPPINGS (decision record 0049): what a sample
 * would project, what is projecting already, and the one action that lands the
 * rest.
 *
 * The reader's whole problem is that installing a provider does nothing on its
 * own: a mapping onto a kind is the sample's declaration, so the closure has
 * to be imported AGAIN before a GitHub user reaches a person. A held sample is
 * never offered an upgrade, so "Import again" is the only door, and it states
 * what a re-import costs before it runs one. */

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
  SuggestedMapping,
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
const STATUS_PATH = "/api/v1/substrate.reamde.dev/core/bundle/status"
const REPOSITORY_PATH = "/api/v1/substrate.reamde.dev/core/repository"
const HOME = "ada.example.com"
const GITHUB = "providers.substrate.reamde.dev/github"
const GOOGLE = "providers.substrate.reamde.dev/google"

function jsonResponse(status: number, body: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
  })
}

/** One suggested mapping as the server serves it: ids and target REHOMED,
 * because the door reports the declarations this repository would hold. */
function mapping(over: Partial<SuggestedMapping>): SuggestedMapping {
  return {
    id: `${HOME}/people/githubuserperson`,
    from: `${GITHUB}/user`,
    to: `${HOME}/people/person`,
    package: GITHUB,
    state: "waiting",
    ...over,
  }
}

function people(suggested: SuggestedMapping[], installed = false): CatalogItem {
  return {
    id: "samples.substrate.reamde.dev/people",
    name: "people",
    authority: "samples.substrate.reamde.dev",
    package: "people",
    description: "The shipped vocabulary for humans.",
    version: 4,
    tier: "sample",
    closure: { kinds: ["samples.substrate.reamde.dev/people/person"] },
    suggestedMappings: suggested,
    installed,
  }
}

const githubEntry: CatalogItem = {
  id: GITHUB,
  name: "github",
  authority: "providers.substrate.reamde.dev",
  package: "github",
  description: "Mirrors the code work you are involved in.",
  version: 12,
  tier: "provider",
  closure: { kinds: [`${GITHUB}/user`] },
  installed: true,
}

const CORE_KIND: KindInfo = {
  identity: "substrate.reamde.dev/core/bundle",
  name: "bundle",
  authority: "substrate.reamde.dev",
  package: "core",
  version: 1,
  plural: "bundles",
  source: "builtin",
}

function peopleStatus(): BundleStatus {
  return {
    id: `${HOME}/people`,
    name: "people",
    authority: HOME,
    package: "people",
    installed: true,
    enabled: true,
    kinds: 1,
    liveRecords: 0,
  }
}

/** The provider's status once installed: a held provider is what makes the
 * "Import again" gate reachable on a row that must never offer it. */
function githubStatus(): BundleStatus {
  return {
    id: GITHUB,
    name: "github",
    authority: "providers.substrate.reamde.dev",
    package: "github",
    installed: true,
    enabled: true,
    kinds: 1,
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
  catalog?: CatalogItem[]
}

describe("Registry suggested mappings", () => {
  const fetchMock = vi.fn<typeof fetch>()
  const imported: string[] = []

  function serve(wire: Wire) {
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
              properties: { name: "ada", authority: HOME },
            },
          ],
        })
      }
      if (path.startsWith("/api/v1/substrate.reamde.dev/core/kind")) {
        return jsonResponse(200, { kinds: [CORE_KIND] })
      }
      if (path === CATALOG_PATH) {
        return jsonResponse(200, { items: wire.catalog ?? [] })
      }
      if (path.endsWith("/import") && method === "POST") {
        imported.push(
          decodeURIComponent(
            path.slice(CATALOG_PATH.length + 1, -"/import".length)
          )
        )
        return jsonResponse(200, peopleStatus())
      }
      return jsonResponse(200, {})
    })
  }

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    navigate.mockClear()
    localStorage.clear()
    imported.length = 0
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

  it("lists a sample's mappings with the state each one has here", async () => {
    serve({
      catalog: [
        people([
          mapping({ state: "landed" }),
          mapping({
            id: `${HOME}/people/googlecontactperson`,
            from: `${GOOGLE}/contact`,
            package: GOOGLE,
            state: "waiting",
          }),
        ]),
      ],
    })
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("people"))
    const landed = within(detail).getByTitle((title) =>
      title.includes(`github/user projects onto ${HOME}/people/person: landed.`)
    )
    expect(landed.textContent).toContain("user → person")
    expect(landed.textContent).toContain("landed")
    const waiting = within(detail).getByTitle(
      /google\/contact projects onto .*: install google, then import people again\./
    )
    expect(waiting.textContent).toContain("contact → person")
    expect(waiting.textContent).toContain("waiting")
    // The way out is stated once, in the row's own copy. This sample is not
    // imported yet, so it is an import rather than a re-import and the
    // replacement warning does not belong on it.
    expect(
      within(detail).getByText(
        "To enable this mapping, install google, then import people."
      )
    ).toBeTruthy()
  })

  it("says a ready mapping needs the import again, and what that replaces", async () => {
    serve({
      statuses: [peopleStatus()],
      catalog: [people([mapping({ state: "ready" })], true), githubEntry],
    })
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("people"))
    const chip = within(detail).getByTitle(/import people again\./)
    expect(chip.textContent).toContain("ready")
    expect(
      within(detail).getByText(
        "To enable this mapping, import people again. Re-importing replaces that package and may remove your changes."
      )
    ).toBeTruthy()
  })

  it("says a blocked mapping needs the provider upgraded, in the loader's words", async () => {
    serve({
      statuses: [peopleStatus()],
      catalog: [
        people(
          [
            mapping({
              state: "blocked",
              problems: [`${GITHUB}/user declares no property "person"`],
            }),
          ],
          true
        ),
        githubEntry,
      ],
    })
    renderPage(<RegistryPage />)
    const detail = expand(await rowOf("people"))
    const chip = within(detail).getByTitle(/upgrade github first/)
    expect(chip.textContent).toContain("blocked")
    expect(chip.getAttribute("title")).toContain(
      'declares no property "person"'
    )
    expect(
      within(detail).getByText(
        "To enable this mapping, upgrade github, then import people again. Re-importing replaces that package and may remove your changes."
      )
    ).toBeTruthy()
  })

  // The action item 2 of the review asked for: an installed sample gets no
  // Upgrade (a sample is never offered one), so without this the reader who
  // installs the provider has nothing to press.
  it("offers Import again on a held sample whose mapping is ready, and confirms first", async () => {
    serve({
      statuses: [peopleStatus()],
      catalog: [people([mapping({ state: "ready" })], true), githubEntry],
    })
    renderPage(<RegistryPage />)
    const row = await rowOf("people")
    const button = within(row).getByRole("button", { name: /Import again/ })
    fireEvent.click(button)
    // The confirmation states the 0048 cost before anything runs.
    const dialog = await screen.findByRole("dialog")
    expect(within(dialog).getByText(/Import people again\?/)).toBeTruthy()
    expect(dialog.textContent).toContain(`REPLACES ${HOME}/people`)
    expect(dialog.textContent).toContain("a kind or a property you added")
    expect(imported).toEqual([])
    fireEvent.click(
      within(dialog).getByRole("button", { name: /^Import again$/ })
    )
    await waitFor(() =>
      expect(imported).toEqual(["samples.substrate.reamde.dev/people"])
    )
  })

  it("offers no Import again while every mapping has landed", async () => {
    serve({
      statuses: [peopleStatus()],
      catalog: [people([mapping({ state: "landed" })], true), githubEntry],
    })
    renderPage(<RegistryPage />)
    const row = await rowOf("people")
    expect(
      within(row).queryByRole("button", { name: /Import again/ })
    ).toBeNull()
  })

  it("offers no Import again while the mapping only waits for a provider", async () => {
    serve({
      statuses: [peopleStatus()],
      catalog: [people([mapping({ state: "waiting" })], true)],
    })
    renderPage(<RegistryPage />)
    const row = await rowOf("people")
    expect(
      within(row).queryByRole("button", { name: /Import again/ })
    ).toBeNull()
  })

  it("lists on a provider the samples that carry a mapping onto it", async () => {
    serve({
      statuses: [peopleStatus(), githubStatus()],
      catalog: [people([mapping({ state: "ready" })], true), githubEntry],
    })
    renderPage(<RegistryPage />)
    const row = await rowOf("github")
    const detail = expand(row)
    const chip = within(detail).getByTitle(/import people again\./)
    expect(chip.textContent).toContain("people: user → person")
    expect(chip.textContent).toContain("ready")
    // A PROVIDER IS NEVER RE-IMPORTED. The mappings on this row are the
    // inbound ones, the people sample's, so offering "Import again" here
    // would POST a provider id at the import door, which the server refuses.
    expect(
      within(row).queryByRole("button", { name: /Import again/ })
    ).toBeNull()
  })
})
