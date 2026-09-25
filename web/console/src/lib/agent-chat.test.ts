/** The chat surface's words: agent and thread names, day headings, what a
 * tool call did, and what an agent may see and change — each derived from the
 * records the page reads, so each is pinned here. */

import { describe, expect, it } from "vitest"

import {
  agentName,
  canChange,
  canSee,
  chatCapable,
  chatRows,
  dayGroup,
  examplePrompts,
  groupByDay,
  openingMessages,
  propertyLabel,
  proposedHeading,
  providerName,
  resolveTool,
  threadAgentId,
  threadTitle,
  toolRoute,
  toolSummary,
  valueWords,
} from "./agent-chat"
import type { ToolCallView } from "./api/transcript"
import type { SubstrateRecord } from "./api/types"

function record(over: Partial<SubstrateRecord>): SubstrateRecord {
  return {
    id: "",
    kind: "",
    properties: {},
    labels: {},
    version: 1,
    createdAt: "2026-09-25T10:00:00Z",
    updatedAt: "2026-09-25T10:00:00Z",
    ...over,
  }
}

const fn = (id: string) => ({
  function: { ref: `substrate.reamde.dev/core/function/${id}` },
})

const assistant = record({
  id: "ada.localhost/llm/substrate",
  kind: "substrate.reamde.dev/core/agent",
  properties: {
    description: "Chat with your substrate.",
    tools: [
      fn("substrate.reamde.dev/core/query"),
      fn("substrate.reamde.dev/core/write"),
      fn("substrate.reamde.dev/core/propose"),
      { ...fn("ada.localhost/notes/savenote"), name: "save" },
    ],
    subagents: [
      { ref: "substrate.reamde.dev/core/agent/ada.localhost/notes/titler" },
    ],
    permissions: {
      reads: { kinds: [{ ref: "substrate.reamde.dev/core/kind/*" }] },
      writes: [
        { ref: "substrate.reamde.dev/core/kind/ada.localhost/tasks/task" },
        {
          ref: "substrate.reamde.dev/core/kind/substrate.reamde.dev/core/recordpatchrequest",
        },
      ],
    },
    provider: { ref: "substrate.reamde.dev/llm/provider/openai" },
  },
})

const call = (over: Partial<ToolCallView>): ToolCallView => ({
  id: "c1",
  name: "query",
  arguments: "",
  output: "",
  ok: true,
  ...over,
})

describe("names", () => {
  it("reads an agent's local name as words", () => {
    expect(agentName("ada.localhost/llm/substrate")).toBe("Substrate")
    expect(agentName("ada.localhost/llm/substrateEditor")).toBe(
      "Substrate editor"
    )
    expect(agentName("notekeeper")).toBe("Notekeeper")
  })

  it("names a provider row the way its vendor spells itself", () => {
    expect(providerName("openai")).toBe("OpenAI")
    expect(providerName("mistral")).toBe("Mistral")
  })

  it("reads the agent a thread ran off its reference, served or authored", () => {
    const served = record({
      properties: {
        agent: {
          ref: "substrate.reamde.dev/core/agent/ada.localhost/llm/substrate",
        },
      },
    })
    expect(threadAgentId(served)).toBe("ada.localhost/llm/substrate")
    expect(threadAgentId(record({ properties: { agent: "scribe" } }))).toBe(
      "scribe"
    )
    expect(threadAgentId(record({}))).toBeUndefined()
  })

  it("keeps an agent that only works for other agents off the chat", () => {
    expect(chatCapable(assistant)).toBe(true)
    expect(chatCapable(record({ properties: { hiddenFromChat: true } }))).toBe(
      false
    )
  })
})

describe("thread titles", () => {
  it("is the opening message's first line, cut short", () => {
    expect(threadTitle("What is due?\nand more")).toBe("What is due?")
    const long = "x".repeat(100)
    expect(threadTitle(long)).toHaveLength(64)
    expect(threadTitle(long).endsWith("…")).toBe(true)
    expect(threadTitle(undefined)).toBe("New chat")
  })

  it("reads a delivery envelope as what fired", () => {
    const envelope = JSON.stringify({
      change: { op: "update", kind: "a.b/c/note", id: "n1" },
      record: { kind: "a.b/c/note", id: "n1", properties: { title: "Retro" } },
    })
    expect(threadTitle(envelope)).toBe("When Retro changed")
  })

  it("takes each thread's first user message", () => {
    const msg = (thread: string, content: string) =>
      record({
        properties: {
          thread: { ref: `substrate.reamde.dev/llm/thread/${thread}` },
          content,
        },
      })
    const openings = openingMessages([
      msg("t1", "first"),
      msg("t2", "other"),
      msg("t1", "second"),
    ])
    expect(openings.get("t1")).toBe("first")
    expect(openings.get("t2")).toBe("other")
    const rows = chatRows(
      [record({ id: "t1", properties: { agent: "scribe" } })],
      openings
    )
    expect(rows).toEqual([
      expect.objectContaining({ title: "first", agentId: "scribe" }),
    ])
  })
})

describe("day headings", () => {
  const now = new Date(2026, 8, 25, 15, 0)
  it("groups by the local calendar day", () => {
    expect(dayGroup(new Date(2026, 8, 25, 0, 5).toISOString(), now)).toBe(
      "Today"
    )
    expect(dayGroup(new Date(2026, 8, 24, 23, 55).toISOString(), now)).toBe(
      "Yesterday"
    )
    expect(dayGroup(new Date(2026, 8, 20).toISOString(), now)).toBe("Earlier")
  })

  it("drops empty headings and keeps the order items arrived in", () => {
    const items = [
      new Date(2026, 8, 25, 9).toISOString(),
      new Date(2026, 8, 1).toISOString(),
      new Date(2026, 8, 25, 8).toISOString(),
    ]
    const groups = groupByDay(items, (i) => i, now)
    expect(groups.map((g) => g.day)).toEqual(["Today", "Earlier"])
    expect(groups[0].items).toEqual([items[0], items[2]])
  })
})

describe("what a tool call did", () => {
  it("resolves a name through the agent's own tools and sub-agents", () => {
    expect(resolveTool(assistant, "query").function).toBe(
      "substrate.reamde.dev/core/query"
    )
    expect(resolveTool(assistant, "save").function).toBe(
      "ada.localhost/notes/savenote"
    )
    expect(resolveTool(assistant, "titler").subagent).toBe(
      "ada.localhost/notes/titler"
    )
    expect(resolveTool(assistant, "unknown")).toEqual({})
    // Unread agent: the host functions still read by their own names.
    expect(resolveTool(undefined, "propose").function).toBe(
      "substrate.reamde.dev/core/propose"
    )
  })

  it("says a read in words: a search, a list, one record, and what it found", () => {
    const query = { function: "substrate.reamde.dev/core/query" }
    expect(
      toolSummary(
        call({
          arguments: '{"q":"handover"}',
          output: '{"records":[{"kind":"a.b/c/d","id":"1"}]}',
        }),
        query
      )
    ).toBe("Searched for “handover”, found 1")
    expect(
      toolSummary(
        call({
          arguments:
            '{"filter":{"kinds":["ada.localhost/tasks/task","ada.localhost/notes/note"]}}',
        }),
        query
      )
    ).toBe("Looked through your Tasks and Notes")
    expect(
      toolSummary(
        call({ arguments: '{"kind":"ada.localhost/people/person","id":"g"}' }),
        query
      )
    ).toBe("Looked up one person")
    expect(toolSummary(call({}), query)).toBe("Looked things up")
  })

  it("says the other host functions and a sub-agent in words", () => {
    expect(
      toolSummary(call({}), { function: "substrate.reamde.dev/core/propose" })
    ).toBe("Suggested a change")
    expect(
      toolSummary(
        call({ arguments: '{"op":"patch","kind":"a.b/tasks/task"}' }),
        {
          function: "substrate.reamde.dev/core/write",
        }
      )
    ).toBe("Changed a task")
    expect(
      toolSummary(call({}), { function: "substrate.reamde.dev/core/ask" })
    ).toBe("Asked you some questions")
    expect(
      toolSummary(call({}), { subagent: "ada.localhost/notes/titler" })
    ).toBe("Asked Titler")
  })

  it("reads any other function by its description, else its name", () => {
    expect(
      toolSummary(call({}), {
        function: "ada.localhost/notes/savenote",
        description: "Saved a note",
      })
    ).toBe("Saved a note")
    expect(toolSummary(call({ name: "stats" }), {})).toBe("Used stats")
  })

  it("names a tool it used as the Tools page does", () => {
    expect(
      toolSummary(call({}), {
        function: "providers.substrate.reamde.dev/google/synccalendar",
      })
    ).toBe("Used Google Calendar sync")
    expect(
      toolSummary(call({}), { function: "ada.localhost/notes/savenote" })
    ).toBe("Used save note")
  })

  it("routes a tool", () => {
    expect(toolRoute("substrate.reamde.dev/core/query")).toEqual({
      authority: "substrate.reamde.dev",
      pkg: "core",
      name: "query",
    })
    expect(toolRoute("not-a-reference")).toBeUndefined()
  })
})

describe("what an agent may see and change", () => {
  it("reads a wildcard read grant as all your data", () => {
    expect(canSee(assistant)).toBe("All your data")
  })

  it("reads exact kinds and globs as words", () => {
    const agent = record({
      properties: {
        permissions: {
          reads: {
            kinds: ["ada.localhost/tasks/task", "ada.localhost/notes/*"],
          },
        },
      },
    })
    expect(canSee(agent)).toBe("Tasks and everything in Notes")
    expect(canSee(record({}))).toBeUndefined()
  })

  it("separates changing things itself from suggesting them", () => {
    expect(canChange(assistant)).toBe(
      "Tasks. It suggests anything you should decide yourself."
    )
    const proposer = record({
      properties: {
        tools: [fn("substrate.reamde.dev/core/propose")],
        permissions: {
          writes: [
            "substrate.reamde.dev/core/kind/substrate.reamde.dev/core/recordpatchrequest",
          ],
        },
      },
    })
    expect(canChange(proposer)).toBe(
      "Nothing on its own; it suggests changes and you approve each one"
    )
    expect(canChange(record({}))).toBe("Nothing")
    expect(
      canChange(
        record({ properties: { tools: [fn("ada.localhost/notes/savenote")] } })
      )
    ).toBe(
      "Nothing directly. Its tools can, each within what it’s allowed to do"
    )
  })

  it("offers openers by what the agent can do", () => {
    expect(examplePrompts(assistant)).toHaveLength(3)
    expect(examplePrompts(record({}))).toEqual(["What can you help me with?"])
    expect(examplePrompts(undefined)).toEqual([])
  })
})

describe("a suggested change, in words", () => {
  it("labels a property", () => {
    expect(propertyLabel("dueAt")).toBe("Due")
    expect(propertyLabel("assignee")).toBe("Assignee")
    expect(propertyLabel("first_name")).toBe("First name")
  })

  it("reads a value", () => {
    expect(valueWords(null)).toBe("Empty")
    expect(valueWords(true)).toBe("Yes")
    expect(valueWords("urgent")).toBe("urgent")
    expect(valueWords(["a", "b"])).toBe("a, b")
    expect(valueWords("2026-10-02T09:00:00Z")).not.toContain("T09")
  })

  it("finds what a new record would be called", () => {
    expect(proposedHeading({ name: "Status page", status: "open" })).toEqual({
      key: "name",
      text: "Status page",
    })
    expect(proposedHeading({ status: "open" })).toBeUndefined()
  })
})
