// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

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
  APP_ICON,
  ConsoleTabBar,
  MAX_TABS,
  MORE,
  OVERVIEW,
  launcherEntries,
  tabsFor,
  type LauncherEntry,
} from "./console-tabs"

function entry(n: number): LauncherEntry {
  return {
    key: `app:a${n}`,
    label: `App ${n}`,
    icon: "list",
    to: `/apps/a${n}`,
  }
}

const entries = (count: number) =>
  Array.from({ length: count }, (_, i) => entry(i + 1))

function app(
  id: string,
  properties: Record<string, unknown> = {}
): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/core/app",
    properties,
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

describe("launcherEntries", () => {
  it("lists launcher apps in id order, home first, off the raw rows", () => {
    const out = launcherEntries([
      app("tasks", { name: "Tasks", icon: "check-square" }),
      app("agenda", { name: "Agenda", attach: ["home"] }),
      app("pulls", { name: "Pull requests", home: true }),
      app("contacts", { attach: ["launcher", "browse"] }),
    ])
    expect(out.map((e) => e.key)).toEqual([
      "app:pulls",
      "app:contacts",
      "app:tasks",
    ])
    expect(out[0].icon).toBe(APP_ICON)
    expect(out[1].label).toBe("contacts")
    expect(out[2]).toEqual({
      key: "app:tasks",
      label: "Tasks",
      icon: "check-square",
      to: "/apps/tasks",
    })
  })
})

describe("tabsFor", () => {
  it("leads with Overview and keeps every entry that fits", () => {
    expect(tabsFor(entries(4)).map((t) => t.key)).toEqual([
      OVERVIEW.key,
      "app:a1",
      "app:a2",
      "app:a3",
      "app:a4",
    ])
  })

  it("gives the fifth slot to More once the entries overflow", () => {
    const tabs = tabsFor(entries(6))
    expect(tabs).toHaveLength(MAX_TABS)
    expect(tabs.map((t) => t.key)).toEqual([
      OVERVIEW.key,
      "app:a1",
      "app:a2",
      "app:a3",
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
      "App 1",
      "App 2",
      "App 3",
    ])
    expect(within(nav).queryByText("More")).toBeNull()
    expect(links[0].getAttribute("aria-current")).toBe("page")
  })

  it("renders at most five, the last one More opening the launcher", () => {
    render(<ConsoleTabBar entries={entries(7)} pathname="/apps/a6" />)
    const links = screen.getAllByRole("link")
    expect(links).toHaveLength(MAX_TABS)
    const more = links[MAX_TABS - 1]
    expect(more.textContent).toBe("More")
    expect(more.getAttribute("href")).toBe("/apps")
    // a6 sits behind More, so More carries the current mark.
    expect(more.getAttribute("aria-current")).toBe("page")
  })

  it("lights the tab whose route the page is under, the splat included", () => {
    render(
      <ConsoleTabBar entries={entries(3)} pathname="/apps/a2/some/route" />
    )
    const lit = screen
      .getAllByRole("link")
      .filter((a) => a.getAttribute("aria-current") === "page")
    expect(lit.map((a) => a.textContent)).toEqual(["App 2"])
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
