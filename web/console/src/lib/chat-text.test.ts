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

const BEAN = "mneme.rxtx.dev/coffee/bean/routes-coffee-skittle-lane"

describe("links", () => {
  it("reads a substrate:// link as the record it names", () => {
    expect(inlineRuns(`[Skittle Lane](substrate://${BEAN}) is saved`)).toEqual([
      {
        type: "record",
        kind: "mneme.rxtx.dev/coffee/bean",
        id: "routes-coffee-skittle-lane",
        text: "Skittle Lane",
      },
      { type: "text", text: " is saved" },
    ])
  })

  it("reads a ref: link as the record it names", () => {
    expect(inlineRuns(`[Skittle Lane](ref:${BEAN}) is saved`)[0]).toEqual({
      type: "record",
      kind: "mneme.rxtx.dev/coffee/bean",
      id: "routes-coffee-skittle-lane",
      text: "Skittle Lane",
    })
  })

  it("keeps a declaration's slashed id whole", () => {
    expect(
      inlineRuns(
        "[task](substrate://substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/tasks/task)"
      )
    ).toEqual([
      {
        type: "record",
        kind: "substrate.reamde.dev/core/kind",
        id: "samples.substrate.reamde.dev/tasks/task",
        text: "task",
      },
    ])
  })

  it("reads an https link as a web link", () => {
    expect(
      inlineRuns("See [BBC Weather](https://www.bbc.com/weather/2761369).")
    ).toEqual([
      { type: "text", text: "See " },
      {
        type: "link",
        href: "https://www.bbc.com/weather/2761369",
        text: "BBC Weather",
      },
      { type: "text", text: "." },
    ])
    expect(inlineRuns("[plain](http://example.com)")).toEqual([
      { type: "link", href: "http://example.com/", text: "plain" },
    ])
  })

  it("keeps balanced parentheses in a URL and drops a title", () => {
    expect(
      inlineRuns(
        '[Coffee](https://en.wikipedia.org/wiki/Coffee_(drink) "the drink")'
      )
    ).toEqual([
      {
        type: "link",
        href: "https://en.wikipedia.org/wiki/Coffee_(drink)",
        text: "Coffee",
      },
    ])
  })

  it("shows any other scheme as its text with the target beside it", () => {
    for (const target of [
      "javascript:alert(1)",
      "JavaScript:alert(1)",
      "data:text/html,<b>x</b>",
      "mailto:someone@example.com",
      "/data/x",
      "substrate://console/agents",
    ]) {
      const runs = inlineRuns(`[click](${target})`)
      expect(runs).toEqual([{ type: "text", text: `click (${target})` }])
    }
  })

  it("never reads a record path inside a link twice", () => {
    expect(inlineRuns(`[${BEAN}](substrate://${BEAN})`)).toEqual([
      {
        type: "record",
        kind: "mneme.rxtx.dev/coffee/bean",
        id: "routes-coffee-skittle-lane",
        text: BEAN,
      },
    ])
    expect(inlineRuns(`[about ${BEAN}](https://example.com/${BEAN})`)).toEqual([
      {
        type: "link",
        href: `https://example.com/${BEAN}`,
        text: `about ${BEAN}`,
      },
    ])
    expect(inlineRuns(`[see ${BEAN}](ftp://example.com/${BEAN})`)).toEqual([
      { type: "text", text: `see ${BEAN} (ftp://example.com/${BEAN})` },
    ])
  })

  it("keeps nested and escaped brackets in the link text", () => {
    expect(inlineRuns("[a [b] c](https://example.com)")).toEqual([
      { type: "link", href: "https://example.com/", text: "a [b] c" },
    ])
    expect(inlineRuns("[a \\] b](https://example.com)")).toEqual([
      { type: "link", href: "https://example.com/", text: "a ] b" },
    ])
  })

  it("leaves malformed links as the text they were", () => {
    for (const line of [
      "[open](https://example.com",
      "[no target]",
      "[spaced] (https://example.com)",
      "[empty]()",
      "[a](two words)",
      "[unclosed [link](",
    ]) {
      expect(inlineRuns(line)).toEqual([{ type: "text", text: line }])
    }
    // An unbalanced bracket before a link leaves only the link read.
    expect(inlineRuns("[a [b](https://example.com)")).toEqual([
      { type: "text", text: "[a " },
      { type: "link", href: "https://example.com/", text: "b" },
    ])
  })

  it("decodes character references in the target before the scheme check", () => {
    expect(
      inlineRuns(
        "[download](https://example.com/file?token=a&amp;expires=b&#38;x=&#x31;)"
      )
    ).toEqual([
      {
        type: "link",
        href: "https://example.com/file?token=a&expires=b&x=1",
        text: "download",
      },
    ])
    // A reference cannot smuggle a scheme past the allowlist.
    expect(inlineRuns("[x](&#106;avascript:alert(1))")).toEqual([
      { type: "text", text: "x (javascript:alert(1))" },
    ])
    // An escaped ampersand is the text it was.
    expect(inlineRuns("[x](https://example.com/?a=1\\&amp;b=2)")).toEqual([
      { type: "link", href: "https://example.com/?a=1&amp;b=2", text: "x" },
    ])
  })

  it("reads a tab as the space before a title", () => {
    expect(
      inlineRuns('[weather](https://example.com/weather\t"Weather")')
    ).toEqual([
      { type: "link", href: "https://example.com/weather", text: "weather" },
    ])
    expect(inlineRuns("[w](\thttps://example.com/w\t)")).toEqual([
      { type: "link", href: "https://example.com/w", text: "w" },
    ])
  })

  it("reads a line of unmatched brackets or openings in linear time", () => {
    const n = 64_000
    for (const line of [
      "[".repeat(n),
      "[a](".repeat(n / 4),
      "[a](<".repeat(n / 5),
      '[a](x "'.repeat(n / 7),
      "[](x".repeat(n / 4),
    ]) {
      const started = performance.now()
      const runs = inlineRuns(line)
      const took = performance.now() - started
      expect(runs.map((r) => r.type)).toEqual(["text"])
      expect(took).toBeLessThan(250)
    }
    // A link after a long malformed run is still read.
    const tail = inlineRuns(`${"[".repeat(n)} [ok](https://example.com)`)
    expect(tail.at(-1)).toEqual({
      type: "link",
      href: "https://example.com/",
      text: "ok",
    })
  })

  it("reads a link inside bold, and leaves one inside code alone", () => {
    expect(inlineRuns(`**[Skittle Lane](ref:${BEAN})**`)).toEqual([
      {
        type: "strong",
        runs: [
          {
            type: "record",
            kind: "mneme.rxtx.dev/coffee/bean",
            id: "routes-coffee-skittle-lane",
            text: "Skittle Lane",
          },
        ],
      },
    ])
    expect(inlineRuns("`[x](https://example.com)`")).toEqual([
      { type: "code", text: "[x](https://example.com)" },
    ])
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
      { type: "strong", runs: [{ type: "text", text: "one" }] },
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

  it("reads bold and links on every line of a paragraph and a list", () => {
    const [paragraph, list] = chatBlocks(
      "one\n**two** [x](https://example.com)\n\n- a\n- **b**"
    )
    expect(paragraph.type === "paragraph" && paragraph.lines[1]).toEqual([
      { type: "strong", runs: [{ type: "text", text: "two" }] },
      { type: "text", text: " " },
      { type: "link", href: "https://example.com/", text: "x" },
    ])
    expect(list.type === "list" && list.items[1]).toEqual([
      { type: "strong", runs: [{ type: "text", text: "b" }] },
    ])
  })
})
