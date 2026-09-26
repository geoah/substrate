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
  [
    "density",
    spec("density", "enum", {
      values: [{ value: "comfortable", label: "Comfortable" }],
    }),
  ],
  ["favorites", spec("favorites", "string", { repeated: true })],
  ["originDigest", spec("originDigest", "string", { managed: true })],
  ["syncToken", spec("syncToken", "string", { writer: "connector" })],
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
    const row = screen.getByText("Priority:").closest("[data-slot=value-move]")
    expect(row?.textContent).toBe("Priority:High→toUrgent")
  })

  it("says added and removed in words, and the arrow to a screen reader", () => {
    show([
      move({ name: "priority", before: "high", after: "urgent" }),
      move({
        name: "emails",
        before: ["old@example.com"],
        after: ["grace@example.com"],
        added: ["grace@example.com"],
        removed: ["old@example.com"],
      }),
    ])
    const to = screen.getByText("to")
    expect(to.className).toContain("sr-only")
    expect(to.previousElementSibling?.getAttribute("aria-hidden")).toBe("true")
    for (const words of ["added", "removed"]) {
      expect(screen.getByText(words).className).not.toContain("sr-only")
    }
    expect(document.body.textContent).not.toMatch(/[+−]/)
  })

  it("says a state's move with its badges", () => {
    show([move({ name: "status", before: "open", after: "done" })])
    const row = screen.getByText("Status:").closest("[data-slot=value-move]")
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
    expect(screen.getByText("added").nextSibling?.textContent).toBe(
      "grace@example.com"
    )
    expect(screen.getByText("removed").nextSibling?.textContent).toBe(
      "old@example.com"
    )
  })

  it("keeps a secret sealed on both sides", () => {
    show([move({ name: "apiKey", before: "<redacted>", after: "<redacted>" })])
    const row = screen.getByText("API key:").closest("[data-slot=value-move]")
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
    const key = screen.getByText("API key:").closest("[data-slot=value-move]")
    expect(key?.textContent).toBe("API key:replaced")
    const priority = screen
      .getByText("Priority:")
      .closest("[data-slot=value-move]")
    expect(priority?.textContent).toBe("Priority:changed and changed back")
  })

  it("cuts long text to a line and keeps the whole in the hover", () => {
    const long = "Numbers from finance first, ".repeat(6).trim()
    show([move({ name: "description", after: long, beforeUnknown: true })])
    const cut = screen.getByTitle(long)
    expect(cut.textContent?.endsWith("…")).toBe(true)
  })

  it("says a cleared value, and an unknown before by where it landed", () => {
    show([
      move({ name: "description", before: "Draft" }),
      move({ name: "priority", after: "urgent", beforeUnknown: true }),
    ])
    expect(screen.getByText("cleared").nextSibling?.textContent).toBe("Draft")
    const row = screen.getByText("Priority:").closest("[data-slot=value-move]")
    expect(row?.textContent).toBe("Priority:set toUrgent")
  })

  it("shows keys and raw values in technical mode", () => {
    show([move({ name: "priority", before: "high", after: "urgent" })], true)
    const row = screen.getByText("priority:").closest("[data-slot=value-move]")
    expect(row?.textContent).toBe("priority:high→tourgent")
  })

  it("says a value set where there was none", () => {
    show([move({ name: "density", after: "comfortable" })])
    const row = screen.getByText("Density:").closest("[data-slot=value-move]")
    expect(row?.textContent).toBe("Density:set toComfortable")
  })

  it("leaves out what the host writes and moves with nothing to say", () => {
    const moves = [
      move({ name: "originDigest", before: "82007523", after: "075e3623" }),
      move({ name: "syncToken", before: "a", after: "b" }),
      move({ name: "favorites", after: [] }),
      move({ name: "density", after: "comfortable" }),
    ]
    show(moves)
    expect(screen.queryByText("Origin digest:")).toBeNull()
    expect(screen.queryByText(/syncToken|Sync token/)).toBeNull()
    expect(screen.queryByText("Favorites:")).toBeNull()
    expect(screen.getByText("Density:")).toBeTruthy()
    cleanup()
    show(moves, true)
    expect(screen.getByText("originDigest:")).toBeTruthy()
    expect(screen.getByText("syncToken:")).toBeTruthy()
  })
})
