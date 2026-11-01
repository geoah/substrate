import { describe, expect, it } from "vitest"

import type {
  AuthorizationBlock,
  AuthorizationRequest,
  AuthorizationScope,
  AuthorizationSetup,
} from "@/lib/api/types"

import {
  asSentence,
  blockedWords,
  blockRemedy,
  blockStatus,
  chainLines,
  codeWords,
  consolePath,
  factCounts,
  failureWords,
  foldRequest,
  grantWords,
  lifetimeEnd,
  needsAction,
  packageName,
  planChanges,
  progressWords,
  propertyLabel,
  propertyValue,
  reauthorizationHeading,
  setupTitle,
  splitByPrevious,
  stillHeld,
  versionWords,
  webhookTriggers,
} from "./app-setup"

const APPLEHEALTH = "providers.substrate.reamde.dev/applehealth"

const HEALTH: AuthorizationSetup = {
  type: "package",
  action: "install",
  package: {
    id: APPLEHEALTH,
    required: 5,
    shipped: 5,
    requires: [
      { id: "substrate.reamde.dev/health", action: "install", shipped: 2 },
      {
        id: "substrate.reamde.dev/labs",
        action: "upgrade",
        held: 1,
        shipped: 2,
        kinds: [
          { identity: "substrate.reamde.dev/labs/analyte" },
          { identity: "substrate.reamde.dev/labs/labreport" },
          { identity: "substrate.reamde.dev/labs/labresult" },
        ],
        functions: [{ id: "substrate.reamde.dev/labs/convertlab" }],
        triggers: [{ id: "labs-on-labresult", enabled: true }],
      },
      {
        id: "substrate.reamde.dev/observations",
        action: "none",
        held: 3,
        shipped: 3,
      },
    ],
    functions: [{ id: `${APPLEHEALTH}/applehealthingest` }],
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
    kind: `${APPLEHEALTH}/account`,
    id: "substrate-sync-1a2b3c4d",
    properties: { displayName: "Ada's iPhone", timezone: "Europe/London" },
  },
}
const WEBHOOK: AuthorizationSetup = {
  type: "webhook",
  action: "enable",
  webhook: { trigger: "applehealth-webhook", package: APPLEHEALTH },
}
const ROWS = [HEALTH, ACCOUNT, WEBHOOK]

function block(
  reason: AuthorizationBlock["reason"],
  fix?: string
): AuthorizationBlock {
  return { reason, problem: `blocked by ${reason}`, ...(fix ? { fix } : {}) }
}

describe("setupTitle", () => {
  it("says what approving does to each row, naming a package from its id", () => {
    expect(packageName(APPLEHEALTH)).toBe("Apple Health")
    expect(packageName("substrate.reamde.dev/health")).toBe("Health")
    expect(setupTitle(HEALTH)).toBe("Install Apple Health")
    expect(setupTitle(ACCOUNT)).toBe(
      "Create the Apple Health account “Ada's iPhone”"
    )
    expect(setupTitle(WEBHOOK)).toBe("Turn on the Apple Health webhook")
    expect(
      setupTitle({
        type: "webhook",
        action: "enable",
        webhook: { trigger: "other-webhook" },
      })
    ).toBe("Turn on the other-webhook webhook")
  })

  it("says a row in place is in place, and an update is an update", () => {
    expect(setupTitle({ ...HEALTH, action: "upgrade" })).toBe(
      "Update Apple Health"
    )
    expect(
      setupTitle({
        type: "package",
        action: "none",
        package: {
          id: "providers.substrate.reamde.dev/apple",
          required: 3,
          held: 3,
          shipped: 3,
        },
      })
    ).toBe("Apple is installed")
    // An account in place is never written, so the name the app asked for
    // is not the one to show.
    expect(setupTitle({ ...ACCOUNT, action: "none" })).toBe(
      "The Apple Health account this app uses is in place"
    )
    expect(setupTitle({ ...WEBHOOK, action: "none" })).toBe(
      "The Apple Health webhook is on"
    )
  })

  it("keeps what a blocked row would do", () => {
    expect(
      setupTitle({
        ...HEALTH,
        action: "upgrade",
        blocked: block("server-update"),
      })
    ).toBe("Update Apple Health")
    expect(setupTitle({ ...ACCOUNT, blocked: block("account-deleted") })).toBe(
      "Create the Apple Health account “Ada's iPhone”"
    )
    expect(setupTitle({ ...WEBHOOK, blocked: block("unsupported") })).toBe(
      "Turn on the Apple Health webhook"
    )
  })
})

describe("the package's words", () => {
  it("says it runs code from its own functions, automatically when an enabled trigger is not a webhook", () => {
    expect(codeWords(HEALTH.package!)).toBe(
      "Runs code on your server, automatically."
    )
    expect(
      codeWords({
        ...HEALTH.package!,
        triggers: [{ id: "applehealth-webhook", webhook: true, enabled: true }],
      })
    ).toBe("Runs code on your server.")
    // What it brings first runs code of its own, said on its own line.
    expect(codeWords({ ...HEALTH.package!, functions: [] })).toBeUndefined()
  })

  it("says code runs each time data arrives when the request turns its webhook on", () => {
    expect(webhookTriggers(ROWS)).toEqual(["applehealth-webhook"])
    expect(codeWords(HEALTH.package!, webhookTriggers(ROWS))).toBe(
      "Runs code on your server, automatically, each time data arrives."
    )
  })

  it("names each package an install brings first with the code it runs, and leaves out what is in place", () => {
    expect(chainLines(HEALTH.package!)).toEqual([
      "Also installs Health.",
      "Also updates Labs, which runs code on your server, automatically.",
    ])
    expect(chainLines({ ...HEALTH.package!, requires: [] })).toEqual([])
  })

  it("counts what a requirement brings of its own", () => {
    expect(factCounts(HEALTH.package!.requires![1])).toBe(
      "3 kinds, 1 function, 1 trigger"
    )
    expect(factCounts(HEALTH.package!.requires![0])).toBeUndefined()
    expect(
      factCounts({
        kinds: [{ identity: "a/b/c" }],
        triggers: [
          { id: "x", enabled: true },
          { id: "y", enabled: false },
        ],
      })
    ).toBe("1 kind, 2 triggers")
  })

  it("names the versions an update goes between and the one an install brings", () => {
    expect(versionWords(HEALTH)).toBe("Version 5")
    expect(
      versionWords({
        ...HEALTH,
        action: "upgrade",
        package: { ...HEALTH.package!, held: 4 },
      })
    ).toBe("From version 4 to 5")
    // A blocked row's problem says its versions.
    expect(
      versionWords({ ...HEALTH, blocked: block("server-update") })
    ).toBeUndefined()
    expect(versionWords({ ...HEALTH, action: "none" })).toBeUndefined()
    expect(versionWords(ACCOUNT)).toBeUndefined()
  })
})

describe("the webhook's access", () => {
  it("says what the app may then do and that refusing it is denying", () => {
    expect(grantWords(WEBHOOK, false)).toBe(
      "The app will then be able to send data to your repository through the Apple Health webhook, for any Apple Health account, and see every delivery to it that could not be filed, whoever sent it. To refuse this, deny the request."
    )
    expect(grantWords({ ...WEBHOOK, action: "none" }, false)).toBe(
      "The app will be able to send data to your repository through the Apple Health webhook, for any Apple Health account, and see every delivery to it that could not be filed, whoever sent it. To refuse this, deny the request."
    )
    expect(grantWords({ ...WEBHOOK, action: "none" }, true)).toBe(
      "The app can already send data to your repository through the Apple Health webhook, for any Apple Health account, and see every delivery to it that could not be filed, whoever sent it."
    )
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

describe("a blocked row", () => {
  it("is cleared by what its reason says, never by whether a link came with it", () => {
    for (const reason of [
      "sample",
      "quarantined",
      "declared-by-owner",
      "upgrade-needs-review",
    ] as const)
      expect(blockRemedy(block(reason, "/providers/x"))).toBe("console")
    expect(blockRemedy(block("server-update"))).toBe("server")
    expect(blockRemedy(block("account-deleted"))).toBe("wait")
    for (const reason of [
      "not-shipped",
      "not-requested",
      "unsupported",
    ] as const)
      expect(blockRemedy(block(reason))).toBe("elsewhere")
    // A reason newer than the console: its link is all it can go by.
    const later = (fix?: string) =>
      ({
        reason: "later",
        problem: "p",
        ...(fix ? { fix } : {}),
      }) as unknown as AuthorizationBlock
    expect(blockRemedy(later("/providers/x"))).toBe("console")
    expect(blockRemedy(later())).toBe("elsewhere")
    expect(blockStatus(block("server-update"))).toBe("Needs a server update")
    expect(blockStatus(block("quarantined", "/p"))).toBe("Needs a fix first")
  })

  it("says below the buttons what the owner can do", () => {
    expect(blockedWords([block("server-update")])).toBe(
      "Your server needs an update before this can be approved. Whoever runs it can update it; check again afterwards, or deny the request."
    )
    expect(blockedWords([block("quarantined", "/providers/x")])).toBe(
      "Fix it in the console (the link beside the row), then check again, or deny the request."
    )
    expect(
      blockedWords([
        block("quarantined", "/providers/x"),
        block("declared-by-owner", "/providers/y"),
      ])
    ).toBe(
      "Fix it in the console (the links beside the rows), then check again, or deny the request."
    )
    expect(
      blockedWords([
        block("server-update"),
        block("upgrade-needs-review", "/providers/x"),
      ])
    ).toBe(
      "Your server needs an update, and the rest can be fixed in the console (the link beside the row), before this can be approved. Check again afterwards, or deny the request."
    )
    expect(blockedWords([block("server-update"), block("not-requested")])).toBe(
      "This can’t be fixed from here: it takes a change to the app or to your server. Deny the request, or check again once one has changed."
    )
    expect(blockedWords([block("account-deleted")])).toBe(
      "This can be approved once what is in the way has cleared. Check again later, or deny the request."
    )
  })

  it("writes the server's sentence as a sentence", () => {
    expect(asSentence("try again shortly")).toBe("Try again shortly.")
    expect(asSentence("Done!")).toBe("Done!")
    expect(asSentence("")).toBe("")
  })
})

describe("needsAction", () => {
  it("is true for what approving runs, never for a row in place", () => {
    for (const a of ["install", "upgrade", "create", "enable"] as const)
      expect(needsAction(a)).toBe(true)
    for (const a of ["none", "unknown"] as const)
      expect(needsAction(a)).toBe(false)
  })
})

describe("re-authorization", () => {
  const scope = (s: string, allowed?: boolean) =>
    ({
      scope: s,
      verb: "read",
      ...(allowed ? { allowed } : {}),
    }) as AuthorizationScope

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

  it("splits the scopes by what the server says the token covers, not by their spelling", () => {
    const split = splitByPrevious([
      scope("read:ada.example.com/tasks/task", true),
      scope("decide:*"),
    ])
    expect(split.fresh.map((s) => s.scope)).toEqual(["decide:*"])
    expect(split.allowed.map((s) => s.scope)).toEqual([
      "read:ada.example.com/tasks/task",
    ])
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

describe("foldRequest", () => {
  const now = Date.parse("2026-11-01T12:00:00Z")
  const ACCOUNTS = {
    scope: `read:${APPLEHEALTH}/account`,
    requested: `read:${APPLEHEALTH}/account`,
    verb: "read",
    write: false,
    pattern: `${APPLEHEALTH}/account`,
    glob: false,
    kinds: [],
    agents: [],
  } satisfies AuthorizationScope
  const TASKS = {
    ...ACCOUNTS,
    scope: "write:ada.example.com/tasks/*",
    requested: "write:~/tasks/*",
    verb: "write",
    write: true,
    pattern: "ada.example.com/tasks/*",
    glob: true,
  } satisfies AuthorizationScope
  const REQUEST: AuthorizationRequest = {
    id: "req-1",
    app: {
      clientId: "https://sync.example.com/client.json",
      host: "sync.example.com",
      local: false,
      actor: "app:sync.example.com",
    },
    redirect: { uri: "https://sync.example.com/cb", host: "sync.example.com" },
    repository: "ada.example.com",
    scopes: [ACCOUNTS],
    lifetimeDays: [30],
    defaultLifetimeDays: 30,
    expiresAt: "2026-11-01T12:10:00Z",
    details: [],
    setup: ROWS,
    state: "pending",
  }
  const previous = {
    approvedAt: "2026-10-01T00:00:00Z",
    expiresAt: "2026-12-01T00:00:00Z",
    scopes: [ACCOUNTS.scope, "webhook:applehealth-webhook"],
  }

  it("lists everything on a first approval", () => {
    const fold = foldRequest(REQUEST, now)
    expect(fold.reauth).toBe(false)
    expect(fold.fresh).toEqual([ACCOUNTS])
    expect(fold.allowed).toEqual([])
    expect(fold.setup.map((l) => l.index)).toEqual([0, 1, 2])
    expect(fold.setup[2].grantHeld).toBe(false)
    expect(fold.folded).toEqual([])
    expect(fold.toDo).toBe(3)
    expect(fold.newAccess).toBe(2)
    expect(fold.blocks).toEqual([])
    // A read alone writes nothing, but what it sets up does.
    expect(fold.writes).toBe(true)
    expect(foldRequest({ ...REQUEST, setup: [] }, now).writes).toBe(false)
    expect(
      foldRequest({ ...REQUEST, setup: [], scopes: [TASKS] }, now).writes
    ).toBe(true)
  })

  it("folds what the token the app holds covers and what is in place, with the access it brings", () => {
    const fold = foldRequest(
      {
        ...REQUEST,
        scopes: [{ ...ACCOUNTS, allowed: true }, TASKS],
        setup: [
          { ...HEALTH, action: "none" },
          ACCOUNT,
          { ...WEBHOOK, action: "none" },
        ],
        previous,
      },
      now
    )
    expect(fold.reauth).toBe(true)
    expect(fold.live).toBe(true)
    expect(fold.fresh.map((s) => s.scope)).toEqual([TASKS.scope])
    expect(fold.allowed.map((s) => s.scope)).toEqual([ACCOUNTS.scope])
    expect(fold.setup.map((l) => l.index)).toEqual([1])
    expect(fold.folded.map((l) => l.index)).toEqual([0, 2])
    expect(fold.folded[1].grantHeld).toBe(true)
    expect(fold.newAccess).toBe(1)
    expect(fold.toDo).toBe(1)
  })

  it("keeps a webhook that is on in view when the access it brings is new", () => {
    const fold = foldRequest(
      {
        ...REQUEST,
        scopes: [{ ...ACCOUNTS, allowed: true }],
        setup: [{ ...WEBHOOK, action: "none" }],
        previous: { ...previous, scopes: [ACCOUNTS.scope] },
      },
      now
    )
    expect(fold.setup.map((l) => l.grantHeld)).toEqual([false])
    expect(fold.folded).toEqual([])
    expect(fold.newAccess).toBe(1)
    expect(fold.toDo).toBe(0)
  })

  it("asks for everything in full when the access it held has ended", () => {
    const fold = foldRequest(
      {
        ...REQUEST,
        scopes: [{ ...ACCOUNTS, allowed: true }],
        setup: [{ ...WEBHOOK, action: "none" }],
        previous: { ...previous, expiresAt: "2026-10-31T00:00:00Z" },
      },
      now
    )
    expect(fold.reauth).toBe(true)
    expect(fold.live).toBe(false)
    expect(fold.fresh.map((s) => s.scope)).toEqual([ACCOUNTS.scope])
    expect(fold.setup).toHaveLength(1)
    expect(fold.folded).toEqual([])
    // Nothing new for the heading: a renewal.
    expect(fold.newAccess).toBe(0)
  })

  it("collects every block and names the row a set up stopped at", () => {
    const blocked = { ...HEALTH, blocked: block("server-update") }
    expect(
      foldRequest({ ...REQUEST, setup: [blocked, ACCOUNT, WEBHOOK] }, now)
        .blocks
    ).toEqual([blocked.blocked])
    expect(
      foldRequest({ ...REQUEST, failure: { row: 1, message: "refused" } }, now)
        .failedRow
    ).toBe(ACCOUNT)
    expect(
      foldRequest({ ...REQUEST, failure: { message: "refused" } }, now)
        .failedRow
    ).toBeUndefined()
    // Only a pending request is stopped.
    expect(
      foldRequest(
        {
          ...REQUEST,
          state: "setting-up",
          failure: { row: 1, message: "refused" },
        },
        now
      ).failedRow
    ).toBeUndefined()
  })
})

describe("planChanges", () => {
  it("names each row whose action or block moved, as it reads now", () => {
    const after: AuthorizationSetup[] = [
      { ...HEALTH, action: "none" },
      ACCOUNT,
      {
        ...WEBHOOK,
        blocked: {
          reason: "not-requested",
          problem:
            "this trigger runs code from a package this request does not set up",
        },
      },
    ]
    expect(planChanges(ROWS, after)).toEqual([
      "Apple Health is installed",
      "Turn on the Apple Health webhook: can’t be done now. This trigger runs code from a package this request does not set up.",
    ])
    expect(planChanges(ROWS, ROWS)).toEqual([])
  })
})

describe("progress", () => {
  it("says which row a run is on and how many of the rows it runs are done", () => {
    const run = (p: AuthorizationSetup["progress"][]) =>
      ROWS.map((r, i) => ({ ...r, progress: p[i] }))
    expect(progressWords(run(["done", "running", "waiting"]))).toBe(
      "Create the Apple Health account “Ada's iPhone”: step 2 of 3"
    )
    expect(progressWords(run(["done", "done", "waiting"]))).toBe("2 of 3 done")
    // A row in place is not part of the run.
    expect(
      progressWords([
        { ...HEALTH, action: "none", progress: "done" },
        { ...ACCOUNT, progress: "running" },
      ])
    ).toBe("Create the Apple Health account “Ada's iPhone”: step 1 of 1")
    expect(progressWords(ROWS)).toBeUndefined()
  })

  it("says where a set up stopped in one line", () => {
    expect(failureWords(ACCOUNT)).toBe(
      "Setting up stopped at Create the Apple Health account “Ada's iPhone”. What landed stays; Try again runs the rest."
    )
    expect(failureWords(undefined)).toBe("Setting up stopped.")
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
