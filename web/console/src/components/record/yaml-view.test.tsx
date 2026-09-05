/** The manifest view. Two owner reports live here: "the YAML is not formatted
 * — there's no syntax highlight", and "I cannot hover to see the description of
 * one of the properties". They are ONE surface, so the test pins them together:
 * shiki must actually tint, and the schema hovers must stand whether or not it
 * does — the annotations belong to the kind, not to the highlighter. */

import { cleanup, render, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}))

import { YamlView } from "./yaml-view"
import type { KeyDocs } from "@/lib/yaml-annotations"

const SOURCE = `kind: substrate.reamde.dev/core/llmprovider
metadata:
  id: default
data:
  properties:
    name: default
    wire: openai
`

const DOCS: KeyDocs = {
  properties: {
    name: {
      type: "string",
      description: "a human label for the row (the id is the reference)",
    },
    wire: { type: "string" },
  },
}

const TARGETS = {
  ids: {},
  kinds: {
    "substrate.reamde.dev/core/llmprovider":
      "/data/substrate.reamde.dev/core/llmproviders",
  },
}

function renderView() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <YamlView source={SOURCE} docs={DOCS} targets={TARGETS} />
    </QueryClientProvider>
  )
}

const triggers = (root: ParentNode) =>
  [...root.querySelectorAll("[data-slot=tooltip-trigger]")].map(
    (el) => el.textContent
  )

afterEach(cleanup)

describe("YamlView", () => {
  it("tints the manifest — every color rides a --shiki-* variable", async () => {
    const { container } = renderView()
    await waitFor(() => {
      expect(container.querySelector("pre span[style]")).toBeTruthy()
    })
    const colors = new Set(
      [...container.querySelectorAll<HTMLElement>("pre span[style]")].map(
        (el) => el.style.color
      )
    )
    // The css-variables theme is what makes the tint follow light/dark.
    expect(colors.size).toBeGreaterThan(1)
    for (const color of colors) expect(color).toContain("--shiki-")
  })

  it("hovers every described key BEFORE the highlighter lands", () => {
    // Synchronous first paint: the tokens query is still pending. The hovers
    // must already be there — a shiki chunk that never arrives (or arrives
    // late) may cost the color, never the schema.
    const { container } = renderView()
    expect(container.querySelector("pre span[style]")).toBeNull()
    expect(triggers(container)).toEqual(["name", "wire"])
  })

  it("keeps the same hovers once tinted", async () => {
    const { container } = renderView()
    await waitFor(() => {
      expect(container.querySelector("pre span[style]")).toBeTruthy()
    })
    expect(triggers(container)).toEqual(["name", "wire"])
  })

  it("renders the whole document, tinted or not — line for line", async () => {
    const { container } = renderView()
    const lines = SOURCE.split("\n")
    const plain = container.querySelector("pre")!
    // The line count is the source's, not the highlighter's: no reflow when
    // the tint lands.
    expect(plain.children).toHaveLength(lines.length)
    for (const line of lines) {
      if (line) expect(plain.textContent).toContain(line)
    }
    await waitFor(() => {
      expect(container.querySelector("pre span[style]")).toBeTruthy()
    })
    const tinted = container.querySelector("pre")!
    expect(tinted.children).toHaveLength(lines.length)
    for (const line of lines) {
      if (line) expect(tinted.textContent).toContain(line)
    }
  })

  it("links a known kind reference from its key position", async () => {
    const { container } = renderView()
    await waitFor(() => {
      expect(container.querySelector("pre span[style]")).toBeTruthy()
    })
    const link = container.querySelector("a")
    expect(link?.getAttribute("href")).toBe(
      "/data/substrate.reamde.dev/core/llmproviders"
    )
    expect(link?.textContent).toBe("substrate.reamde.dev/core/llmprovider")
  })
})
