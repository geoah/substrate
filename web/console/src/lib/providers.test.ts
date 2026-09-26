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
  lastSyncOf,
  providerActors,
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
import { FAILED_PREVIEW_BLOCKER } from "@/lib/bundles"
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
  const ada = {
    record: { id: "ada" },
    label: "ada@example.com",
    tokenStatus: "connected",
    sync: syncFieldsOf({ syncState: "ok" }),
  }
  const base: StandingInput = {
    installed: true,
    enabled: true,
    quarantined: false,
    upgradeBlockers: [],
    otherSetup: [],
    steps: setupSteps({
      ...FACTS,
      configured: true,
      accounts: [{ connected: true, chosen: 1 }],
    }),
    oauth: true,
    accounts: [ada],
  }

  it("is On and up to date when every step is done", () => {
    expect(providerStanding(base)).toEqual({
      tone: "on",
      pill: "On",
      line: "Connected · up to date",
      problems: [],
    })
  })

  it("says it is syncing while a sync runs", () => {
    expect(
      providerStanding({
        ...base,
        accounts: [{ ...ada, sync: syncFieldsOf({ syncState: "running" }) }],
      }).line
    ).toBe("Connected · syncing")
  })

  it("offers to add a provider that is not here", () => {
    expect(providerStanding({ ...base, installed: false })).toEqual({
      tone: "add",
      pill: "",
      line: "Not added",
      problems: [],
    })
  })

  it("names the step a provider is on", () => {
    const s = providerStanding({
      ...base,
      accounts: [],
      steps: setupSteps(FACTS),
    })
    expect(s).toMatchObject({
      tone: "setup",
      pill: "Set up",
      step: 2,
      line: "Almost there · step 2 of 4",
      problems: [],
    })
  })

  it("says which account fails to sync, with the error and the fixes", () => {
    const s = providerStanding({
      ...base,
      accounts: [
        {
          ...ada,
          sync: syncFieldsOf({
            syncState: "erroring",
            syncError: "gmail: HTTP 403\ntrace",
          }),
        },
      ],
    })
    expect(s).toMatchObject({
      tone: "attention",
      pill: "Needs attention",
      line: "Syncing ada@example.com is failing",
    })
    expect(s.problems).toEqual([
      {
        code: "sync",
        summary: "Syncing ada@example.com is failing",
        detail: ["gmail: HTTP 403"],
        account: "ada",
        fixes: ["sync-now", "reconnect"],
      },
    ])
  })

  it("reads a legacy erroring status as a failing sync", () => {
    const s = providerStanding({
      ...base,
      oauth: false,
      accounts: [
        {
          ...ada,
          sync: syncFieldsOf({}),
          legacySyncStatus: "erroring: token expired",
        },
      ],
    })
    expect(s.problems[0]).toMatchObject({
      code: "sync",
      detail: ["erroring: token expired"],
      fixes: ["sync-now"],
    })
  })

  it("asks to reconnect an account whose sign-in stopped working", () => {
    const s = providerStanding({
      ...base,
      accounts: [
        {
          ...ada,
          tokenStatus: "erroring",
          sync: syncFieldsOf({ syncState: "erroring", syncError: "401" }),
        },
      ],
    })
    expect(s.problems).toEqual([
      {
        code: "sign-in",
        summary: "The sign-in for ada@example.com stopped working",
        detail: ["401"],
        account: "ada",
        fixes: ["reconnect"],
      },
    ])
  })

  it("says why it failed to load, before anything else", () => {
    const s = providerStanding({
      ...base,
      quarantined: true,
      installed: false,
      quarantineReason: "kind google/contact: unknown key",
      accounts: [{ ...ada, tokenStatus: "erroring" }],
    })
    expect(s).toMatchObject({ tone: "attention", line: "It failed to load" })
    expect(s.problems).toEqual([
      {
        code: "failed-to-load",
        summary: "It failed to load",
        detail: ["kind google/contact: unknown key"],
        fixes: ["add-again"],
      },
    ])
  })

  it("names what blocks an update", () => {
    const s = providerStanding({
      ...base,
      upgradeBlockers: ["google/contact: 3 records still hold nickname"],
    })
    expect(s.line).toBe("An update is waiting on your records")
    expect(s.problems[0].detail).toEqual([
      "google/contact: 3 records still hold nickname",
    ])
    expect(
      providerStanding({ ...base, upgradeBlockers: [FAILED_PREVIEW_BLOCKER] })
        .problems[0].summary
    ).toBe("An update couldn’t be checked")
  })

  it("counts the parked runs of its triggers and offers to retry them", () => {
    const s = providerStanding({
      ...base,
      triggers: [
        { id: "google-gmail-scheduled", parked: 2 },
        { id: "google-drive-scheduled", parked: 1 },
        { id: "google-contacts-scheduled", parked: 0 },
      ],
    })
    expect(s.line).toBe("3 runs failed and are waiting to be retried")
    expect(s.problems[0].fixes).toEqual(["retry-parked"])
  })

  it("names a trigger that cannot run", () => {
    const s = providerStanding({
      ...base,
      triggers: [
        { id: "google-gmail-scheduled", parked: 0, error: "no such function" },
      ],
    })
    expect(s.problems[0]).toMatchObject({
      code: "trigger-broken",
      summary: "One of its syncs can’t run",
      detail: ["google-gmail-scheduled: no such function"],
    })
  })

  it("calls a setup gap a problem once accounts depend on it", () => {
    const gap = {
      ...base,
      otherSetup: [{ code: "setting" as const, message: "apiBase is empty" }],
    }
    expect(providerStanding(gap).problems).toEqual([
      {
        code: "setup-missing",
        summary: "A setting it needs is empty",
        detail: ["apiBase is empty"],
        fixes: ["set-up"],
      },
    ])
    expect(
      providerStanding({ ...gap, steps: setupSteps(FACTS) }).problems[0]
    ).toMatchObject({ summary: "Its sign-in details are missing" })
    // Without accounts it is still the set-up the steps walk through.
    expect(providerStanding({ ...gap, accounts: [] })).toMatchObject({
      tone: "setup",
      line: "Almost there · finish its settings",
    })
  })

  it("adds how many more problems there are to the card's line", () => {
    const s = providerStanding({
      ...base,
      upgradeBlockers: ["x"],
      triggers: [{ id: "t", parked: 1 }],
    })
    expect(s.line).toBe(
      "1 run failed and is waiting to be retried · and 1 more"
    )
  })

  it("is Paused while disabled", () => {
    expect(providerStanding({ ...base, enabled: false })).toMatchObject({
      tone: "paused",
      pill: "Paused",
    })
  })

  it("gives every Needs attention a reason and a line that says it", () => {
    const accounts = {
      none: [],
      healthy: [ada],
      pending: [{ ...ada, tokenStatus: "pending" }],
      signIn: [{ ...ada, tokenStatus: "erroring" }],
      sync: [{ ...ada, sync: syncFieldsOf({ syncState: "erroring" }) }],
      legacy: [
        { ...ada, sync: syncFieldsOf({}), legacySyncStatus: "erroring" },
      ],
    }
    const triggers = {
      none: [],
      parked: [{ id: "t", parked: 2 }],
      broken: [{ id: "t", parked: 0, error: "no callable" }],
    }
    const flags = [true, false]
    let attention = 0
    for (const quarantined of flags)
      for (const installed of flags)
        for (const enabled of flags)
          for (const blockers of [[], ["x"], [FAILED_PREVIEW_BLOCKER]])
            for (const otherSetup of [
              [],
              [{ code: "setting" as const, message: "m" }],
            ])
              for (const steps of [base.steps, setupSteps(FACTS)])
                for (const oauth of flags)
                  for (const [name, list] of Object.entries(accounts))
                    for (const t of Object.values(triggers)) {
                      const input: StandingInput = {
                        installed,
                        enabled,
                        quarantined,
                        upgradeBlockers: blockers,
                        otherSetup,
                        steps,
                        oauth,
                        accounts: list,
                        triggers: t,
                      }
                      const s = providerStanding(input)
                      const context = JSON.stringify({ input, name })
                      expect(s.tone === "attention", context).toBe(
                        s.problems.length > 0
                      )
                      if (s.tone !== "attention") continue
                      attention++
                      expect(s.pill, context).toBe("Needs attention")
                      expect(s.line.startsWith(s.problems[0].summary)).toBe(
                        true
                      )
                      for (const p of s.problems) {
                        expect(p.summary, context).toMatch(/\S/)
                        expect(
                          p.fixes.length > 0 || (p.detail?.length ?? 0) > 0,
                          context
                        ).toBe(true)
                      }
                      // Whatever the old rule called broken still is.
                      const broken =
                        name === "signIn" ||
                        name === "sync" ||
                        name === "legacy"
                      if (quarantined || (installed && enabled && broken))
                        expect(s.tone, context).toBe("attention")
                    }
    expect(attention).toBeGreaterThan(0)
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

describe("what a provider did", () => {
  const G = "providers.substrate.reamde.dev/google"
  const KIND = "substrate.reamde.dev/core/kind"
  const FN = "substrate.reamde.dev/core/function"
  const fn = (name: string, writes: string[]) =>
    record(
      `${G}/${name}`,
      { permissions: { writes: writes.map((k) => ({ ref: `${KIND}/${k}` })) } },
      FN
    )
  const trigger = (id: string, fnName: string) =>
    record(
      id,
      { callable: { ref: `${FN}/${G}/${fnName}` } },
      "substrate.reamde.dev/core/trigger"
    )
  const run = (trigger: string, status: string, finishedAt: string) =>
    record(
      `run-${trigger}-${finishedAt}`,
      {
        trigger: { ref: `substrate.reamde.dev/core/trigger/${trigger}` },
        status,
        finishedAt,
      },
      "substrate.reamde.dev/core/triggerrun"
    )

  it("names the bundle and its functions as actors", () => {
    expect(
      providerActors(G, [
        `${G}/synccontacts`,
        "providers.substrate.reamde.dev/github/sync",
        `${G}/syncgmail`,
      ])
    ).toEqual([
      "bundle:providers.substrate.reamde.dev:google",
      "function:providers.substrate.reamde.dev:google:synccontacts",
      "function:providers.substrate.reamde.dev:google:syncgmail",
    ])
  })

  it("dates a kind's last sync by the newest finished run of a function that writes it", () => {
    const functions = [
      fn("synccontacts", [`${G}/contact`, `${G}/emailaddress`]),
      fn("syncgmail", [`${G}/gmailthread`, `${G}/emailaddress`]),
    ]
    const triggers = [
      trigger("contacts-hourly", "synccontacts"),
      trigger("gmail-hourly", "syncgmail"),
    ]
    const runs = [
      run("contacts-hourly", "ok", "2026-09-25T09:00:00Z"),
      run("contacts-hourly", "parked", "2026-09-25T11:00:00Z"),
      run("gmail-hourly", "ok", "2026-09-25T10:00:00Z"),
      run("gmail-hourly", "skipped", "2026-09-25T12:00:00Z"),
    ]
    expect(lastSyncOf(`${G}/contact`, G, functions, triggers, runs)).toBe(
      "2026-09-25T09:00:00Z"
    )
    expect(lastSyncOf(`${G}/emailaddress`, G, functions, triggers, runs)).toBe(
      "2026-09-25T10:00:00Z"
    )
    expect(
      lastSyncOf(`${G}/account`, G, functions, triggers, runs)
    ).toBeUndefined()
  })
})
