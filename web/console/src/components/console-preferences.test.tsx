// @vitest-environment jsdom
/** The provider's promise: an action made while the record is still loading,
 * or while another save is in flight, is queued and lands, never dropped. */

import { useEffect } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useConsolePreferences } from "@/hooks/use-console-preferences"
import { ConsolePreferencesProvider } from "./console-preferences"

let stored: Record<string, unknown> = {}
let version = 0
const puts: Array<Record<string, unknown>> = []

beforeEach(() => {
  // `SidebarProvider` asks whether the viewport is a phone; jsdom implements
  // no media queries.
  window.matchMedia = (media: string) =>
    ({
      media,
      matches: false,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList
  stored = {}
  version = 0
  puts.length = 0
  localStorage.clear()
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).includes("/api/v1/records")) {
        // the kind registry: this repository's preference kind is version 1
        return new Response(JSON.stringify({ records: [] }), { status: 200 })
      }
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body))
        puts.push(body.properties)
        stored = { ...stored, ...body.properties }
        version++
      }
      return new Response(
        JSON.stringify({
          id: "navigation",
          kind: "substrate.reamde.dev/core/consolepreference",
          version,
          properties: stored,
        }),
        { status: 200 }
      )
    })
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const probe: { handle?: ReturnType<typeof useConsolePreferences> } = {}

function Probe() {
  const handle = useConsolePreferences()
  useEffect(() => {
    probe.handle = handle
  })
  return (
    <p data-testid="state">
      {handle.preferences.favorites.join(",")}|
      {String(handle.preferences.technicalDetails)}
    </p>
  )
}

function renderProvider() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ConsolePreferencesProvider>
        <Probe />
      </ConsolePreferencesProvider>
    </QueryClientProvider>
  )
}

describe("ConsolePreferencesProvider", () => {
  it("queues actions made while busy and saves every one of them", async () => {
    renderProvider()
    act(() => {
      probe.handle!.change({
        type: "favorite",
        key: "example.com/a/a",
        starred: true,
      })
      probe.handle!.change({
        type: "favorite",
        key: "example.com/b/b",
        starred: true,
      })
    })
    // shown at once, before any save answers
    expect(screen.getByTestId("state").textContent).toContain(
      "example.com/a/a,example.com/b/b"
    )
    await waitFor(() => expect(puts).toHaveLength(2))
    expect(stored.favorites).toEqual(["example.com/a/a", "example.com/b/b"])
  })

  it("keeps a setting the stored kind does not declare in this browser", async () => {
    renderProvider()
    act(() => probe.handle!.set("technicalDetails", true))
    await waitFor(() =>
      expect(localStorage.getItem("substrate.console.technicalDetails")).toBe(
        "true"
      )
    )
    expect(screen.getByTestId("state").textContent).toContain("true")
    expect(puts).toHaveLength(0)
  })

  it("closes the sidebar in this window only: nothing reaches the record", async () => {
    renderProvider()
    act(() => probe.handle!.change({ type: "sidebar", open: false }))
    expect(probe.handle!.preferences.sidebarOpen).toBe(false)
    expect(localStorage.getItem("substrate.console.sidebarOpen")).toBe("false")
    act(() =>
      probe.handle!.change({
        type: "favorite",
        key: "example.com/a/a",
        starred: true,
      })
    )
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0]).not.toHaveProperty("sidebarOpen")
  })
})
