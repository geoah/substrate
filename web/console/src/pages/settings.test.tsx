// @vitest-environment jsdom
/** The two settings surfaces: `/settings` is the LIST — one row per bundle
 * that ships settings, with what it has and what it still needs — and
 * `/settings/$id` is that one bundle's form.
 *
 * The form is the half worth pinning hardest, and the secret case hardest of
 * all: its stored value never reaches the browser, so the input starts blank
 * whatever the read served, says whether a value is set, and a blank one is
 * never sent. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Toaster } from "@/components/ui/toast"
import type { SubstrateRecord } from "@/lib/api/types"

const navigate = vi.fn()
/** What `useParams` answers on the detail page; each detail test sets it. */
let routeParams: Record<string, string> = {}

vi.mock("@tanstack/react-router", () => ({
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
  useNavigate: () => navigate,
}))

vi.mock("@/router", () => ({
  bundleSettingsRoute: { useParams: () => routeParams },
}))

import { BundleSettingsPage, SettingsPage } from "./settings"

const SETTINGS_PATH = "/api/v1/substrate.reamde.dev/core/setting"
const SECRETS_PATH = "/api/v1/substrate.reamde.dev/core/secret"
const STATUSES_PATH = "/api/v1/substrate.reamde.dev/core/bundle/status"
const SETTING_KIND = "substrate.reamde.dev/core/setting"
const SECRET_KIND = "substrate.reamde.dev/core/secret"

function record(
  id: string,
  kind: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  }
}

const BASE_URL = record("ada.example.com/firecrawl/baseURL", SETTING_KIND, {
  displayName: "Base URL",
  description: "Where the crawler talks to.",
  type: "url",
  value: "https://api.example.com",
})

const API_KEY = record("ada.example.com/firecrawl/apiKey", SECRET_KIND, {
  displayName: "API key",
  required: true,
  value: "",
})

const TIMEOUT = record("ada.example.com/web/timeout", SETTING_KIND, {
  displayName: "Timeout",
  type: "int",
  value: "30",
})

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200 })
}

function renderPage(page: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Toaster>{page}</Toaster>
    </QueryClientProvider>
  )
}

/** The bundle's own form, as the route under `/settings` renders it. */
function renderBundle(id: string) {
  routeParams = { id }
  return renderPage(<BundleSettingsPage />)
}

/** The kinds a records-route URL lists, read off its `filter`: the list route
 * is one path for every kind, so a stub dispatches on this, not the path. */
function listedKinds(path: string): string[] {
  const url = new URL(path, "http://x")
  if (url.pathname !== "/api/v1/records") return []
  const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
    kinds?: string[]
  }
  return filter.kinds ?? []
}

describe("the settings surfaces", () => {
  const fetchMock = vi.fn<typeof fetch>()
  let settings: SubstrateRecord[] = []
  let secrets: SubstrateRecord[] = []

  beforeEach(() => {
    settings = [BASE_URL, TIMEOUT]
    secrets = [API_KEY]
    routeParams = {}
    fetchMock.mockImplementation(async (url) => {
      const path = String(url)
      // Both kinds arrive in ONE list read; the record's kind tells them apart.
      if (listedKinds(path).includes(SETTING_KIND)) {
        return jsonResponse({ records: [...settings, ...secrets] })
      }
      if (path === STATUSES_PATH) {
        return jsonResponse({
          items: [
            { id: "ada.example.com/firecrawl", name: "firecrawl" },
            { id: "ada.example.com/web", name: "web" },
          ],
        })
      }
      return jsonResponse({})
    })
    vi.stubGlobal("fetch", fetchMock)
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    navigate.mockReset()
    localStorage.clear()
  })

  /** Every write the page made, as [url, decoded body], in order. */
  function patches(): [string, unknown][] {
    return fetchMock.mock.calls
      .filter(
        ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
      )
      .map(([url, init]) => [
        String(url),
        JSON.parse(String((init as RequestInit).body)),
      ])
  }

  // ── the list ──────────────────────────────────────────────────────────────

  describe("SettingsPage", () => {
    it("lists one row per bundle, with what it holds and what it still needs", async () => {
      renderPage(<SettingsPage />)
      await screen.findByText("firecrawl")
      expect(screen.getByText("web")).toBeTruthy()
      // The row is the bundle, not the setting: no control is on this page.
      expect(screen.queryByLabelText(/Base URL/)).toBeNull()
      // firecrawl owns two (a setting and a secret), web owns one.
      const firecrawlRow = screen.getByText("firecrawl").closest("tr")!
      expect(firecrawlRow.textContent).toContain("ada.example.com/firecrawl")
      expect(firecrawlRow.textContent).toContain("2")
      // Its secret is required and empty, so the row says one step stands;
      // web's lone setting has a value, so its row says nothing.
      expect(firecrawlRow.textContent).toContain("1 setup step")
      expect(screen.getByText("web").closest("tr")!.textContent).not.toContain(
        "setup step"
      )
    })

    it("opens the bundle's own settings page when a row is clicked", async () => {
      renderPage(<SettingsPage />)
      fireEvent.click(await screen.findByText("firecrawl"))
      expect(navigate).toHaveBeenCalledWith({
        to: "/settings/$id",
        params: { id: "ada.example.com/firecrawl" },
      })
    })

    it("says where settings come from when the repository has none", async () => {
      settings = []
      secrets = []
      renderPage(<SettingsPage />)
      expect(await screen.findByText("No settings yet")).toBeTruthy()
      expect(
        screen.getByText(/A bundle that needs settings ships them/)
      ).toBeTruthy()
    })
  })

  // ── one bundle's form ─────────────────────────────────────────────────────

  describe("BundleSettingsPage", () => {
    it("renders that bundle's fields alone, headed by a link to its page", async () => {
      renderBundle("ada.example.com/firecrawl")
      expect(await screen.findByLabelText(/Base URL/)).toBeTruthy()
      const link = screen.getByText("ada.example.com/firecrawl")
      expect(link.closest("a")?.getAttribute("data-to")).toBe("/registry/$id")
      expect(
        JSON.parse(link.closest("a")!.getAttribute("data-params")!)
      ).toEqual({ id: "ada.example.com/firecrawl" })
      expect(screen.getByLabelText(/API key/)).toBeTruthy()
      // The other bundle's setting is not on this page.
      expect(screen.queryByLabelText(/Timeout/)).toBeNull()
    })

    it("saves a changed value as a patch of that record alone", async () => {
      renderBundle("ada.example.com/firecrawl")
      const input = await screen.findByLabelText(/Base URL/)
      fireEvent.change(input, {
        target: { value: "https://crawl.example.com" },
      })
      fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0])

      await waitFor(() => expect(patches()).toHaveLength(1))
      expect(patches()[0][0]).toBe(
        `${SETTINGS_PATH}/ada.example.com%2Ffirecrawl%2FbaseURL`
      )
      expect(patches()[0][1]).toEqual({
        properties: { value: "https://crawl.example.com" },
      })
    })

    it("shows a secret write-only: blank, said to be unset, and never sent empty", async () => {
      renderBundle("ada.example.com/firecrawl")
      const secret = await screen.findByLabelText(/API key/)
      expect(secret.getAttribute("type")).toBe("password")
      expect((secret as HTMLInputElement).value).toBe("")
      expect(screen.getByText("not set")).toBeTruthy()
      // Nothing typed, nothing to save.
      expect(
        screen
          .getAllByRole("button", { name: "Save" })[0]
          .hasAttribute("disabled")
      ).toBe(true)
    })

    // A record is unique by kind AND id, so one bundle may own a `setting` and
    // a `secret` both called apiKey. A draft shared between them would PATCH
    // the typed secret into the plain setting, where anybody can read it back.
    it("never writes a secret into the plain setting of the same name", async () => {
      settings = [
        record("ada.example.com/firecrawl/apiKey", SETTING_KIND, {
          displayName: "Key hint",
          type: "string",
          value: "hint",
        }),
      ]
      secrets = [
        record("ada.example.com/firecrawl/apiKey", SECRET_KIND, {
          displayName: "API key",
          value: "",
        }),
      ]
      renderBundle("ada.example.com/firecrawl")
      fireEvent.change(await screen.findByLabelText(/API key/), {
        target: { value: "sk-live-secret" },
      })
      fireEvent.click(screen.getByRole("button", { name: "Save" }))

      await waitFor(() => expect(patches()).toHaveLength(1))
      const [url, body] = patches()[0]
      expect(url).toBe(`${SECRETS_PATH}/ada.example.com%2Ffirecrawl%2FapiKey`)
      expect(body).toEqual({ properties: { value: "sk-live-secret" } })
      // The plain setting is untouched, and still says what it always said.
      expect(
        (screen.getByLabelText(/Key hint/) as HTMLInputElement).value
      ).toBe("hint")
    })

    // Every control holds a value whether or not anybody typed into it: an
    // empty bool renders as an unchecked box. Only what was edited is written,
    // or saving one field would fill in every untouched checkbox beside it.
    it("writes only the fields that were edited, leaving an untouched bool unset", async () => {
      settings = [
        BASE_URL,
        record("ada.example.com/firecrawl/verbose", SETTING_KIND, {
          displayName: "Verbose",
          type: "bool",
          value: "",
        }),
      ]
      secrets = []
      renderBundle("ada.example.com/firecrawl")
      fireEvent.change(await screen.findByLabelText(/Base URL/), {
        target: { value: "https://crawl.example.com" },
      })
      fireEvent.click(screen.getByRole("button", { name: "Save" }))

      await waitFor(() => expect(patches()).toHaveLength(1))
      expect(patches()[0][0]).toBe(
        `${SETTINGS_PATH}/ada.example.com%2Ffirecrawl%2FbaseURL`
      )
    })

    it("says so when the bundle in the path ships none", async () => {
      renderBundle("ada.example.com/nothing")
      expect(await screen.findByText("No settings here")).toBeTruthy()
    })
  })
})
