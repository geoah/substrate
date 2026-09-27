// @vitest-environment jsdom
/** A bundle's settings form. The secret case is pinned hardest: its stored
 * value never reaches the browser, so the input starts blank whatever the
 * read served, says whether a value is saved, and a blank one is never sent;
 * and only what the person edited is written. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { BundleSettingsForm } from "@/components/bundle-settings"
import { Toaster } from "@/components/ui/toast"
import type { SubstrateRecord } from "@/lib/api/types"
import { groupSettings } from "@/lib/settings"

const SETTINGS_PATH = "/api/v1/substrate.reamde.dev/core/setting"
const SECRETS_PATH = "/api/v1/substrate.reamde.dev/core/secret"
const SETTING_KIND = "substrate.reamde.dev/core/setting"
const SECRET_KIND = "substrate.reamde.dev/core/secret"
const BUNDLE = "ada.example.com/firecrawl"

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

const BASE_URL = record(`${BUNDLE}/baseURL`, SETTING_KIND, {
  displayName: "Base URL",
  description: "Where the crawler talks to.",
  type: "url",
  value: "https://api.example.com",
})

const API_KEY = record(`${BUNDLE}/apiKey`, SECRET_KIND, {
  displayName: "API key",
  required: true,
  value: "",
})

function renderForm(records: SubstrateRecord[]) {
  const fields =
    groupSettings(records).find((g) => g.bundle === BUNDLE)?.fields ?? []
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Toaster>
        <BundleSettingsForm fields={fields} />
      </Toaster>
    </QueryClientProvider>
  )
}

describe("BundleSettingsForm", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    fetchMock.mockImplementation(
      async () => new Response(JSON.stringify({}), { status: 200 })
    )
    vi.stubGlobal("fetch", fetchMock)
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

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

  it("saves a changed value as a patch of that record alone", async () => {
    renderForm([BASE_URL, API_KEY])
    fireEvent.change(await screen.findByLabelText(/Base URL/), {
      target: { value: "https://crawl.example.com" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0]).toEqual([
      `${SETTINGS_PATH}/ada.example.com%2Ffirecrawl%2FbaseURL`,
      { properties: { value: "https://crawl.example.com" } },
    ])
  })

  it("shows a secret write-only: blank, said to be unsaved, and never sent empty", async () => {
    renderForm([BASE_URL, API_KEY])
    const secret = await screen.findByLabelText(/API key/)
    expect(secret.getAttribute("type")).toBe("password")
    expect((secret as HTMLInputElement).value).toBe("")
    expect(screen.getByText("Not saved yet")).toBeTruthy()
    expect(
      screen.getByRole("button", { name: "Save" }).hasAttribute("disabled")
    ).toBe(true)
  })

  // One bundle may own a `setting` and a `secret` both called apiKey; a
  // shared draft would PATCH the typed secret into the readable setting.
  it("never writes a secret into the plain setting of the same name", async () => {
    renderForm([
      record(`${BUNDLE}/apiKey`, SETTING_KIND, {
        displayName: "Key hint",
        type: "string",
        value: "hint",
      }),
      record(`${BUNDLE}/apiKey`, SECRET_KIND, {
        displayName: "API key",
        value: "",
      }),
    ])
    fireEvent.change(await screen.findByLabelText(/API key/), {
      target: { value: "sk-live-secret" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0]).toEqual([
      `${SECRETS_PATH}/ada.example.com%2Ffirecrawl%2FapiKey`,
      { properties: { value: "sk-live-secret" } },
    ])
    expect((screen.getByLabelText(/Key hint/) as HTMLInputElement).value).toBe(
      "hint"
    )
  })

  it("writes only the fields that were edited, leaving an untouched bool unset", async () => {
    renderForm([
      BASE_URL,
      record(`${BUNDLE}/verbose`, SETTING_KIND, {
        displayName: "Verbose",
        type: "bool",
        value: "",
      }),
    ])
    fireEvent.change(await screen.findByLabelText(/Base URL/), {
      target: { value: "https://crawl.example.com" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(patches()).toHaveLength(1))
    expect(patches()[0][0]).toBe(
      `${SETTINGS_PATH}/ada.example.com%2Ffirecrawl%2FbaseURL`
    )
  })

  it("holds a required setting's error until it is touched or a save is tried", async () => {
    renderForm([BASE_URL, API_KEY])
    const secret = await screen.findByLabelText(/API key/)
    expect(screen.queryByText(/Required\. This has no value yet/)).toBeNull()
    fireEvent.change(secret, { target: { value: "sk" } })
    fireEvent.change(secret, { target: { value: "" } })
    expect(screen.getByText(/Required\. This has no value yet/)).toBeTruthy()
  })

  it("names every unset required setting once a save is tried", async () => {
    renderForm([BASE_URL, API_KEY])
    fireEvent.change(await screen.findByLabelText(/Base URL/), {
      target: { value: "https://crawl.example.com" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(
      await screen.findByText(/Required\. This has no value yet/)
    ).toBeTruthy()
  })
})
