// @vitest-environment jsdom
/** The Settings page: two bundles' settings read out of the two core
 * collections, grouped by the id prefix that owns them, saved as a PATCH of
 * the one property the form writes.
 *
 * The secret case is the one worth pinning: its stored value never reaches
 * the browser, so the input starts blank whatever the read served, says
 * whether a value is set, and a blank one is never sent. */

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
}))

import { SettingsPage } from "./settings"

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

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Toaster>
        <SettingsPage />
      </Toaster>
    </QueryClientProvider>
  )
}

describe("SettingsPage", () => {
  const fetchMock = vi.fn<typeof fetch>()
  let settings: SubstrateRecord[] = []
  let secrets: SubstrateRecord[] = []

  beforeEach(() => {
    settings = [BASE_URL, TIMEOUT]
    secrets = [API_KEY]
    fetchMock.mockImplementation(async (url) => {
      const path = String(url)
      if (path.startsWith(SETTINGS_PATH)) {
        return jsonResponse({ records: settings })
      }
      if (path.startsWith(SECRETS_PATH))
        return jsonResponse({ records: secrets })
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

  it("groups every setting under the bundle its id names, linked to its page", async () => {
    renderPage()
    const firecrawl = await screen.findByText("firecrawl")
    expect(firecrawl.closest("a")?.getAttribute("data-to")).toBe(
      "/registry/$id"
    )
    expect(
      JSON.parse(firecrawl.closest("a")!.getAttribute("data-params")!)
    ).toEqual({ id: "ada.example.com/firecrawl" })
    expect(screen.getByText("web")).toBeTruthy()
    // Each bundle's own fields, and no other bundle's.
    expect(screen.getByLabelText(/Base URL/)).toBeTruthy()
    expect(screen.getByLabelText(/Timeout/)).toBeTruthy()
  })

  it("saves a changed value as a patch of that record alone", async () => {
    renderPage()
    const input = await screen.findByLabelText(/Base URL/)
    fireEvent.change(input, { target: { value: "https://crawl.example.com" } })
    fireEvent.click(screen.getAllByRole("button", { name: "Save" })[0])

    await waitFor(() => {
      const patch = fetchMock.mock.calls.find(
        ([, init]) => (init as RequestInit | undefined)?.method === "PATCH"
      )
      expect(patch).toBeTruthy()
      expect(String(patch![0])).toBe(
        `${SETTINGS_PATH}/ada.example.com%2Ffirecrawl%2FbaseURL`
      )
      expect(JSON.parse(String((patch![1] as RequestInit).body))).toEqual({
        properties: { value: "https://crawl.example.com" },
      })
    })
  })

  it("shows a secret write-only: blank, said to be unset, and never sent empty", async () => {
    renderPage()
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

  // A record is unique by kind AND id, so one bundle may own a `setting` and a
  // `secret` both called apiKey. A draft shared between them would PATCH the
  // typed secret into the plain setting, where anybody can read it back.
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
    renderPage()
    fireEvent.change(await screen.findByLabelText(/API key/), {
      target: { value: "sk-live-secret" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() => expect(patches()).toHaveLength(1))
    const [url, body] = patches()[0]
    expect(url).toBe(`${SECRETS_PATH}/ada.example.com%2Ffirecrawl%2FapiKey`)
    expect(body).toEqual({ properties: { value: "sk-live-secret" } })
    // The plain setting is untouched, and still says what it always said.
    expect((screen.getByLabelText(/Key hint/) as HTMLInputElement).value).toBe(
      "hint"
    )
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
    renderPage()
    fireEvent.change(await screen.findByLabelText(/Base URL/), {
      target: { value: "https://crawl.example.com" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))

    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0][0]).toBe(
      `${SETTINGS_PATH}/ada.example.com%2Ffirecrawl%2FbaseURL`
    )
  })

  it("says where settings come from when the repository has none", async () => {
    settings = []
    secrets = []
    renderPage()
    expect(await screen.findByText("No settings yet")).toBeTruthy()
    expect(
      screen.getByText(/A bundle that needs settings ships them/)
    ).toBeTruthy()
  })
})
