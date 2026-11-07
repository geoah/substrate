/** The result shapes the console draws itself: a function tool's output is
 * matched on its shape, never its name, and said in words. What a page or a
 * model chose is never trusted: an address that is not http(s) is dropped. */

import { describe, expect, it } from "vitest"

import type { ToolCallView } from "@/lib/api/transcript"

import { toolResult, toolResultSummary, webHost } from "./tool-views"

function call(over: Partial<ToolCallView>): ToolCallView {
  return { id: "c1", name: "search", arguments: "{}", ok: true, ...over }
}

const SEARCH_OUTPUT = JSON.stringify({
  effects: 0,
  output: {
    results: [
      {
        title: "I Tested the Best Robot Vacuums for 2026",
        url: "https://uk.pcmag.com/robot-vacuums/74630",
        snippet: "Six days ago, the Dyson...",
      },
      { title: "", url: "https://www.youtube.com/watch?v=1" },
      { title: "Sneaky", url: "javascript:alert(1)" },
    ],
  },
})

describe("a list of links", () => {
  it("reads its hits, titling one without a title by its site", () => {
    expect(toolResult(call({ output: SEARCH_OUTPUT }))).toEqual({
      type: "web-hits",
      hits: [
        {
          title: "I Tested the Best Robot Vacuums for 2026",
          url: "https://uk.pcmag.com/robot-vacuums/74630",
          snippet: "Six days ago, the Dyson...",
        },
        { title: "youtube.com", url: "https://www.youtube.com/watch?v=1" },
      ],
    })
  })

  it("says what it searched for and how much it found", () => {
    const args = JSON.stringify({ query: "quiet robot vacuum" })
    expect(
      toolResultSummary(call({ arguments: args, output: SEARCH_OUTPUT }))
    ).toBe("Searched for “quiet robot vacuum”, 2 results")
    expect(toolResultSummary(call({ arguments: args, ok: undefined }))).toBe(
      "Searched for “quiet robot vacuum”"
    )
    expect(toolResultSummary(call({ output: SEARCH_OUTPUT }))).toBe(
      "Found 2 results"
    )
  })

  it("draws nothing for a failed call or a list of something else", () => {
    expect(
      toolResult(call({ ok: false, output: SEARCH_OUTPUT }))
    ).toBeUndefined()
    expect(
      toolResult(call({ output: '{"output":{"results":[{"name":"x"}]}}' }))
    ).toBeUndefined()
  })
})

describe("a page", () => {
  it("reads the page and its length, and says where it read", () => {
    const output = JSON.stringify({
      output: {
        url: "https://www.formula1.com/en/latest",
        title: "F1's youngest world champions",
        document: "doc1",
        content: "x".repeat(1200),
      },
    })
    expect(toolResult(call({ output }))).toEqual({
      type: "web-page",
      page: {
        url: "https://www.formula1.com/en/latest",
        title: "F1's youngest world champions",
        characters: 1200,
      },
    })
    expect(toolResultSummary(call({ output }))).toBe("Read formula1.com")
  })

  it("is not any output that merely carries a url", () => {
    expect(
      toolResult(call({ output: '{"output":{"url":"https://example.com"}}' }))
    ).toBeUndefined()
  })
})

describe("a host", () => {
  it("names a site by its host, without www", () => {
    expect(webHost("https://www.bbc.co.uk/weather")).toBe("bbc.co.uk")
    expect(webHost("not a url")).toBe("not a url")
  })
})
