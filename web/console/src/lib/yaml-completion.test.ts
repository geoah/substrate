/** What the YAML lens offers, and where. The rules are the declaration's: the
 * envelope's own keys at the top, the declared properties inside
 * `data.properties`, an enum's admitted values after its key, a state machine's
 * states, the declared edge rels after `rel:` — and never a property the
 * document already writes. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { completionsAt, pathAt, writtenProperties } from "./yaml-completion"

const taskKind: KindInfo = {
  identity: "tasks.substrate.reamde.dev/task",
  name: "task",
  authority: "tasks.substrate.reamde.dev",
  version: "",
  plural: "tasks",
  source: "installed",
  definition: {
    properties: {
      title: { type: "string", required: true, description: "what to do" },
      dueAt: { type: "datetime" },
      priority: {
        type: "enum",
        values: [
          { value: "low", label: "Low" },
          { value: "high", label: "High" },
        ],
        description: "how much it matters",
      },
      status: {
        type: "state",
        states: ["proposed", "open", "done"],
        initial: "open",
      },
      pinned: { type: "bool" },
    },
    edges: {
      assignee: {
        to: "people.substrate.reamde.dev/person",
        description: "who is on it",
      },
    },
  },
}

const DOC = `kind: tasks.substrate.reamde.dev/task
metadata:
  id: t1
data:
  properties:
    title: write it
    priority: `

/** Complete at the end of `text`, the way a cursor at the end of a typed
 * document does. */
function at(text: string) {
  const lines = text.split("\n")
  const line = lines.length - 1
  return completionsAt(text, line, lines[line].length, taskKind)
}

function labels(text: string): string[] {
  return (at(text)?.options ?? []).map((o) => o.label)
}

describe("pathAt", () => {
  it("reads the mapping keys a line sits under", () => {
    const lines = DOC.split("\n")
    expect(pathAt(lines, 0)).toEqual([])
    expect(pathAt(lines, 2)).toEqual(["metadata"])
    expect(pathAt(lines, 4)).toEqual(["data"])
    expect(pathAt(lines, 5)).toEqual(["data", "properties"])
  })
})

describe("writtenProperties", () => {
  it("names what the properties block already writes", () => {
    expect([...writtenProperties(DOC.split("\n"))].sort()).toEqual([
      "priority",
      "title",
    ])
  })
})

describe("completionsAt", () => {
  it("offers the envelope's own keys at the top", () => {
    expect(labels("k")).toEqual(["kind", "metadata", "data"])
    expect(labels("metadata:\n  ")).toEqual(["id", "labels", "annotations"])
    expect(labels("data:\n  ")).toEqual(["properties", "edges"])
  })

  it("offers the declared properties inside the properties block", () => {
    const found = at("data:\n  properties:\n    t")
    expect(found?.options.map((o) => o.label)).toContain("title")
    const title = found?.options.find((o) => o.label === "title")
    expect(title?.detail).toContain("required")
    expect(title?.info).toBe("what to do")
    // A key completes with its colon, so the next keystroke is the value.
    expect(title?.apply).toBe("title: ")
    // The column it replaces from is where the partial word started.
    expect(found?.from).toBe(4)
  })

  it("never offers a property the document already writes", () => {
    expect(labels(`${DOC}\n    d`)).not.toContain("priority")
    expect(labels(`${DOC}\n    d`)).toContain("dueAt")
  })

  it("offers an enum's admitted values after its key", () => {
    expect(labels(DOC)).toEqual(["low", "high"])
    const found = at(DOC)
    expect(found?.options[0].detail).toBe("Low")
  })

  it("offers a state machine's states, marking the initial one", () => {
    const found = at("data:\n  properties:\n    status: ")
    expect(found?.options.map((o) => o.label)).toEqual([
      "proposed",
      "open",
      "done",
    ])
    expect(found?.options.find((o) => o.label === "open")?.detail).toBe("initial")
  })

  it("offers true/false for a bool, and a worked example otherwise", () => {
    expect(labels("data:\n  properties:\n    pinned: ")).toEqual(["true", "false"])
    expect(labels("data:\n  properties:\n    dueAt: ")).toEqual([
      "2026-01-31T09:00:00Z",
    ])
  })

  it("offers this collection's kind after the envelope's `kind:`", () => {
    expect(labels("kind: ")).toEqual(["tasks.substrate.reamde.dev/task"])
  })

  it("offers the declared edge rels after a `rel:`", () => {
    const found = at("data:\n  edges:\n    - rel: ")
    expect(found?.options.map((o) => o.label)).toEqual(["assignee"])
    expect(found?.options[0].detail).toBe("→ people.substrate.reamde.dev/person")
  })

  it("says nothing where nothing useful can be said", () => {
    // A value position on a free-text property with no example to give.
    expect(at("data:\n  properties:\n    title: ")).toBeNull()
    // Inside a block the envelope does not describe.
    expect(at("metadata:\n  labels:\n    ")).toBeNull()
  })
})
