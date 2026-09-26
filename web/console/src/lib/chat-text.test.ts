/** A reply's markdown subset and the record paths inside it. */

import { describe, expect, it } from "vitest"

import { chatBlocks, inlineRuns, recordPathOf } from "./chat-text"

describe("record paths in prose", () => {
  it("lifts a path out of a sentence, leaving its full stop", () => {
    expect(inlineRuns("See ada.localhost/tasks/task/x052.")).toEqual([
      { type: "text", text: "See " },
      {
        type: "record",
        kind: "ada.localhost/tasks/task",
        id: "x052",
        text: "ada.localhost/tasks/task/x052",
      },
      { type: "text", text: "." },
    ])
  })

  it("reads a path in backticks as the record, other code as code", () => {
    expect(inlineRuns("`ada.localhost/tasks/task/x1`")[0]).toMatchObject({
      type: "record",
      id: "x1",
    })
    expect(inlineRuns("`npm test`")).toEqual([
      { type: "code", text: "npm test" },
    ])
  })

  it("leaves a URL's path alone", () => {
    expect(
      inlineRuns("https://example.com/a/b/c").every((r) => r.type === "text")
    ).toBe(true)
  })

  it("matches only a whole path", () => {
    expect(recordPathOf("a.b/c/d/e")).toEqual({ kind: "a.b/c/d", id: "e" })
    expect(recordPathOf("not/a/path")).toBeUndefined()
    expect(recordPathOf("a.b/c/d/e and more")).toBeUndefined()
  })
})

describe("blocks", () => {
  it("reads paragraphs, lists, headings and code", () => {
    const blocks = chatBlocks(
      [
        "Two things:",
        "",
        "- **one**",
        "- two",
        "  continued",
        "",
        "1. first",
        "2. second",
        "",
        "## Next",
        "```",
        "raw <b>",
        "```",
      ].join("\n")
    )
    expect(blocks.map((b) => b.type)).toEqual([
      "paragraph",
      "list",
      "list",
      "heading",
      "code",
    ])
    const bullets = blocks[1]
    expect(bullets.type === "list" && bullets.ordered).toBe(false)
    expect(bullets.type === "list" && bullets.items[0]).toEqual([
      { type: "strong", text: "one" },
    ])
    expect(bullets.type === "list" && bullets.items[1]).toEqual([
      { type: "text", text: "two continued" },
    ])
    const numbered = blocks[2]
    expect(numbered.type === "list" && numbered.ordered).toBe(true)
    const code = blocks[4]
    expect(code.type === "code" && code.text).toBe("raw <b>")
  })

  it("keeps a paragraph's line breaks", () => {
    const [block] = chatBlocks("one\ntwo")
    expect(block.type === "paragraph" && block.lines).toHaveLength(2)
  })
})
