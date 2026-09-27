import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  EVERYDAY_MAX,
  everydayDescription,
  firstClause,
  firstSentence,
  kindDescription,
  plainSentence,
} from "./kind-copy"

function kind(identity: string, description: string): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "builtin",
    description,
    definition: {},
  }
}

// Real descriptions from samples/ and kinds/providers.substrate.reamde.dev/,
// cut short where the tail does not matter to the case.
const TASK =
  "Something you need to do. It can arrive any way: typed by hand, projected from a Linear issue, or proposed by an agent."
const PROJECT =
  "A project your tasks belong to. What tasks group under: a name, a lifecycle and a summary, with the tasks pointing at it."
const SERIES =
  "An event that repeats, such as a weekly meeting. The recurring DEFINITION behind repeating events: its RRULE, the dates that rule skips and the first occurrence it counts from."
const TASKLOG =
  "The done or skipped mark for one occurrence of a repeating task. The task stays open while it keeps repeating."
const LINEAR_STATE =
  "A status a Linear issue can be in, such as Todo or Done. One Linear workflow state - an issue status - from `workflowStates` (introspected 2026-09-16); `at` is `createdAt`."
const GOOGLE_EVENT =
  "A single event on your Google calendar. A Calendar API v3 Event WITHOUT `recurrence` (`events.list`, singleEvents=false), whole: a single event."
const WHOOP_CYCLE =
  "One day on WHOOP, from waking up to waking up, and the strain it scored. A WHOOP physiological cycle."
// What the provider declarations said before they opened with a plain
// sentence; an installed copy that has not upgraded still reads this way.
const OLD_GOOGLE_EVENT =
  "One Google Calendar API v3 Event WITHOUT `recurrence` (`events.list`, singleEvents=false), whole: a single event."
const OLD_LINEAR_STATE =
  "One Linear workflow state - an issue status - from `workflowStates` (introspected 2026-09-16); `at` is `createdAt`."
const OLD_SERIES =
  "The recurring DEFINITION behind repeating events: its RRULE, the dates that rule skips and the first occurrence it counts from."

describe("firstSentence", () => {
  it.each([
    [TASK, "Something you need to do."],
    [GOOGLE_EVENT, "A single event on your Google calendar."],
    [
      TASKLOG,
      "The done or skipped mark for one occurrence of a repeating task.",
    ],
    ["No full stop at all", "No full stop at all"],
    ["  Wrapped\n  across   lines. Then more.", "Wrapped across lines."],
  ])("%s", (text, sentence) => {
    expect(firstSentence(text)).toBe(sentence)
  })
})

describe("plainSentence", () => {
  it.each([
    "Something you need to do.",
    "A Linear cycle: a fixed stretch of time a team plans its work in.",
    "One day on WHOOP, from waking up to waking up, and the strain it scored.",
    "The API key and address your Linear workspace connects through.",
  ])("reads %s as plain", (sentence) => {
    expect(plainSentence(sentence)).toBe(true)
  })

  it.each([
    firstSentence(OLD_GOOGLE_EVENT),
    firstSentence(OLD_LINEAR_STATE),
    "One Gmail Message, Gmail API v1, from users.messages.get?format=FULL.",
    "One Notion user as GET /v1/users returns it.",
    "The recurring DEFINITION behind repeating events.",
    "",
    `A ${"very ".repeat(30)}long sentence.`,
  ])("reads %s as an API note", (sentence) => {
    expect(plainSentence(sentence)).toBe(false)
  })
})

describe("firstClause", () => {
  it.each([
    [OLD_SERIES, "The recurring definition behind repeating events."],
    [
      TASKLOG,
      "The done or skipped mark for one occurrence of a repeating task.",
    ],
    [OLD_LINEAR_STATE, "One Linear workflow state."],
  ])("%s", (text, clause) => {
    expect(firstClause(text)).toBe(clause)
  })

  it("cuts a long clause at a word, with an ellipsis", () => {
    const long = `A ${"very ".repeat(40)}long clause`
    const out = firstClause(long)
    expect(out.length).toBeLessThanOrEqual(EVERYDAY_MAX + 1)
    expect(out.endsWith("very…")).toBe(true)
  })

  it("keeps an acronym and a name spelled in capitals", () => {
    expect(firstClause("An HTML page, saved")).toBe("An HTML page, saved.")
    expect(firstClause("One WHOOP cycle: a day")).toBe("One WHOOP cycle.")
  })
})

describe("everydayDescription", () => {
  it("reads a provider kind's own first sentence", () => {
    expect(
      everydayDescription(
        kind(
          "providers.substrate.reamde.dev/google/calendarevent",
          GOOGLE_EVENT
        )
      )
    ).toBe("A single event on your Google calendar.")
    expect(
      everydayDescription(
        kind(
          "providers.substrate.reamde.dev/linear/workflowstate",
          LINEAR_STATE
        )
      )
    ).toBe("A status a Linear issue can be in, such as Todo or Done.")
    expect(
      everydayDescription(
        kind("providers.substrate.reamde.dev/whoop/cycle", WHOOP_CYCLE)
      )
    ).toBe(
      "One day on WHOOP, from waking up to waking up, and the strain it scored."
    )
  })

  it("says what a provider's kind is for when its declaration opens with API notes", () => {
    expect(
      everydayDescription(
        kind(
          "providers.substrate.reamde.dev/google/calendarevent",
          OLD_GOOGLE_EVENT
        )
      )
    ).toBe("Your Google calendar events, copied in and kept up to date.")
    expect(
      everydayDescription("providers.substrate.reamde.dev/google/gmailthread")
    ).toBe("Your Google Gmail threads, copied in and kept up to date.")
    expect(
      everydayDescription("providers.substrate.reamde.dev/whoop/cycle")
    ).toBe("Your WHOOP cycles, copied in and kept up to date.")
  })

  it("reads a sample kind's first sentence", () => {
    expect(
      everydayDescription(kind("samples.substrate.reamde.dev/tasks/task", TASK))
    ).toBe("Something you need to do.")
    expect(
      everydayDescription(
        kind(
          "samples.substrate.reamde.dev/calendar/calendareventseries",
          SERIES
        )
      )
    ).toBe("An event that repeats, such as a weekly meeting.")
  })

  it("falls back to the first clause for any other kind that opens with notes", () => {
    expect(
      everydayDescription(kind("ada.example.com/calendar/series", OLD_SERIES))
    ).toBe("The recurring definition behind repeating events.")
  })

  it("is undefined without a description", () => {
    expect(everydayDescription("ada.example.com/x/y")).toBeUndefined()
  })

  it("takes a catalog closure's description for a kind not held yet", () => {
    expect(everydayDescription("ada.example.com/tasks/project", PROJECT)).toBe(
      "A project your tasks belong to."
    )
  })
})

describe("kindDescription", () => {
  it("is the declaration's own words with technical details on", () => {
    const k = kind(
      "providers.substrate.reamde.dev/google/calendarevent",
      GOOGLE_EVENT
    )
    expect(kindDescription(k, true)).toBe(GOOGLE_EVENT)
    expect(kindDescription(k, false)).toBe(
      "A single event on your Google calendar."
    )
    expect(kindDescription(kind("a.example.com/p/k", ""), true)).toBeUndefined()
  })
})
