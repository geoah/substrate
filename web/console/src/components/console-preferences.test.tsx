// @vitest-environment jsdom
/** The provider's promise: an action made while the record is still loading,
 * or while another save is in flight, is queued and lands, never dropped. */

import { useEffect } from "react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { ThemeProvider } from "@/components/theme-provider"
import { useConsolePreferences } from "@/hooks/use-console-preferences"
import { ConsolePreferencesProvider } from "./console-preferences"

let stored: Record<string, unknown> = {}
let version = 0
/** The kinds the registry answers with: none, unless a case declares the
 * preference kind. */
let registry: unknown[] = []
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
  registry = []
  puts.length = 0
  localStorage.clear()
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).includes("/api/v1/records")) {
        // the kind registry: this repository's preference kind is version 1
        return new Response(JSON.stringify({ records: [], kinds: registry }), {
          status: 200,
        })
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
      <ThemeProvider>
        <ConsolePreferencesProvider>
          <Probe />
        </ConsolePreferencesProvider>
      </ThemeProvider>
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

describe("a theme chosen while signed out", () => {
  const html = document.documentElement

  beforeEach(() => {
    // The repository's record holds Light, and its kind declares the theme.
    stored = { theme: "light" }
    version = 1
    registry = [
      {
        identity: "substrate.reamde.dev/core/consolepreference",
        name: "consolepreference",
        authority: "substrate.reamde.dev",
        package: "core",
        version: 2,
        source: "builtin",
        description: "",
        definition: { properties: { theme: {} } },
      },
    ]
    // This browser chose Dark.
    localStorage.setItem("theme", "dark")
  })

  it("is saved to the record instead of replaced by it", async () => {
    localStorage.setItem("substrate.console.signedOutTheme", "dark")
    renderProvider()
    await waitFor(() => expect(puts).toHaveLength(1))
    expect(puts[0].theme).toBe("dark")
    expect(probe.handle!.preferences.theme).toBe("dark")
    expect(html.className).toBe("dark")
    expect(localStorage.getItem("theme")).toBe("dark")
    expect(localStorage.getItem("substrate.console.signedOutTheme")).toBeNull()
  })

  it("gives way to the record when it was not chosen signed out", async () => {
    renderProvider()
    await waitFor(() => expect(html.className).toBe("light"))
    expect(probe.handle!.preferences.theme).toBe("light")
    expect(localStorage.getItem("theme")).toBe("light")
    expect(puts).toHaveLength(0)
  })
})
