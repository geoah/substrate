// @vitest-environment jsdom
/** The old Registry, Connections and bundle Settings addresses land on the
 * Providers pages that replaced them. */

import { describe, expect, it } from "vitest"

import {
  bundleDetailRoute,
  bundleSettingsRoute,
  connectionDetailRoute,
  connectionsRoute,
  registryRoute,
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
    expect(
      target(bundleSettingsRoute, { id: "ada.example.com/firecrawl" })
    ).toMatchObject({
      to: "/providers/$authority/$pkg",
      params: { authority: "ada.example.com", pkg: "firecrawl" },
      hash: "settings",
    })
  })
})
