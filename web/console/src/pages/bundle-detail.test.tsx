/** The bundle detail after the import, answering the same questions the
 * Registry row answered before it: what this bundle IS (Vocabulary /
 * Integration, at which shipped version), what it declared against (each
 * requirement checked against the live kind registry), and what it installed
 * (kinds linked to their collections).
 *
 * The vocabulary case is the one that used to lie: a bundle that ships kinds
 * and nothing else declares NO config type, and the page must say so rather
 * than offer a singleton configuration the loader would refuse to admit. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactElement } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import type { BundleStatus, CatalogBundle, KindInfo } from "@/lib/api/types"

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
  configType: "google.bundles.substrate.reamde.dev/config",
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
    definition: { traits: ["bundleconfig"] },
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
    configured: false,
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

describe("BundleDetailPage", () => {
  const fetchMock = vi.fn<typeof fetch>()

  function serve(bundleStatus: BundleStatus) {
    fetchMock.mockImplementation(async (url) => {
      const path = String(url)
      if (path.includes("/bundles/") && path.endsWith("/status")) {
        return jsonResponse(200, bundleStatus)
      }
      if (path.startsWith("/api/v1/core.substrate.reamde.dev/kinds")) {
        return jsonResponse(200, { kinds: KINDS })
      }
      if (path === CATALOG_PATH) {
        return jsonResponse(200, { catalog: [PEOPLE, GOOGLE] })
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
    it("says it is vocabulary, at the shipped version, and configures nothing", async () => {
      renderPage(<BundleDetailPage />)
      await screen.findByText("people")
      expect(screen.getByText("Vocabulary")).toBeTruthy()
      expect(screen.getByText("· v1alpha1")).toBeTruthy()
      expect(screen.getByText("The shipped vocabulary for humans.")).toBeTruthy()
      expect(
        screen.getByText(/record kinds and nothing else/)
      ).toBeTruthy()
      // The singleton config surface never appears — there is no config type.
      expect(screen.queryByText("Needs configuration")).toBeNull()
      expect(screen.queryByRole("button", { name: "Configure" })).toBeNull()
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

  describe("an integration with requirements", () => {
    beforeEach(() => {
      params.id = GOOGLE.id
      serve(
        status({
          id: GOOGLE.id,
          name: "google",
          authority: "google.bundles.substrate.reamde.dev",
          configType: "google.bundles.substrate.reamde.dev/config",
          configured: false,
          kinds: 2,
          functions: 1,
        })
      )
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

    it("keeps the configuration surface for a bundle that declares a config type", async () => {
      renderPage(<BundleDetailPage />)
      await screen.findByText("google")
      await waitFor(() =>
        expect(screen.getByText("Needs configuration")).toBeTruthy()
      )
      expect(screen.getByText("Integration")).toBeTruthy()
    })
  })
})
