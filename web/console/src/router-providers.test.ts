// @vitest-environment jsdom
/** The old Registry, Connections, Services and bundle Settings addresses land
 * on the Providers pages that replaced them. */

import { afterEach, describe, expect, it } from "vitest"

import {
  bundleDetailRoute,
  bundleSettingsRoute,
  connectionDetailRoute,
  connectionsRoute,
  localHref,
  registryRoute,
  servicesRoute,
} from "@/router"

/** What a route's beforeLoad redirects to. */
function target(
  route: { options: { beforeLoad?: unknown } },
  params: Record<string, string> = {},
  search: Record<string, unknown> = {}
): unknown {
  const beforeLoad = route.options.beforeLoad as (ctx: unknown) => void
  try {
    beforeLoad({ params, search, location: { search } })
  } catch (thrown) {
    const r = thrown as { options?: Record<string, unknown> }
    const { to, params: p, search, hash } = r.options ?? {}
    return { to, params: p, search, hash }
  }
  return undefined
}

describe("the retired provider addresses", () => {
  it("sends the registry and connections lists to Providers", () => {
    expect(target(registryRoute)).toMatchObject({ to: "/providers" })
    expect(target(connectionsRoute)).toMatchObject({ to: "/providers" })
  })

  it("sends a Services bookmark to Providers, which lists what it did", () => {
    expect(target(servicesRoute)).toMatchObject({ to: "/providers" })
  })

  it("carries an OAuth return to the connected account's provider page", () => {
    // The substrate's return page falls back to `/registry?connected=`.
    expect(
      target(
        registryRoute,
        {},
        {
          connected:
            "providers.substrate.reamde.dev/google/account/george-work",
        }
      )
    ).toMatchObject({
      to: "/providers/$authority/$pkg",
      params: { authority: "providers.substrate.reamde.dev", pkg: "google" },
      search: {
        account: "george-work",
        connected: "providers.substrate.reamde.dev/google/account/george-work",
      },
    })
  })

  it("carries an OAuth failure, or an account it cannot place, to Providers", () => {
    // A digit-only correlation arrives parsed as a number.
    expect(target(registryRoute, {}, { error: 4812 })).toEqual({
      to: "/providers",
      search: { error: "4812" },
    })
    expect(target(registryRoute, {}, { connected: "george-work" })).toEqual({
      to: "/providers",
      search: { connected: "george-work" },
    })
  })

  it("sends a bundle to its provider page, and a shipped sample to All data", () => {
    expect(
      target(bundleDetailRoute, { id: "providers.substrate.reamde.dev/google" })
    ).toMatchObject({
      to: "/providers/$authority/$pkg",
      params: { authority: "providers.substrate.reamde.dev", pkg: "google" },
    })
    expect(
      target(bundleDetailRoute, { id: "samples.substrate.reamde.dev/tasks" })
    ).toMatchObject({ to: "/data" })
  })

  it("sends an account to its provider page, naming the account", () => {
    expect(
      target(connectionDetailRoute, {
        authority: "providers.substrate.reamde.dev",
        pkg: "google",
        name: "account",
        id: "george-work",
      })
    ).toMatchObject({
      to: "/providers/$authority/$pkg",
      params: { authority: "providers.substrate.reamde.dev", pkg: "google" },
      search: { account: "george-work" },
    })
  })

  it("sends a bundle's settings to its provider page's settings", () => {
    expect(target(bundleSettingsRoute, { id: "~/firecrawl" })).toMatchObject({
      to: "/providers/$authority/$pkg",
      params: { authority: "~", pkg: "firecrawl" },
      hash: "settings",
    })
  })
})

/** The repository's own packages sit under `~`, and its full name is another
 * spelling of it (decision record 0259): an address from before names the
 * package by the name. */
describe("an address under the repository's full name", () => {
  afterEach(() => localStorage.clear())

  it("lands a held package's old Registry and settings addresses under ~", () => {
    localStorage.setItem("substrate.repository", "ada.example.com")
    expect(
      target(bundleDetailRoute, { id: "ada.example.com/tasks" })
    ).toMatchObject({
      to: "/providers/$authority/$pkg",
      params: { authority: "~", pkg: "tasks" },
    })
    expect(
      target(bundleSettingsRoute, { id: "ada.example.com/firecrawl" })
    ).toMatchObject({ params: { authority: "~", pkg: "firecrawl" } })
    // Another authority's package keeps its own.
    expect(
      target(bundleSettingsRoute, { id: "other.example.com/firecrawl" })
    ).toMatchObject({
      params: { authority: "other.example.com", pkg: "firecrawl" },
    })
  })

  it("moves a data, tool or provider address to ~, search and hash kept", () => {
    const name = "ada.example.com"
    expect(localHref("/data/ada.example.com/tasks/task/01jbx", name)).toBe(
      "/data/~/tasks/task/01jbx"
    )
    expect(localHref("/data/ada.example.com", name)).toBe("/data/~")
    expect(localHref("/tools/ada.example.com/notes/save?x=1#a", name)).toBe(
      "/tools/~/notes/save?x=1#a"
    )
    expect(localHref("/providers/ada.example.com/tasks", name)).toBe(
      "/providers/~/tasks"
    )
  })

  it("leaves every other address alone", () => {
    const name = "ada.example.com"
    expect(localHref("/data/~/tasks/task", name)).toBeUndefined()
    expect(localHref("/data/ada.example.com.uk/tasks", name)).toBeUndefined()
    expect(localHref("/data/other.example.com/tasks", name)).toBeUndefined()
    expect(localHref("/agents/ada.example.com", name)).toBeUndefined()
    expect(localHref("/data/ada.example.com/tasks", null)).toBeUndefined()
  })
})
