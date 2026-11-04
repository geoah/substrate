// @vitest-environment jsdom
/** The appearance switch on the signed-out pages: it is on the landing
 * header and on the doors, it writes through the theme provider, and the
 * resolved theme reaches both the `<html>` class and `color-scheme`. */

import type { ReactNode } from "react"
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ to, children }: { to: string; children?: ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}))

vi.mock("@/lib/site", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/lib/site")>()
  return { ...real, useSite: () => null }
})

import { ThemeProvider } from "@/components/theme-provider"
import { DoorLayout } from "@/components/door/door-layout"
import { LandingPage } from "@/components/site/landing-page"
import { ColorSchemeMenu } from "./color-scheme-menu"

/** A `prefers-color-scheme: dark` query whose answer a case flips. */
const media = {
  dark: false,
  listeners: new Set<() => void>(),
  flip(dark: boolean) {
    media.dark = dark
    for (const listener of media.listeners) listener()
  },
}

beforeEach(() => {
  media.dark = false
  media.listeners.clear()
  localStorage.clear()
  document.documentElement.className = ""
  document.documentElement.style.colorScheme = ""
  window.matchMedia = (query: string) =>
    ({
      media: query,
      get matches() {
        return query.includes("dark") && media.dark
      },
      onchange: null,
      addEventListener: (_: string, listener: () => void) =>
        media.listeners.add(listener),
      removeEventListener: (_: string, listener: () => void) =>
        media.listeners.delete(listener),
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as unknown as MediaQueryList
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("<h1>Hello</h1>", { status: 200 }))
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const html = document.documentElement

async function choose(label: "System" | "Light" | "Dark") {
  fireEvent.click(screen.getByRole("button", { name: /^Appearance: / }))
  fireEvent.click(
    await screen.findByRole("menuitemradio", { name: new RegExp(label) })
  )
}

describe("ColorSchemeMenu", () => {
  it("is on the landing page's header", () => {
    render(
      <ThemeProvider>
        <LandingPage site={{ name: "Crane", landing: "/site/landing.html" }} />
      </ThemeProvider>
    )
    const header = document.querySelector("header")!
    expect(
      header.querySelector('[aria-label="Appearance: System"]')
    ).not.toBeNull()
  })

  it("is on the doors", () => {
    render(
      <ThemeProvider>
        <DoorLayout title="Sign in">
          <p>step</p>
        </DoorLayout>
      </ThemeProvider>
    )
    expect(
      screen.getByRole("button", { name: "Appearance: System" })
    ).toBeTruthy()
  })

  it("offers the three choices and marks the current one", async () => {
    localStorage.setItem("theme", "light")
    render(
      <ThemeProvider>
        <ColorSchemeMenu />
      </ThemeProvider>
    )
    fireEvent.click(screen.getByRole("button", { name: "Appearance: Light" }))
    const items = await screen.findAllByRole("menuitemradio")
    expect(items.map((i) => i.textContent?.trim())).toEqual([
      "System",
      "Light",
      "Dark",
    ])
    expect(items.map((i) => i.getAttribute("aria-checked")).join(",")).toBe(
      "false,true,false"
    )
  })

  it("applies a choice to the page, keeps it, and marks it for the record", async () => {
    render(
      <ThemeProvider>
        <ColorSchemeMenu />
      </ThemeProvider>
    )
    expect(html.className).toBe("light")
    expect(html.style.colorScheme).toBe("light")

    await choose("Dark")
    expect(html.className).toBe("dark")
    expect(html.style.colorScheme).toBe("dark")
    expect(localStorage.getItem("theme")).toBe("dark")
    expect(localStorage.getItem("substrate.console.signedOutTheme")).toBe(
      "dark"
    )
    expect(
      screen.getByRole("button", { name: "Appearance: Dark" })
    ).toBeTruthy()

    // An explicit choice outranks the operating system.
    act(() => media.flip(true))
    act(() => media.flip(false))
    expect(html.className).toBe("dark")
    expect(html.style.colorScheme).toBe("dark")
  })

  it("follows the operating system while the choice is System", async () => {
    localStorage.setItem("theme", "light")
    render(
      <ThemeProvider>
        <ColorSchemeMenu />
      </ThemeProvider>
    )
    await choose("System")
    expect(localStorage.getItem("theme")).toBe("system")
    expect(html.style.colorScheme).toBe("light")

    act(() => media.flip(true))
    expect(html.className).toBe("dark")
    expect(html.style.colorScheme).toBe("dark")

    act(() => media.flip(false))
    expect(html.className).toBe("light")
    expect(html.style.colorScheme).toBe("light")
  })
})
