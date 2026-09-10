// @vitest-environment jsdom
/** The manifest view. Two owner reports live here: "the YAML is not formatted
 * — there's no syntax highlight", and "I cannot hover to see the description of
 * one of the properties". They are ONE surface, so the test pins them together:
 * the tint must actually reach the rendered runs, and the schema hovers must
 * stand whether or not it does: the annotations belong to the kind, not to the
 * highlighter.
 *
 * `useCodeTokens` is stubbed, so no grammar is loaded here. What this view
 * needs from the hook is its SHAPE: nothing on the first paint, one token row
 * per line a tick later. What the real highlighter produces from that grammar,
 * `src/lib/shiki.test.ts` pins. */

import { useEffect, useState } from "react"
import { cleanup, render, waitFor } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { CodeToken } from "@/lib/shiki"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a href={to}>{children}</a>
  ),
}))

/** One token per word and per run of punctuation, which is the granularity the
 * real grammar gives a YAML line and the granularity an annotation needs: a
 * described key or a linked reference lands on the run whose whole text is it.
 * The colors alternate so a test can tell one run's tint from its neighbour's. */
function stubTokens(source: string): CodeToken[][] {
  return source.split("\n").map((line, row) =>
    line
      .split(/([\s:]+)/)
      .filter((run) => run !== "")
      .map((content, col) => ({
        content,
        color: `var(--shiki-token-${(row + col) % 2 ? "keyword" : "string"})`,
        italic: false,
      }))
  )
}

vi.mock("@/lib/code", async (importOriginal) => {
  const real = await importOriginal<typeof import("@/lib/code")>()
  return {
    ...real,
    /** The real hook resolves a dynamic `import()` of the grammar bundle and
     * then tokenizes; the stub keeps the one thing the view can see, which is
     * that the tint arrives a render AFTER the text. Only this one export is
     * replaced, so a later import of `prettyJSON` from the same module still
     * gets the real function. */
    useCodeTokens: (source: string) => {
      const [tokens, setTokens] = useState<CodeToken[][] | undefined>(undefined)
      useEffect(() => {
        let cancelled = false
        // A microtask, not the same render: the real hook awaits a dynamic
        // `import()`, and the first paint being untinted is the whole point of
        // the "before the highlighter lands" case below.
        void Promise.resolve().then(() => {
          if (!cancelled) setTokens(stubTokens(source))
        })
        return () => {
          cancelled = true
        }
      }, [source])
      return tokens
    },
  }
})

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
  it("paints each run in the token's own color", async () => {
    const { container } = renderView()
    await waitFor(() => {
      expect(container.querySelector("pre span[style]")).toBeTruthy()
    })
    const colors = new Set(
      [...container.querySelectorAll<HTMLElement>("pre span[style]")].map(
        (el) => el.style.color
      )
    )
    // Every color the hook handed over reached an element, and none was
    // flattened into one: the view carries the token's color, not its own.
    expect(colors).toEqual(
      new Set(["var(--shiki-token-keyword)", "var(--shiki-token-string)"])
    )
  })

  it("hovers every described key BEFORE the highlighter lands", () => {
    // Synchronous first paint: the tokens are still absent. The hovers must
    // already be there. A shiki chunk that never arrives (or arrives late) may
    // cost the color, never the schema.
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
