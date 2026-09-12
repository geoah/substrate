/** The tokenizer itself, against the real highlighter, under node. What the
 * grammars and the css-variables theme produce is pinned here, where no DOM is
 * involved and the load costs 55 ms. `yaml-view.test.tsx` stubs
 * `useCodeTokens` instead of tokenizing for real; the suites that render
 * `CodeBlock` (tool-call, trigger-context, row-detail) still reach the real
 * hook, so they load the grammar bundle too.
 *
 * Two things are worth pinning. Every color must ride a `--shiki-*` variable,
 * because that is what makes the tint follow light and dark (the app defines
 * the variables in `index.css`); a theme with literal hex colors would look
 * right in one mode and wrong in the other. And the line array is the source's
 * lines, one for one, because every caller renders its own line count and
 * indexes the tokens by it. */

import { describe, expect, it } from "vitest"

import { tokenize, type CodeToken } from "./shiki"

const YAML = `# the manifest
kind: substrate.reamde.dev/llm/provider
metadata:
  id: default
`

const flat = (rows: CodeToken[][]) => rows.flat()

describe("tokenize", () => {
  it("returns one row per line of the source, in order", async () => {
    const rows = await tokenize(YAML, "yaml")
    expect(rows).toHaveLength(YAML.split("\n").length)
    expect(rows.map((row) => row.map((t) => t.content).join(""))).toEqual(
      YAML.split("\n")
    )
  })

  it("colors every token through a --shiki-* variable, and not all alike", async () => {
    const colors = new Set(
      flat(await tokenize(YAML, "yaml")).map((t) => t.color)
    )
    expect(colors.size).toBeGreaterThan(1)
    for (const color of colors) {
      expect(color).toMatch(/^var\(--shiki-[\w-]+\)$/)
    }
  })

  it("italicizes a comment, which the css-variables theme does not", async () => {
    const [comment] = await tokenize(YAML, "yaml")
    expect(comment.map((t) => t.content).join("")).toBe("# the manifest")
    expect(comment.every((t) => t.italic)).toBe(true)
  })

  it("tints JSON too, and marks nothing else italic", async () => {
    const rows = await tokenize('{"a": 1}', "json")
    const tokens = flat(rows)
    expect(tokens.map((t) => t.content).join("")).toBe('{"a": 1}')
    expect(new Set(tokens.map((t) => t.color)).size).toBeGreaterThan(1)
    expect(tokens.some((t) => t.italic)).toBe(false)
  })
})
