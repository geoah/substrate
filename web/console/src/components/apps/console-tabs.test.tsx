// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { LauncherEntry } from "@/lib/apps/attach"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    children,
    ...rest
  }: {
    to: string
    children: ReactNode
    className?: string
    "aria-current"?: "page"
  }) => (
    <a href={to} {...rest}>
      {children}
    </a>
  ),
  useMatches: () => [],
  useRouterState: () => "/",
}))

// DynamicIcon lazy-loads a chunk per name; a span keeps the render synchronous.
vi.mock("@/components/apps/icon", () => ({
  AppIcon: ({ name }: { name?: string }) => <span data-icon={name} />,
}))

import {
  ConsoleTabBar,
  MAX_TABS,
  MORE,
  OVERVIEW,
  tabsFor,
} from "./console-tabs"

function entry(n: number): LauncherEntry {
  return {
    key: `view:v${n}`,
    label: `View ${n}`,
    icon: "list",
    to: `/views/v${n}`,
  }
}

const entries = (count: number) =>
  Array.from({ length: count }, (_, i) => entry(i + 1))

describe("tabsFor", () => {
  it("leads with Overview and keeps every entry that fits", () => {
    expect(tabsFor(entries(4)).map((t) => t.key)).toEqual([
      OVERVIEW.key,
      "view:v1",
      "view:v2",
      "view:v3",
      "view:v4",
    ])
  })

  it("gives the fifth slot to More once the entries overflow", () => {
    const tabs = tabsFor(entries(6))
    expect(tabs).toHaveLength(MAX_TABS)
    expect(tabs.map((t) => t.key)).toEqual([
      OVERVIEW.key,
      "view:v1",
      "view:v2",
      "view:v3",
      MORE.key,
    ])
  })
})

describe("ConsoleTabBar", () => {
  afterEach(cleanup)

  it("renders Overview and the entries, no More, when they fit", () => {
    render(<ConsoleTabBar entries={entries(3)} pathname="/" />)
    const nav = screen.getByRole("navigation", { name: "Console" })
    const links = within(nav).getAllByRole("link")
    expect(links.map((a) => a.textContent)).toEqual([
      "Overview",
      "View 1",
      "View 2",
      "View 3",
    ])
    expect(within(nav).queryByText("More")).toBeNull()
    expect(links[0].getAttribute("aria-current")).toBe("page")
  })

  it("renders at most five, the last one More opening the launcher", () => {
    render(<ConsoleTabBar entries={entries(7)} pathname="/views/v6" />)
    const links = screen.getAllByRole("link")
    expect(links).toHaveLength(MAX_TABS)
    const more = links[MAX_TABS - 1]
    expect(more.textContent).toBe("More")
    expect(more.getAttribute("href")).toBe("/apps")
    // v6 sits behind More, so More carries the current mark.
    expect(more.getAttribute("aria-current")).toBe("page")
  })

  it("lights the tab whose route the page is under", () => {
    render(
      <ConsoleTabBar entries={entries(3)} pathname="/views/v2/some-record" />
    )
    const lit = screen
      .getAllByRole("link")
      .filter((a) => a.getAttribute("aria-current") === "page")
    expect(lit.map((a) => a.textContent)).toEqual(["View 2"])
  })

  it("pads the safe area and keeps touch-height targets", () => {
    render(<ConsoleTabBar entries={entries(2)} pathname="/changelog" />)
    const nav = screen.getByRole("navigation", { name: "Console" })
    expect(nav.className).toContain("pb-[env(safe-area-inset-bottom)]")
    for (const link of screen.getAllByRole("link")) {
      expect(link.className).toContain("min-h-11")
    }
  })
})
