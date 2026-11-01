import { describe, expect, it } from "vitest"

import type { AuthorizationScope, AuthorizationSetup } from "@/lib/api/types"

import {
  asSentence,
  packageName,
  chainWords,
  codeWords,
  consolePath,
  lifetimeEnd,
  needsAction,
  planChanges,
  problemWords,
  progressWords,
  propertyLabel,
  propertyValue,
  reauthorizationHeading,
  setupTitle,
  shownProgress,
  splitByPrevious,
  stillHeld,
  webhookTriggers,
} from "./app-setup"

const HEALTH: AuthorizationSetup = {
  type: "package",
  action: "install",
  package: {
    id: "providers.substrate.reamde.dev/applehealth",
    name: "Apple Health",
    required: 5,
    held: 0,
    shipped: 5,
    requires: [
      {
        id: "substrate.reamde.dev/health",
        name: "Health",
        action: "install",
        held: 0,
        shipped: 2,
      },
      {
        id: "substrate.reamde.dev/labs",
        action: "upgrade",
        held: 1,
        shipped: 2,
      },
      {
        id: "substrate.reamde.dev/observations",
        action: "none",
        held: 3,
        shipped: 3,
      },
    ],
    functions: [{ id: "applehealthingest" }],
    triggers: [
      { id: "applehealth-webhook", webhook: true, enabled: false },
      { id: "applehealth-to-health", enabled: true },
    ],
  },
}
const ACCOUNT: AuthorizationSetup = {
  type: "account",
  action: "create",
  account: {
    kind: "providers.substrate.reamde.dev/applehealth/account",
    id: "substrate-sync-1a2b3c4d",
    properties: { displayName: "Ada's iPhone", timezone: "Europe/London" },
  },
}
const WEBHOOK: AuthorizationSetup = {
  type: "webhook",
  action: "arm",
  webhook: {
    trigger: "applehealth-webhook",
    package: "providers.substrate.reamde.dev/applehealth",
    enabled: false,
    keyed: false,
  },
}
const ROWS = [HEALTH, ACCOUNT, WEBHOOK]

describe("setupTitle", () => {
  it("says what approving does to each row, by the catalog's names", () => {
    expect(setupTitle(HEALTH, ROWS)).toBe("Install Apple Health")
    // A catalog that names a package by its word alone reads in words.
    expect(
      packageName({
        id: "providers.substrate.reamde.dev/applehealth",
        name: "applehealth",
      })
    ).toBe("Apple Health")
    expect(
      packageName({ id: "substrate.reamde.dev/health", name: "health" })
    ).toBe("Health")
    expect(setupTitle(ACCOUNT, ROWS)).toBe(
      "Create the Apple Health account “Ada's iPhone”"
    )
    expect(setupTitle(WEBHOOK, ROWS)).toBe("Turn on the Apple Health webhook")
  })

  it("says a row in place is in place, and names a package it has no name for by its word", () => {
    expect(
      setupTitle(
        {
          ...HEALTH,
          action: "upgrade",
          package: { ...HEALTH.package!, held: 4 },
        },
        ROWS
      )
    ).toBe("Update Apple Health")
    expect(
      setupTitle(
        {
          type: "package",
          action: "none",
          package: {
            id: "providers.substrate.reamde.dev/apple",
            required: 3,
            held: 3,
            shipped: 3,
          },
        },
        ROWS
      )
    ).toBe("Apple is installed")
    // An account in place is never written, so the name the app asked for
    // is not the one to show.
    expect(setupTitle({ ...ACCOUNT, action: "none" }, ROWS)).toBe(
      "The Apple Health account this app uses is in place"
    )
    expect(setupTitle({ ...WEBHOOK, action: "none" }, ROWS)).toBe(
      "The Apple Health webhook is on"
    )
  })
})

describe("the package's words", () => {
  it("says it runs code, automatically when an enabled trigger is not a webhook", () => {
    expect(codeWords(HEALTH.package!)).toBe(
      "Runs code on your server, automatically."
    )
    expect(
      codeWords({
        ...HEALTH.package!,
        triggers: [{ id: "applehealth-webhook", webhook: true, enabled: true }],
      })
    ).toBe("Runs code on your server.")
    expect(codeWords({ ...HEALTH.package!, functions: [] })).toBeUndefined()
  })

  it("says code runs each time data arrives when the request turns its webhook on", () => {
    expect(webhookTriggers(ROWS)).toEqual(["applehealth-webhook"])
    expect(codeWords(HEALTH.package!, webhookTriggers(ROWS))).toBe(
      "Runs code on your server, automatically, each time data arrives."
    )
    expect(
      codeWords(
        {
          ...HEALTH.package!,
          triggers: [
            { id: "applehealth-webhook", webhook: true, enabled: false },
          ],
        },
        ["applehealth-webhook"]
      )
    ).toBe("Runs code on your server, automatically, each time data arrives.")
  })

  it("names what an install brings first, and leaves out what is in place", () => {
    expect(chainWords(HEALTH.package!)).toBe(
      "Also installs Health and updates Labs."
    )
    expect(chainWords({ ...HEALTH.package!, requires: [] })).toBeUndefined()
  })
})

describe("the account's values", () => {
  it("labels a property by its words and shows every value as text", () => {
    expect(propertyLabel("displayName")).toBe("Display name")
    expect(propertyLabel("timezone")).toBe("Timezone")
    expect(propertyValue("Europe/London")).toBe("Europe/London")
    expect(propertyValue(true)).toBe("Yes")
    expect(propertyValue(3)).toBe("3")
    expect(propertyValue(["a", "b"])).toBe("a, b")
    expect(propertyValue({ a: 1 })).toBe('{"a":1}')
  })
})

describe("a blocked row's problem", () => {
  const floor: AuthorizationSetup = {
    ...HEALTH,
    action: "blocked",
    package: { ...HEALTH.package!, name: "Apple", shipped: 3, required: 4 },
    problem:
      "this server ships version 3 of providers.substrate.reamde.dev/apple, and the app needs version 4: the server needs an update",
  }

  it("says a catalog below the app's floor in the owner's words", () => {
    expect(problemWords(floor)).toBe(
      "Your server can install Apple up to version 3; this app needs version 4. The server needs an update first."
    )
    expect(
      problemWords({
        ...HEALTH,
        action: "blocked",
        package: {
          ...HEALTH.package!,
          requires: [
            {
              id: "substrate.reamde.dev/health",
              name: "Health",
              action: "blocked",
              held: 0,
              shipped: 1,
            },
          ],
        },
        problem:
          "this server ships version 1 of substrate.reamde.dev/health, which providers.substrate.reamde.dev/applehealth needs at version 2: the server needs an update",
      })
    ).toBe(
      "Your server can install Health up to version 1, and Apple Health needs a later one. The server needs an update first."
    )
  })

  it("keeps the server's sentence for any other block, as a sentence", () => {
    // A quarantined package links to its page, so the versions are not why.
    expect(
      problemWords({
        ...floor,
        fix: "/providers/x",
        problem: "providers.substrate.reamde.dev/apple is quarantined: bad",
      })
    ).toBe("Providers.substrate.reamde.dev/apple is quarantined: bad.")
    expect(
      problemWords({
        ...WEBHOOK,
        action: "blocked",
        problem: "the trigger's function is not Apple Health's",
      })
    ).toBe("The trigger's function is not Apple Health's.")
    expect(asSentence("try again shortly")).toBe("Try again shortly.")
    expect(asSentence("Done!")).toBe("Done!")
    expect(asSentence("")).toBe("")
  })
})

describe("needsAction", () => {
  it("is true for what approving runs, never for a row in place or blocked", () => {
    for (const a of ["install", "upgrade", "create", "arm"] as const)
      expect(needsAction(a)).toBe(true)
    for (const a of ["none", "blocked", "unknown"] as const)
      expect(needsAction(a)).toBe(false)
  })
})

describe("re-authorization", () => {
  const scope = (s: string) => ({ scope: s }) as AuthorizationScope

  it("holds access only until it ends", () => {
    const now = Date.parse("2026-11-01T12:00:00Z")
    const approvedAt = "2026-10-01T00:00:00Z"
    expect(stillHeld({ approvedAt, scopes: [] }, now)).toBe(true)
    expect(
      stillHeld(
        { approvedAt, expiresAt: "2026-11-02T00:00:00Z", scopes: [] },
        now
      )
    ).toBe(true)
    expect(
      stillHeld(
        { approvedAt, expiresAt: "2026-10-31T09:00:00Z", scopes: [] },
        now
      )
    ).toBe(false)
  })

  it("splits the scopes the app holds from the new ones", () => {
    const split = splitByPrevious([scope("read:*"), scope("decide:*")], {
      approvedAt: "2026-10-01T00:00:00Z",
      scopes: ["read:*"],
    })
    expect(split.fresh.map((s) => s.scope)).toEqual(["decide:*"])
    expect(split.allowed.map((s) => s.scope)).toEqual(["read:*"])
    expect(splitByPrevious([scope("read:*")], undefined).fresh).toHaveLength(1)
  })

  it("heads by what changed", () => {
    expect(reauthorizationHeading(0, 0)).toBe("Renew access")
    expect(reauthorizationHeading(2, 0)).toBe("Allow more access")
    expect(reauthorizationHeading(0, 1)).toBe("Update server setup")
    expect(reauthorizationHeading(1, 1)).toBe(
      "Allow more access and update server setup"
    )
  })
})

describe("planChanges", () => {
  it("names each row whose action moved, as it reads now", () => {
    const after: AuthorizationSetup[] = [
      { ...HEALTH, action: "none" },
      ACCOUNT,
      {
        ...WEBHOOK,
        action: "blocked",
        problem: "the trigger's function is not Apple Health's",
      },
    ]
    expect(planChanges(ROWS, after)).toEqual([
      "Apple Health is installed",
      "Turn on the Apple Health webhook: can’t be done now. The trigger's function is not Apple Health's.",
    ])
    expect(planChanges(ROWS, ROWS)).toEqual([])
  })
})

describe("progress", () => {
  it("shows a run's progress while it runs, and once it stopped only what landed and where it stopped", () => {
    expect(shownProgress("setting-up", "install", "waiting")).toBe("waiting")
    expect(shownProgress("setting-up", "none", "done")).toBe("done")
    expect(shownProgress("approved", "none", "done")).toBe("done")
    // Pending again: nothing runs, so nothing waits.
    expect(shownProgress("pending", "arm", "waiting")).toBeUndefined()
    expect(shownProgress("pending", "arm", "running")).toBeUndefined()
    expect(shownProgress("pending", "none", "done")).toBe("done")
    expect(shownProgress("pending", "create", "failed")).toBe("failed")
    // Stopped once, and in place since: it is in place.
    expect(shownProgress("pending", "none", "failed")).toBeUndefined()
    expect(shownProgress("pending", "create", undefined)).toBeUndefined()
  })

  it("says which row a run is on and how many of its rows are done", () => {
    expect(progressWords(ROWS, ["done", "running", "waiting"], [0, 1, 2])).toBe(
      "Create the Apple Health account “Ada's iPhone”: step 2 of 3"
    )
    expect(progressWords(ROWS, ["done", "done", "waiting"], [0, 1, 2])).toBe(
      "2 of 3 done"
    )
    expect(progressWords(ROWS, undefined, [0])).toBeUndefined()
    expect(progressWords(ROWS, ["done"], [])).toBeUndefined()
  })
})

describe("lifetimeEnd", () => {
  it("names the day access ends, the year once it is not this one", () => {
    const now = new Date(2026, 10, 1, 12)
    expect(lifetimeEnd(30, now)).toBe("1 December")
    expect(lifetimeEnd(90, now)).toBe("30 January 2027")
    expect(lifetimeEnd(0, now)).toBeUndefined()
  })
})

describe("consolePath", () => {
  it("follows a path on the console and nothing else", () => {
    expect(consolePath("/providers/applehealth")).toBe("/providers/applehealth")
    expect(consolePath("//evil.example.com")).toBeUndefined()
    expect(consolePath("/\\evil.example.com")).toBeUndefined()
    expect(consolePath("https://evil.example.com")).toBeUndefined()
    expect(consolePath(undefined)).toBeUndefined()
  })
})
