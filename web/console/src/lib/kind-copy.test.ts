import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  EVERYDAY_MAX,
  everydayDescription,
  firstClause,
  kindDescription,
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

// Real descriptions from samples/ and kinds/providers.substrate.reamde.dev/.
const TASK =
  "Something to do, however it arrived: typed by hand, projected from a Linear issue, or proposed by an agent. Status carries the whole lifecycle."
const PROJECT =
  "What tasks group under: a name, a lifecycle and a summary, with the tasks pointing at it."
const EVENT =
  "One concrete occurrence on the timeline — title, location, prose, the join link, and who is coming."
const SERIES =
  "The recurring DEFINITION behind repeating events: its RRULE, the dates that rule skips and the first occurrence it counts from."
const TASKLOG =
  "The done or skipped mark for one occurrence of a repeating task. The task stays open while it keeps repeating."
const LINEAR_STATE =
  "One Linear workflow state - an issue status - from `workflowStates` (introspected 2026-09-16); `at` is `createdAt`."
const GOOGLE_EVENT =
  "One Google Calendar API v3 Event WITHOUT `recurrence` (`events.list`, singleEvents=false), whole: a single event."

describe("firstClause", () => {
  it.each([
    [TASK, "Something to do, however it arrived."],
    [PROJECT, "What tasks group under."],
    [EVENT, "One concrete occurrence on the timeline."],
    [SERIES, "The recurring definition behind repeating events."],
    [
      TASKLOG,
      "The done or skipped mark for one occurrence of a repeating task.",
    ],
    [LINEAR_STATE, "One Linear workflow state."],
  ])("%s", (text, clause) => {
    expect(firstClause(text)).toBe(clause)
  })

  it("cuts a long clause at a word, with an ellipsis", () => {
    const long = `A ${"very ".repeat(40)}long clause`
    const out = firstClause(long)
    expect(out.length).toBeLessThanOrEqual(EVERYDAY_MAX + 1)
    expect(out.endsWith("very…")).toBe(true)
  })

  it("keeps an acronym in capitals", () => {
    expect(firstClause("An HTML page, saved")).toBe("An HTML page, saved.")
  })
})

describe("everydayDescription", () => {
  it("says what a provider's kind is for, not what its API calls it", () => {
    expect(
      everydayDescription(
        kind(
          "providers.substrate.reamde.dev/google/calendarevent",
          GOOGLE_EVENT
        )
      )
    ).toBe("Your Google calendar events, copied in and kept up to date.")
    expect(
      everydayDescription("providers.substrate.reamde.dev/google/gmailthread")
    ).toBe("Your Google Gmail threads, copied in and kept up to date.")
    expect(
      everydayDescription("providers.substrate.reamde.dev/google/drivefile")
    ).toBe("Your Google Drive files, copied in and kept up to date.")
  })

  it("reads a sample kind's first clause", () => {
    expect(
      everydayDescription(kind("samples.substrate.reamde.dev/tasks/task", TASK))
    ).toBe("Something to do, however it arrived.")
  })

  it("is undefined without a description", () => {
    expect(everydayDescription("ada.example.com/x/y")).toBeUndefined()
  })

  it("takes a catalog closure's description for a kind not held yet", () => {
    expect(everydayDescription("ada.example.com/tasks/project", PROJECT)).toBe(
      "What tasks group under."
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
      "Your Google calendar events, copied in and kept up to date."
    )
    expect(kindDescription(kind("a.example.com/p/k", ""), true)).toBeUndefined()
  })
})
