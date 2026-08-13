/** The bundle detail after the import, answering the same questions the
 * Registry row answered before it: what this bundle IS (Vocabulary /
 * Integration, at which shipped version), what it declared against (each
 * requirement checked against the live kind registry), and what it installed
 * (kinds linked to their collections).
 *
 * The vocabulary case is the one that used to lie: a bundle that ships kinds
 * and nothing else declares NO inputs and carries NO setup, so the page must
 * show no setup UI at all rather than an empty surface inviting a write the
 * loader would refuse to admit. */

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
import type {
  BundleStatus,
  CatalogBundle,
  KindInfo,
  SubstrateRecord,
} from "@/lib/api/types"

const params = { id: "people.substrate.reamde.dev/people" }

vi.mock("@tanstack/react-router", () => ({
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
  bundleDetailRoute: { useParams: () => params },
}))

import { BundleDetailPage } from "./bundle-detail"

const CATALOG_PATH = "/api/v1/core.substrate.reamde.dev/catalog"

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
    installed: true,
    ...over,
  }
}

const PEOPLE = bundle({
  id: "people.substrate.reamde.dev/people",
  name: "people",
  authority: "people.substrate.reamde.dev",
  description: "The shipped vocabulary for humans.",
  version: "v1alpha1",
  vocabulary: true,
  resources: { kinds: ["people.substrate.reamde.dev/person"] },
})

const GOOGLE = bundle({
  id: "google.bundles.substrate.reamde.dev/google",
  name: "google",
  authority: "google.bundles.substrate.reamde.dev",
  description: "Connects a Google account.",
  inputs: {
    client: {
      kind: "google.bundles.substrate.reamde.dev/config",
      description: "The OAuth client record.",
    },
  },
  integration: true,
  requires: ["people.substrate.reamde.dev", "messaging.substrate.reamde.dev"],
  resources: {
    kinds: [
      "google.bundles.substrate.reamde.dev/config",
      "google.bundles.substrate.reamde.dev/contact",
    ],
    functions: ["google.bundles.substrate.reamde.dev/syncgoogle"],
  },
})

function kind(over: Partial<KindInfo>): KindInfo {
  return {
    identity: "people.substrate.reamde.dev/person",
    name: "person",
    authority: "people.substrate.reamde.dev",
    version: "v1alpha1",
    plural: "persons",
    source: "builtin",
    ...over,
  }
}

const KINDS: KindInfo[] = [
  kind({}),
  kind({
    identity: "google.bundles.substrate.reamde.dev/config",
    name: "config",
    authority: "google.bundles.substrate.reamde.dev",
    plural: "configs",
    definition: { traits: ["oauth2"] },
  }),
  kind({
    identity: "google.bundles.substrate.reamde.dev/contact",
    name: "contact",
    authority: "google.bundles.substrate.reamde.dev",
    plural: "contacts",
  }),
]

function status(over: Partial<BundleStatus>): BundleStatus {
  return {
    id: PEOPLE.id,
    name: "people",
    authority: "people.substrate.reamde.dev",
    installed: true,
    enabled: true,
    kinds: 1,
    liveRecords: 0,
    ...over,
  }
}

function renderPage(ui: ReactElement) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Toaster>{ui}</Toaster>
    </QueryClientProvider>
  )
}

function configRecord(id: string): SubstrateRecord {
  return {
    id,
    kind: "google.bundles.substrate.reamde.dev/config",
    properties: { title: `client ${id}` },
    labels: {},
    version: 1,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  }
}

describe("BundleDetailPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  function serve(
    bundleStatus: BundleStatus,
    opts: { configs?: SubstrateRecord[] } = {}
  ) {
    fetchMock.mockImplementation(async (url, init) => {
      const method = (init as RequestInit | undefined)?.method ?? "GET"
      const path = String(url)
      if (path.includes("/bundles/") && path.endsWith("/status")) {
        return jsonResponse(200, bundleStatus)
      }
      if (path.includes("/bundles/") && path.endsWith("/bind") && method === "POST") {
        return jsonResponse(200, bundleStatus)
      }
      if (path.startsWith("/api/v1/core.substrate.reamde.dev/kinds")) {
        return jsonResponse(200, { kinds: KINDS })
      }
      if (path === CATALOG_PATH) {
        return jsonResponse(200, { catalog: [PEOPLE, GOOGLE] })
      }
      if (path.startsWith("/api/v1/google.bundles.substrate.reamde.dev/configs")) {
        return jsonResponse(200, { records: opts.configs ?? [] })
      }
      return jsonResponse(200, { records: [] })
    })
  }

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    params.id = PEOPLE.id
    serve(status({}))
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  describe("a vocabulary bundle", () => {
    it("declares no inputs and has no setup, so NO setup UI and NO warning chip", async () => {
      renderPage(<BundleDetailPage />)
      await screen.findByText("people")
      expect(screen.getByText("Vocabulary")).toBeTruthy()
      expect(screen.getByText("· v1alpha1")).toBeTruthy()
      expect(screen.getByText("The shipped vocabulary for humans.")).toBeTruthy()
      // No Setup section at all: no heading, no empty state, no chip.
      expect(screen.queryByText("Setup")).toBeNull()
      expect(screen.queryByText(/setup step/)).toBeNull()
      expect(screen.getByText("enabled")).toBeTruthy()
    })

    it("links each installed kind to its collection", async () => {
      renderPage(<BundleDetailPage />)
      const person = await screen.findByText("person")
      const link = person.closest("a")!
      expect(link.getAttribute("data-to")).toBe("/data/$authority/$plural")
      expect(JSON.parse(link.getAttribute("data-params")!)).toEqual({
        authority: "people.substrate.reamde.dev",
        plural: "persons",
      })
    })

    it("declares against nothing, so no requires note is shown", async () => {
      renderPage(<BundleDetailPage />)
      await screen.findByText("people")
      expect(screen.queryByText("Requires")).toBeNull()
    })
  })

  describe("an integration with requirements and inputs", () => {
    function googleStatus(over: Partial<BundleStatus> = {}): BundleStatus {
      return status({
        id: GOOGLE.id,
        name: "google",
        authority: "google.bundles.substrate.reamde.dev",
        inputs: [
          {
            name: "client",
            kind: "google.bundles.substrate.reamde.dev/config",
            description: "The OAuth client record.",
          },
        ],
        setup: [
          {
            code: "missing",
            input: "client",
            kind: "google.bundles.substrate.reamde.dev/config",
            message: "no config record exists yet",
          },
        ],
        kinds: 2,
        functions: 1,
        ...over,
      })
    }

    beforeEach(() => {
      params.id = GOOGLE.id
      serve(googleStatus())
    })

    it("checks each requirement against the live registry", async () => {
      renderPage(<BundleDetailPage />)
      await screen.findByText("google")
      const note = screen.getByText("Requires").closest("div") as HTMLElement
      // people is reconciled in the kind registry; messaging is not.
      expect(
        within(note).getByTitle("people.substrate.reamde.dev is imported")
      ).toBeTruthy()
      expect(
        within(note).getByTitle("messaging.substrate.reamde.dev is not imported")
      ).toBeTruthy()
      expect(
        screen.getByText(/Not in this repository: messaging.substrate.reamde.dev/)
      ).toBeTruthy()
    })

    it("wears the setup chip beside the lifecycle badge and renders the input", async () => {
      renderPage(<BundleDetailPage />)
      await screen.findByText("google")
      expect(screen.getByText("Integration")).toBeTruthy()
      // Separate signals: the lifecycle badge stays `enabled`, the chip counts.
      expect(screen.getByText("enabled")).toBeTruthy()
      expect(screen.getByText("1 setup step")).toBeTruthy()
      // The Setup section carries the input by name and kind, with its
      // unresolved problem in the server's words.
      await waitFor(() => expect(screen.getByText("Setup")).toBeTruthy())
      const section = screen.getByText("Setup").closest("section") as HTMLElement
      expect(within(section).getByText("client")).toBeTruthy()
      expect(
        within(section).getByText("google.bundles.substrate.reamde.dev/config")
      ).toBeTruthy()
      expect(
        within(section).getByText("no config record exists yet")
      ).toBeTruthy()
    })

    it("names the resolution on a resolved input", async () => {
      serve(
        googleStatus({
          inputs: [
            {
              name: "client",
              kind: "google.bundles.substrate.reamde.dev/config",
              record: "default",
              via: "default",
            },
          ],
          setup: undefined,
        }),
        { configs: [configRecord("default")] }
      )
      renderPage(<BundleDetailPage />)
      await screen.findByText("google")
      expect(screen.queryByText(/setup step/)).toBeNull()
      await waitFor(() =>
        expect(screen.getByText("in use via default")).toBeTruthy()
      )
    })

    it("binds a picked record to the input through the bind verb", async () => {
      serve(googleStatus(), {
        configs: [configRecord("cfg-1"), configRecord("cfg-2")],
      })
      renderPage(<BundleDetailPage />)
      await screen.findByText("google")
      const useButtons = await screen.findAllByRole("button", {
        name: "Use this",
      })
      expect(useButtons).toHaveLength(2)
      fireEvent.click(useButtons[0])
      await waitFor(() =>
        expect(
          fetchMock.mock.calls.some(([url, init]) => {
            const req = init as RequestInit | undefined
            return (
              String(url) ===
                "/api/v1/core.substrate.reamde.dev/bundles/google.bundles.substrate.reamde.dev%2Fgoogle/bind" &&
              req?.method === "POST" &&
              JSON.parse(String(req.body)) !== null &&
              JSON.parse(String(req.body)).input === "client" &&
              JSON.parse(String(req.body)).record === "cfg-1"
            )
          })
        ).toBe(true)
      )
    })

    it("offers Unbind on the bound record", async () => {
      serve(
        googleStatus({
          inputs: [
            {
              name: "client",
              kind: "google.bundles.substrate.reamde.dev/config",
              record: "cfg-1",
              via: "bound",
            },
          ],
          setup: undefined,
        }),
        { configs: [configRecord("cfg-1"), configRecord("cfg-2")] }
      )
      renderPage(<BundleDetailPage />)
      await screen.findByText("google")
      const unbind = await screen.findByRole("button", { name: "Unbind" })
      fireEvent.click(unbind)
      await waitFor(() =>
        expect(
          fetchMock.mock.calls.some(([url, init]) => {
            const req = init as RequestInit | undefined
            return (
              String(url).endsWith("/bind") &&
              req?.method === "POST" &&
              JSON.parse(String(req.body)).record === ""
            )
          })
        ).toBe(true)
      )
    })
  })

  describe("standalone setup items", () => {
    it("renders a provider step as a warning row linking the llmprovider record", async () => {
      params.id = PEOPLE.id
      serve(
        status({
          setup: [
            {
              code: "provider",
              kind: "core.substrate.reamde.dev/llmprovider",
              record: "openai",
              message: "llmprovider openai has no key",
            },
          ],
        })
      )
      renderPage(<BundleDetailPage />)
      await screen.findByText("people")
      expect(screen.getByText("1 setup step")).toBeTruthy()
      const row = screen
        .getByText("llmprovider openai has no key")
        .closest("div") as HTMLElement
      const link = within(row).getByText("openai").closest("a")!
      expect(link.getAttribute("data-to")).toBe("/data/$authority/$plural/$id")
      expect(JSON.parse(link.getAttribute("data-params")!)).toEqual({
        authority: "core.substrate.reamde.dev",
        plural: "llmproviders",
        id: "openai",
      })
    })
  })
})
