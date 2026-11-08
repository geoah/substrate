import { describe, expect, it } from "vitest"

import { mdBlocks, mdInline } from "./markdown"

describe("a Text's markdown", () => {
  it("reads bold, italic and code inside a line", () => {
    expect(mdInline("**Dune** · *Herbert* `1965`")).toEqual([
      { type: "strong", runs: [{ type: "text", text: "Dune" }] },
      { type: "text", text: " · " },
      { type: "em", runs: [{ type: "text", text: "Herbert" }] },
      { type: "text", text: " " },
      { type: "code", text: "1965" },
    ])
  })

  it("keeps a link's and an image's words and drops their targets", () => {
    expect(
      mdInline("see [the site](https://example.com) ![logo](x.png)")
    ).toEqual([{ type: "text", text: "see the site logo" }])
  })

  it("leaves an unmatched delimiter and a snake_case word as text", () => {
    expect(mdInline("2 * 3 and snake_case_word")).toEqual([
      { type: "text", text: "2 * 3 and snake_case_word" },
    ])
  })

  it("splits headings, lists, paragraphs and fenced code", () => {
    const blocks = mdBlocks(
      "### Reading\n\n- one\n- two\n\n1. first\n2. second\n\nline a\nline b\n\n```\ncode\n```"
    )
    expect(blocks.map((b) => b.type)).toEqual([
      "heading",
      "list",
      "list",
      "paragraph",
      "code",
    ])
    expect(blocks[0]).toMatchObject({ level: 3 })
    expect(blocks[2]).toMatchObject({ ordered: true })
    expect(blocks[3]).toMatchObject({
      lines: [[{ text: "line a" }], [{ text: "line b" }]],
    })
  })
})
