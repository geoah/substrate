// @vitest-environment jsdom
/** A change says its values the way the property sheet shows them: an enum
 * by its label, a state as its badge, a list as what it gained and lost, a
 * secret sealed on both sides, long text on one line with the whole in the
 * hover; technical mode says the keys and the raw values. */

import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { ValueMoves } from "./value-moves"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import type { ValueMove } from "@/lib/change-values"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import type { PropSpec } from "@/lib/record-schema"

afterEach(cleanup)

function preferences(
  technicalDetails: boolean
): ConsolePreferencesContextValue {
  return {
    preferences: {
      collapsed: [],
      favorites: [],
      sidebarOpen: true,
      ...DEFAULT_SETTINGS,
      technicalDetails,
    },
    busy: false,
    change: () => {},
    set: () => {},
  }
}

function spec(
  name: string,
  kind: string,
  extra: Partial<PropSpec> = {}
): PropSpec {
  return {
    name,
    label: extra.label ?? name[0].toUpperCase() + name.slice(1),
    kind,
    required: false,
    repeated: false,
    keyed: false,
    managed: false,
    ...extra,
  }
}

const SPECS = new Map<string, PropSpec>([
  [
    "priority",
    spec("priority", "enum", {
      values: [
        { value: "high", label: "High" },
        { value: "urgent", label: "Urgent" },
      ],
    }),
  ],
  [
    "status",
    spec("status", "state", { states: ["open", "done"], initial: "open" }),
  ],
  ["emails", spec("emails", "email", { repeated: true })],
  ["description", spec("description", "markdown")],
  ["apiKey", spec("apiKey", "secret", { label: "API key" })],
])

function show(moves: ValueMove[], technical = false) {
  return render(
    <ConsolePreferencesContext.Provider value={preferences(technical)}>
      <ValueMoves moves={moves} specs={SPECS} />
    </ConsolePreferencesContext.Provider>
  )
}

function move(m: Partial<ValueMove> & { name: string }): ValueMove {
  return { beforeUnknown: false, ...m }
}

describe("ValueMoves", () => {
  it("says an enum's move by its labels", () => {
    show([move({ name: "priority", before: "high", after: "urgent" })])
    const row = screen.getByText("Priority").closest("[data-slot=value-move]")
    expect(row?.textContent).toBe("PriorityHigh→Urgent")
  })

  it("says a state's move with its badges", () => {
    show([move({ name: "status", before: "open", after: "done" })])
    const row = screen.getByText("Status").closest("[data-slot=value-move]")
    expect(row?.textContent).toContain("Open")
    expect(row?.textContent).toContain("Done")
  })

  it("says a list as what it gained and lost", () => {
    show([
      move({
        name: "emails",
        before: ["old@example.com"],
        after: ["grace@example.com"],
        added: ["grace@example.com"],
        removed: ["old@example.com"],
      }),
    ])
    expect(screen.getByLabelText("added").nextSibling?.textContent).toBe(
      "grace@example.com"
    )
    expect(screen.getByLabelText("removed").nextSibling?.textContent).toBe(
      "old@example.com"
    )
  })

  it("keeps a secret sealed on both sides", () => {
    show([move({ name: "apiKey", before: "<redacted>", after: "<redacted>" })])
    const row = screen.getByText("API key").closest("[data-slot=value-move]")
    expect(row?.textContent).not.toContain("<redacted>")
    expect(row?.textContent?.match(/sealed/g)).toHaveLength(2)
  })

  it("says a replaced secret and a value changed back in words", () => {
    show([
      move({
        name: "apiKey",
        before: "<redacted>",
        after: "<redacted>",
        replaced: true,
      }),
      move({
        name: "priority",
        before: "high",
        after: "high",
        changedBack: true,
      }),
    ])
    const key = screen.getByText("API key").closest("[data-slot=value-move]")
    expect(key?.textContent).toBe("API keyreplaced")
    const priority = screen
      .getByText("Priority")
      .closest("[data-slot=value-move]")
    expect(priority?.textContent).toBe("Prioritychanged and changed back")
  })

  it("cuts long text to a line and keeps the whole in the hover", () => {
    const long = "Numbers from finance first, ".repeat(6).trim()
    show([move({ name: "description", after: long, beforeUnknown: true })])
    const cut = screen.getByTitle(long)
    expect(cut.textContent?.endsWith("…")).toBe(true)
  })

  it("says a cleared value as removed, and an unknown before by where it landed", () => {
    show([
      move({ name: "description", before: "Draft" }),
      move({ name: "priority", after: "urgent", beforeUnknown: true }),
    ])
    expect(screen.getByLabelText("removed").nextSibling?.textContent).toBe(
      "Draft"
    )
    const row = screen.getByText("Priority").closest("[data-slot=value-move]")
    expect(row?.textContent).toBe("Priority→Urgent")
  })

  it("shows keys and raw values in technical mode", () => {
    show([move({ name: "priority", before: "high", after: "urgent" })], true)
    const row = screen.getByText("priority").closest("[data-slot=value-move]")
    expect(row?.textContent).toBe("priorityhigh→urgent")
  })
})
