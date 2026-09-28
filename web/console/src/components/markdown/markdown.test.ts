// @vitest-environment jsdom
/** The editor stores Markdown: what it reads it writes back unchanged, a
 * record link is a plain Markdown link to `substrate://<kind>/<id>`, and a link to
 * any other URL stays a link. */

import { Editor, type JSONContent } from "@tiptap/core"
import { afterEach, describe, expect, it } from "vitest"

import { markdownExtensions, storedMarkdown } from "./extensions"
import { recordLinkMarkdown } from "./record-link"

let editor: Editor | undefined

function load(markdown: string): Editor {
  editor = new Editor({
    extensions: markdownExtensions(),
    content: markdown,
    contentType: "markdown",
  })
  return editor
}

afterEach(() => {
  editor?.destroy()
  editor = undefined
})

const PERSON = "ada.example.com/people/person"

describe("the Markdown round trip", () => {
  it.each([
    ["paragraphs", "One paragraph.\n\nAnother."],
    ["headings", "# One\n\n## Two\n\n### Three"],
    ["marks", "**bold**, *italic*, ~~struck~~ and `code`"],
    ["a link", "[the spec](https://example.com/spec)"],
    ["a bulleted list", "- one\n- two"],
    ["a numbered list", "1. one\n2. two"],
    ["a to-do list", "- [ ] open\n- [x] done"],
    ["a quote", "> quoted"],
    ["a code block", "```go\nfunc main() {}\n```"],
    ["a divider", "above\n\n---\n\nbelow"],
    [
      "a table",
      "| Name  | Role     |\n| ----- | -------- |\n| Ada   | Engineer |\n| Grace | Admiral  |",
    ],
    ["a record link", `Met [Ada Lovelace](substrate://${PERSON}/ada) today.`],
  ])("keeps %s", (_, markdown) => {
    expect(storedMarkdown(load(markdown))).toBe(markdown)
  })

  it("pads a table's columns and keeps the padded form", () => {
    const table = storedMarkdown(load("| a | bb |\n| - | - |\n| ccc | d |"))
    expect(table).toBe("| a   | bb  |\n| --- | --- |\n| ccc | d   |")
    expect(storedMarkdown(load(table))).toBe(table)
  })

  it("reads a substrate:// link as a record link, not as a link", () => {
    const json = load(`See [Ada](substrate://${PERSON}/ada).`).getJSON()
    const para = json.content?.[0]
    expect(para?.content?.[1]).toEqual({
      type: "recordLink",
      attrs: { kind: PERSON, id: "ada", title: "Ada" },
    })
  })

  it("leaves a substrate:// target that names no record as a plain link", () => {
    const json = load("[odd](substrate://not-a-path)").getJSON()
    const text = json.content?.[0]?.content?.[0]
    expect(text?.type).toBe("text")
    expect(text?.marks?.[0]?.type).toBe("link")
  })

  it("writes the record link it inserts", () => {
    const e = load("Met ")
    e.commands.focus("end")
    e.commands.insertRecordLink({ kind: PERSON, id: "ada", title: "Ada" })
    // The space after it is the cursor's, and it is not stored at a line end.
    expect(e.getMarkdown()).toBe(`Met [Ada](substrate://${PERSON}/ada) `)
    expect(storedMarkdown(e)).toBe(`Met [Ada](substrate://${PERSON}/ada)`)
  })
})

describe("record link text", () => {
  it("escapes brackets in the title and falls back to the id", () => {
    const md = recordLinkMarkdown({ kind: PERSON, id: "a1", title: "[draft]" })
    expect(md).toBe(`[\\[draft\\]](substrate://${PERSON}/a1)`)
    const json: JSONContent = load(md).getJSON()
    expect(json.content?.[0]?.content?.[0]?.attrs).toEqual({
      kind: PERSON,
      id: "a1",
      title: "[draft]",
    })
    expect(recordLinkMarkdown({ kind: PERSON, id: "a1", title: "" })).toBe(
      `[a1](substrate://${PERSON}/a1)`
    )
  })

  it("copies as the record's console page and pastes back as the link", () => {
    const html = load(`See [Ada](substrate://${PERSON}/ada).`).getHTML()
    const anchor = new DOMParser()
      .parseFromString(html, "text/html")
      .querySelector("a")!
    expect(anchor.getAttribute("href")).toBe(
      `${location.origin}/data/${PERSON}/ada`
    )
    expect(anchor.hasAttribute("id")).toBe(false)
    const pasted = load("")
    pasted.commands.setContent(html)
    expect(storedMarkdown(pasted)).toBe(`See [Ada](substrate://${PERSON}/ada).`)
  })
})
