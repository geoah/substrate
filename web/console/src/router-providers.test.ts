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
  params: Record<string, string> = {}
): unknown {
  const beforeLoad = route.options.beforeLoad as (ctx: unknown) => void
  try {
    beforeLoad({ params })
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
