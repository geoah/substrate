import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord, TriggerStatus } from "@/lib/api/types"
import {
  andList,
  bundleParams,
  choiceSentence,
  chosenCount,
  connectionWords,
  currentStep,
  enumLabel,
  fillsIn,
  firstSentence,
  providerStanding,
  providerTools,
  recurrenceWords,
  removalLadder,
  setupSteps,
  syncWords,
  toolActivity,
  triggerCadence,
  triggerCallable,
  type StandingInput,
  type StepFacts,
} from "@/lib/providers"
import { syncFieldsOf } from "@/lib/sync"

function record(
  id: string,
  properties: Record<string, unknown>,
  kind = "providers.substrate.reamde.dev/google/account"
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  }
}

const FACTS: StepFacts = {
  installed: true,
  needsCredentials: true,
  configured: false,
  hasAccountKind: true,
  accounts: [],
}

function states(f: StepFacts) {
  return setupSteps(f).map((s) => `${s.key}:${s.state}`)
}

describe("setupSteps", () => {
  it("makes adding the provider the current step until it is here", () => {
    expect(states({ ...FACTS, installed: false })).toEqual([
      "add:now",
      "credentials:todo",
      "account:todo",
      "choose:todo",
    ])
  })

  it("asks for the sign-in details next", () => {
    expect(states(FACTS)).toEqual([
      "add:done",
      "credentials:now",
      "account:todo",
      "choose:todo",
    ])
    expect(currentStep(setupSteps(FACTS))?.n).toBe(2)
  })

  it("asks for an account once the details are saved", () => {
    expect(states({ ...FACTS, configured: true })).toEqual([
      "add:done",
      "credentials:done",
      "account:now",
      "choose:todo",
    ])
  })

  it("keeps connecting current while the only account waits for approval", () => {
    expect(
      states({
        ...FACTS,
        configured: true,
        accounts: [{ connected: false, chosen: 2 }],
      })
    ).toEqual(["add:done", "credentials:done", "account:now", "choose:done"])
  })

  it("asks what to bring in when a connected account has nothing on", () => {
    expect(
      states({
        ...FACTS,
        configured: true,
        accounts: [{ connected: true, chosen: 0 }],
      })
    ).toEqual(["add:done", "credentials:done", "account:done", "choose:now"])
  })

  it("is done with a connected account that brings something in", () => {
    const steps = setupSteps({
      ...FACTS,
      configured: true,
      accounts: [{ connected: true, chosen: 1 }],
    })
    expect(currentStep(steps)).toBeUndefined()
  })

  it("needs no details and no account where the provider declares neither", () => {
    expect(
      states({ ...FACTS, needsCredentials: false, hasAccountKind: false })
    ).toEqual(["add:done", "credentials:done", "account:done", "choose:done"])
  })

  it("treats an account kind without switches as nothing to choose", () => {
    expect(
      states({
        ...FACTS,
        configured: true,
        accounts: [{ connected: true, chosen: undefined }],
      })
    ).toEqual(["add:done", "credentials:done", "account:done", "choose:done"])
  })
})

describe("chosenCount", () => {
  it("counts the switches that are on", () => {
    const r = record("a", { enabledGmail: true, enabledDrive: false })
    expect(chosenCount(r, ["enabledGmail", "enabledDrive"])).toBe(1)
    expect(chosenCount(r, [])).toBeUndefined()
  })
})

describe("providerStanding", () => {
  const base: StandingInput = {
    installed: true,
    enabled: true,
    quarantined: false,
    upgradeBlocked: false,
    otherSetup: 0,
    steps: setupSteps({
      ...FACTS,
      configured: true,
      accounts: [{ connected: true, chosen: 1 }],
    }),
    accounts: [
      {
        label: "ada@example.com",
        health: "healthy",
        sync: syncFieldsOf({ syncState: "ok" }),
      },
    ],
  }

  it("is On and up to date when every step is done", () => {
    expect(providerStanding(base)).toEqual({
      tone: "on",
      pill: "On",
      line: "Connected · up to date",
    })
  })

  it("says it is syncing while a sync runs", () => {
    expect(
      providerStanding({
        ...base,
        accounts: [
          { ...base.accounts[0], sync: syncFieldsOf({ syncState: "running" }) },
        ],
      }).line
    ).toBe("Connected · syncing")
  })

  it("offers to add a provider that is not here", () => {
    expect(providerStanding({ ...base, installed: false })).toEqual({
      tone: "add",
      pill: "",
      line: "Not added",
    })
  })

  it("names the step a provider is on", () => {
    const s = providerStanding({ ...base, steps: setupSteps(FACTS) })
    expect(s).toMatchObject({
      tone: "setup",
      pill: "Set up",
      step: 2,
      line: "Almost there · step 2 of 4",
    })
  })

  it("puts a broken account before anything else but a failed load", () => {
    const broken = {
      ...base,
      steps: setupSteps(FACTS),
      accounts: [{ ...base.accounts[0], health: "broken" as const }],
    }
    expect(providerStanding(broken)).toMatchObject({
      tone: "attention",
      pill: "Needs attention",
      line: "Having trouble with ada@example.com",
    })
    expect(
      providerStanding({ ...broken, quarantined: true, installed: false })
    ).toMatchObject({
      tone: "attention",
      line: expect.stringMatching(/failed to load/),
    })
  })

  it("says a blocked update needs attention", () => {
    expect(providerStanding({ ...base, upgradeBlocked: true }).tone).toBe(
      "attention"
    )
  })

  it("is Paused while disabled", () => {
    expect(providerStanding({ ...base, enabled: false })).toMatchObject({
      tone: "paused",
      pill: "Paused",
    })
  })

  it("stays in set-up while a setting is empty", () => {
    expect(providerStanding({ ...base, otherSetup: 1 })).toMatchObject({
      tone: "setup",
      line: "Almost there · finish its settings",
    })
  })
})

describe("syncWords", () => {
  const words = (p: Record<string, unknown>) =>
    syncWords(syncFieldsOf(p), "Google").text
  it("says each of the trait's states in plain words", () => {
    expect(words({})).toBe("Not synced yet")
    expect(words({ syncState: "running" })).toBe("Syncing…")
    expect(words({ syncState: "ok" })).toBe("Up to date")
    expect(words({ syncState: "throttled" })).toBe("Slowed down by Google")
    expect(words({ syncState: "ok", syncPaused: true })).toBe("Paused")
  })
  it("quotes the first line of the error", () => {
    expect(
      words({ syncState: "erroring", syncError: "gmail: HTTP 403\ntrace" })
    ).toBe("Having trouble: gmail: HTTP 403")
    expect(words({ syncState: "erroring" })).toBe("Having trouble")
  })
})

describe("connectionWords", () => {
  it("says where an account's approval stands", () => {
    expect(connectionWords("connected", "Google").text).toBe("Connected")
    expect(connectionWords("pending", "Google").text).toBe(
      "Waiting for you to approve it at Google"
    )
    expect(connectionWords(undefined, "Google").text).toBe("Not connected yet")
    expect(connectionWords("erroring", "Google").tone).toBe("bad")
  })
})

describe("enumLabel", () => {
  const kind = {
    definition: {
      properties: {
        syncFrequency: {
          type: "enum",
          values: [{ value: "hourly", label: "Every hour" }, "daily"],
        },
      },
    },
  } as unknown as KindInfo
  it("reads the label an enum declares for a value", () => {
    expect(enumLabel(kind, "syncFrequency", "hourly")).toBe("Every hour")
    expect(enumLabel(kind, "syncFrequency", "daily")).toBe("daily")
    expect(enumLabel(kind, "syncFrequency", undefined)).toBeUndefined()
  })
})

describe("choiceSentence", () => {
  const toggles = [
    { name: "enabledCalendar", label: "Calendar" },
    { name: "enabledContacts", label: "Contacts" },
    { name: "enabledGmail", label: "Gmail" },
    { name: "enabledDrive", label: "Drive" },
  ]
  it("says what is on, then what is off", () => {
    expect(
      choiceSentence(
        record("a", { enabledCalendar: true, enabledContacts: true }),
        toggles
      )
    ).toBe("Calendar and Contacts. Gmail and Drive are off.")
    expect(
      choiceSentence(
        record("a", {
          enabledCalendar: true,
          enabledContacts: true,
          enabledGmail: true,
        }),
        toggles
      )
    ).toBe("Calendar, Contacts and Gmail. Drive is off.")
    expect(choiceSentence(record("a", {}), toggles)).toBe(
      "Nothing is turned on yet."
    )
  })
})

describe("tools", () => {
  function trigger(id: string, source: Record<string, unknown>) {
    return record(
      id,
      {
        source,
        callable: {
          ref: "substrate.reamde.dev/core/function/providers.substrate.reamde.dev/google/synccalendar",
        },
      },
      "substrate.reamde.dev/core/trigger"
    )
  }
  const TRIGGERS = [
    trigger("google-calendar-scheduled", {
      schedule: { recurrence: "FREQ=HOURLY" },
    }),
    trigger("google-calendar-on-request", {
      record: { kinds: ["x"], when: "record.properties.syncRequestedAt != ''" },
    }),
    trigger("google-calendar-on-connect", { record: { kinds: ["x"] } }),
  ]

  it("says how often a schedule runs", () => {
    expect(recurrenceWords("FREQ=HOURLY")).toBe("Every hour")
    expect(recurrenceWords("RRULE:FREQ=MINUTELY;INTERVAL=15")).toBe(
      "Every 15 minutes"
    )
    expect(recurrenceWords("nonsense")).toBe("On a schedule")
  })

  it("says when each trigger runs its tool", () => {
    expect(TRIGGERS.map(triggerCadence)).toEqual([
      "Every hour",
      "When you press Sync now",
      "When an account connects",
    ])
    expect(triggerCallable(TRIGGERS[0])).toBe(
      "providers.substrate.reamde.dev/google/synccalendar"
    )
  })

  it("joins a provider's functions with the triggers that run them", () => {
    const [tool] = providerTools(
      ["providers.substrate.reamde.dev/google/synccalendar"],
      { "providers.substrate.reamde.dev/google/synccalendar": "Syncs." },
      TRIGGERS
    )
    expect(tool).toEqual({
      reference: "providers.substrate.reamde.dev/google/synccalendar",
      name: "Google Calendar sync",
      description: "Syncs.",
      cadences: [
        "Every hour",
        "When you press Sync now",
        "When an account connects",
      ],
      triggerIds: TRIGGERS.map((t) => t.id),
    })
  })

  it("reads a tool's last run and its parked deliveries off its triggers", () => {
    const status = (
      id: string,
      lastFire: string | undefined,
      parked: number
    ): TriggerStatus => ({
      id,
      kind: "schedule",
      callable: "",
      enabled: true,
      head: 0,
      parked,
      pending: 0,
      lastFire,
    })
    expect(
      toolActivity(
        ["a", "b"],
        [
          status("a", "2026-09-01T10:00:00Z", 0),
          status("b", "2026-09-02T10:00:00Z", 2),
          status("c", "2026-09-03T10:00:00Z", 5),
        ]
      )
    ).toEqual({ lastFire: "2026-09-02T10:00:00Z", parked: 2 })
  })
})

describe("fillsIn", () => {
  it("names the kinds a provider kind's records are mapped onto", () => {
    const mapping = record(
      "providers.localhost/people/googlecontactperson",
      {
        from: {
          ref: "substrate.reamde.dev/core/kind/providers.substrate.reamde.dev/google/contact",
        },
        to: {
          ref: "substrate.reamde.dev/core/kind/providers.localhost/people/person",
        },
      },
      "substrate.reamde.dev/core/recordmapping"
    )
    expect(
      fillsIn([mapping], "providers.substrate.reamde.dev/google/contact")
    ).toEqual(["providers.localhost/people/person"])
    expect(
      fillsIn([mapping], "providers.substrate.reamde.dev/google/drivefile")
    ).toEqual([])
  })
})

describe("removalLadder", () => {
  it("climbs the rungs the server enforces, from where the bundle stands", () => {
    expect(
      removalLadder({ installed: true, enabled: true, liveRecords: 3 })
    ).toEqual(["disable", "purge", "uninstall"])
    expect(
      removalLadder({ installed: true, enabled: false, liveRecords: 0 })
    ).toEqual(["uninstall"])
    // A quarantined bundle has no registration to remove, only data.
    expect(
      removalLadder({ installed: false, enabled: false, liveRecords: 2 })
    ).toEqual(["purge"])
  })
})

describe("words and ids", () => {
  it("splits a bundle id into its route params", () => {
    expect(bundleParams("providers.substrate.reamde.dev/google")).toEqual({
      authority: "providers.substrate.reamde.dev",
      pkg: "google",
    })
  })
  it("keeps a description's first sentence", () => {
    expect(firstSentence("Keeps a copy. Read-only.")).toBe("Keeps a copy.")
    expect(firstSentence("No stop")).toBe("No stop")
  })
  it("joins a list the way a sentence does", () => {
    expect(andList(["a"])).toBe("a")
    expect(andList(["a", "b", "c"])).toBe("a, b and c")
  })
})
