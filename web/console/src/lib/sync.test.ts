/** The Connections page's pure half: the trait read leniently off a record,
 * the health a row wears, the provider fold, and which of a kind's triggers
 * are the on-request ones. */

import { describe, expect, it } from "vitest"

import type { BundleStatus, KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  accountViewOf,
  countPhrase,
  cursorText,
  durationText,
  healthOf,
  kindGlobMatches,
  kindHasTrait,
  providerNextStep,
  providerViews,
  requestServed,
  requestTriggers,
  syncFieldsOf,
  triggersOnKind,
  type AccountView,
} from "./sync"

const GOOGLE = "providers.substrate.reamde.dev/google"
const ACCOUNT = `${GOOGLE}/account`

function kind(
  identity: string,
  traits: string[],
  props: Record<string, unknown> = {}
): KindInfo {
  const [authority, pkg, name] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "published",
    description: "",
    definition: { traits, properties: props },
  }
}

function record(
  id: string,
  properties: Record<string, unknown>,
  k = ACCOUNT
): SubstrateRecord {
  return {
    id,
    kind: k,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-19T08:00:00Z",
    updatedAt: "2026-09-19T09:00:00Z",
  }
}

describe("syncFieldsOf", () => {
  it("reads the trait's properties and narrows the state", () => {
    const f = syncFieldsOf({
      syncState: "erroring",
      syncMessage: "ok (12 pending)",
      syncError: "gmail: HTTP 403",
      syncPaused: false,
      lastSyncDurationMs: "1200",
      syncProgress: { phase: "drain", done: "3", total: 10 },
      syncStreams: { gmail: { state: "erroring", pending: 12 }, contacts: {} },
    })
    expect(f.state).toBe("erroring")
    expect(f.lastSyncDurationMs).toBe(1200)
    expect(f.progress).toEqual({
      phase: "drain",
      done: 3,
      total: 10,
      pending: 0,
    })
    expect(f.streams.gmail.pending).toBe(12)
    expect(f.streams.contacts.pending).toBe(0)
  })

  it("keeps a body's own word beside the neutral state", () => {
    const f = syncFieldsOf({ syncState: "backfilling" })
    expect(f.state).toBe("never")
    expect(f.rawState).toBe("backfilling")
  })

  it("survives a mis-shaped progress and no streams", () => {
    const f = syncFieldsOf({ syncProgress: "half", syncStreams: [1, 2] })
    expect(f.progress).toBeUndefined()
    expect(f.streams).toEqual({})
  })
})

describe("requestServed", () => {
  it("is served with no request, and once the ack reaches the request", () => {
    expect(requestServed({})).toBe(true)
    expect(requestServed({ requestedAt: "2026-09-19T09:00:00Z" })).toBe(false)
    expect(
      requestServed({
        requestedAt: "2026-09-19T09:00:00Z",
        requestedAck: "2026-09-19T08:00:00Z",
      })
    ).toBe(false)
    expect(
      requestServed({
        requestedAt: "2026-09-19T09:00:00Z",
        requestedAck: "2026-09-19T09:00:00Z",
      })
    ).toBe(true)
  })
})

describe("healthOf", () => {
  const ok = syncFieldsOf({ syncState: "ok" })
  it("is broken on a dead grant, an erroring sync or a legacy erroring status", () => {
    expect(healthOf("erroring", ok)).toBe("broken")
    expect(healthOf("connected", syncFieldsOf({ syncState: "erroring" }))).toBe(
      "broken"
    )
    expect(
      healthOf(
        "connected",
        syncFieldsOf({}),
        "erroring: github returned HTTP 403"
      )
    ).toBe("broken")
  })
  it("wants attention while pending, paused or throttled", () => {
    expect(healthOf("pending", ok)).toBe("attention")
    expect(
      healthOf("connected", syncFieldsOf({ syncState: "ok", syncPaused: true }))
    ).toBe("attention")
    expect(
      healthOf("connected", syncFieldsOf({ syncState: "throttled" }))
    ).toBe("attention")
    expect(healthOf(undefined, syncFieldsOf({}))).toBe("attention")
  })
  it("is healthy when connected and synced, idle when connected and never synced", () => {
    expect(healthOf("connected", ok)).toBe("healthy")
    expect(healthOf("connected", syncFieldsOf({}), "ok")).toBe("healthy")
    expect(healthOf("connected", syncFieldsOf({}))).toBe("idle")
  })
})

describe("accountViewOf", () => {
  const kinds = [kind(ACCOUNT, ["accountconfig", "sync"])]
  it("labels by email, folds the provider and reads the trait", () => {
    const v = accountViewOf(
      record("george-work", {
        email: "george@example.com",
        tokenStatus: "connected",
        grantedScopes: ["a", "b"],
        syncFrequency: "hourly",
        backfillDepth: "last30d",
        syncState: "ok",
      }),
      kinds
    )
    expect(v.label).toBe("george@example.com")
    expect(v.provider).toBe(GOOGLE)
    expect(v.grantedScopes).toEqual(["a", "b"])
    expect(v.syncable).toBe(true)
    expect(v.health).toBe("healthy")
  })
  it("falls back through displayName, login and the id, and is not syncable without the trait", () => {
    const plain = [kind(ACCOUNT, ["accountconfig"])]
    expect(
      accountViewOf(record("x", { displayName: "Work" }), plain).label
    ).toBe("Work")
    expect(accountViewOf(record("x", { login: "geoah" }), plain).label).toBe(
      "geoah"
    )
    const v = accountViewOf(record("x", {}), plain)
    expect(v.label).toBe("x")
    expect(v.syncable).toBe(false)
  })
})

describe("kindHasTrait", () => {
  it("matches the bare shipped spelling and the full identity", () => {
    expect(
      kindHasTrait(kind(ACCOUNT, ["sync"]), "substrate.reamde.dev/core/sync")
    ).toBe(true)
    expect(
      kindHasTrait(
        kind(ACCOUNT, ["substrate.reamde.dev/core/sync"]),
        "substrate.reamde.dev/core/sync"
      )
    ).toBe(true)
    expect(
      kindHasTrait(
        kind(ACCOUNT, ["accountconfig"]),
        "substrate.reamde.dev/core/sync"
      )
    ).toBe(false)
    expect(kindHasTrait(undefined, "x")).toBe(false)
  })
})

describe("providerViews", () => {
  const status = (over: Partial<BundleStatus>): BundleStatus => ({
    id: GOOGLE,
    name: "google",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    installed: true,
    enabled: true,
    accounts: 0,
    functions: 0,
    kinds: 0,
    liveRecords: 0,
    ...over,
  })
  const kinds = [
    kind(ACCOUNT, ["accountconfig", "sync"]),
    kind(`${GOOGLE}/config`, ["oauth2"], {
      clientId: { type: "string" },
      clientSecret: { type: "secret" },
    }),
    kind("samples.substrate.reamde.dev/tasks/task", []),
  ]
  const accounts = [
    accountViewOf(record("a", { tokenStatus: "connected" }), kinds),
    accountViewOf(record("b", { tokenStatus: "pending" }), kinds),
    accountViewOf(record("c", {}), kinds),
  ]

  it("lists a bundle with an account kind, counting its accounts by token status", () => {
    const [p] = providerViews(
      [
        status({
          inputs: [
            {
              name: "client",
              kind: `${GOOGLE}/config`,
              record: "default",
              via: "default",
            },
          ],
        }),
      ],
      new Set(),
      kinds,
      accounts
    )
    expect(p.id).toBe(GOOGLE)
    expect(p.accountKind?.identity).toBe(ACCOUNT)
    expect(p.configKind?.identity).toBe(`${GOOGLE}/config`)
    expect(p.configRecord).toBe("default")
    expect(p.configured).toBe(true)
    expect(p.byTokenStatus).toEqual({
      connected: 1,
      pending: 1,
      "not connected": 1,
    })
    expect(countPhrase(p.byTokenStatus)).toBe(
      "1 connected, 1 not connected, 1 pending"
    )
  })

  it("reads credentials missing off the oauth-client setup item", () => {
    const [p] = providerViews(
      [
        status({
          inputs: [{ name: "client", kind: `${GOOGLE}/config` }],
          setup: [{ code: "missing", input: "client", message: "no client" }],
        }),
      ],
      new Set(),
      kinds,
      []
    )
    expect(p.configured).toBe(false)
    expect(p.setupSteps).toBe(1)
  })

  it("includes a catalog provider with no accounts yet and leaves a sample out", () => {
    const rows = providerViews(
      [
        status({}),
        status({
          id: "samples.substrate.reamde.dev/tasks",
          name: "tasks",
          package: "tasks",
        }),
      ],
      new Set([GOOGLE]),
      kinds,
      []
    )
    expect(rows.map((r) => r.id)).toEqual([GOOGLE])
  })

  it("reads whether connecting is a consent flow off the client input's trait", () => {
    const client = { name: "client", kind: `${GOOGLE}/config`, record: "x" }
    const [oauth] = providerViews(
      [status({ inputs: [client] })],
      new Set(),
      kinds,
      []
    )
    expect(oauth.oauth).toBe(true)
    const [token] = providerViews(
      [status({ inputs: [client] })],
      new Set(),
      [
        kind(ACCOUNT, ["accountconfig", "sync"]),
        kind(`${GOOGLE}/config`, [], { apiKey: { type: "secret" } }),
      ],
      []
    )
    expect(token.oauth).toBe(false)
  })
})

describe("providerNextStep", () => {
  const status = (over: Partial<BundleStatus>): BundleStatus => ({
    id: GOOGLE,
    name: "google",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    installed: true,
    enabled: true,
    accounts: 0,
    functions: 0,
    kinds: 0,
    liveRecords: 0,
    ...over,
  })
  const client = { name: "client", kind: `${GOOGLE}/config`, record: "x" }
  const oauthKinds = [
    kind(ACCOUNT, ["accountconfig", "sync"]),
    kind(`${GOOGLE}/config`, ["oauth2"], {
      clientId: { type: "string" },
      clientSecret: { type: "secret" },
    }),
  ]
  const tokenKinds = [
    kind(ACCOUNT, ["accountconfig", "sync"]),
    kind(`${GOOGLE}/config`, [], { apiKey: { type: "secret" } }),
  ]
  const step = (
    over: Partial<BundleStatus>,
    accounts: AccountView[],
    kinds = oauthKinds
  ) =>
    providerNextStep(
      providerViews([status(over)], new Set(), kinds, accounts)[0]
    )

  it("walks install, credentials, account, connect, ready in that order", () => {
    expect(step({ enabled: false, inputs: [client] }, []).step).toBe("install")
    expect(
      step(
        {
          inputs: [client],
          setup: [{ code: "oauth-client", input: "client", message: "" }],
        },
        []
      ).step
    ).toBe("credentials")
    expect(step({ inputs: [client] }, []).step).toBe("account")
    const connected = accountViewOf(
      record("a", { tokenStatus: "connected", email: "a@example.com" }),
      oauthKinds
    )
    const pending = accountViewOf(
      record("b", { tokenStatus: "pending", email: "b@example.com" }),
      oauthKinds
    )
    const next = step({ inputs: [client] }, [connected, pending])
    expect(next.step).toBe("connect")
    expect(next.account?.label).toBe("b@example.com")
    expect(step({ inputs: [client] }, [connected]).step).toBe("ready")
  })

  it("never asks a token provider to connect", () => {
    const account = accountViewOf(record("a", {}), tokenKinds)
    expect(step({ inputs: [client] }, [account], tokenKinds).step).toBe("ready")
  })
})

describe("triggers on a kind", () => {
  const trigger = (id: string, kinds: string[], when = "") =>
    record(
      id,
      {
        enabled: true,
        source: { record: { kinds, ops: ["create", "update"], when } },
      },
      "substrate.reamde.dev/core/trigger"
    )

  it("matches the four glob spellings", () => {
    expect(kindGlobMatches("*", ACCOUNT)).toBe(true)
    expect(kindGlobMatches("providers.substrate.reamde.dev/*", ACCOUNT)).toBe(
      true
    )
    expect(kindGlobMatches(`${GOOGLE}/*`, ACCOUNT)).toBe(true)
    expect(kindGlobMatches(ACCOUNT, ACCOUNT)).toBe(true)
    expect(kindGlobMatches(`${GOOGLE}/contact`, ACCOUNT)).toBe(false)
  })

  it("finds the record triggers on the kind and the on-request ones among them", () => {
    const sources = triggersOnKind(
      [
        trigger(
          "google-gmail-on-connect",
          [ACCOUNT],
          '!("gmailLastSyncedAt" in record.properties)'
        ),
        trigger(
          "google-gmail-on-request",
          [ACCOUNT],
          "record.properties.syncRequestedAt != x"
        ),
        trigger("mneme-on-demand", [`${GOOGLE}/*`]),
        trigger("other", ["samples.substrate.reamde.dev/tasks/task"]),
        record(
          "google-scheduled",
          { source: { schedule: { recurrence: "FREQ=HOURLY" } } },
          "substrate.reamde.dev/core/trigger"
        ),
      ],
      ACCOUNT
    )
    expect(sources.map((s) => s.id)).toEqual([
      "google-gmail-on-connect",
      "google-gmail-on-request",
      "mneme-on-demand",
    ])
    expect(requestTriggers(sources).map((s) => s.id)).toEqual([
      "google-gmail-on-request",
      "mneme-on-demand",
    ])
  })
})

describe("rendering helpers", () => {
  it("renders a cursor as its size, never its bytes", () => {
    expect(cursorText(undefined)).toBe("—")
    expect(cursorText("")).toBe("—")
    expect(cursorText("abc")).toBe("set")
    expect(cursorText([1, 2, 3])).toBe("3 entries")
    expect(cursorText({ a: 1, b: 2 })).toBe("2 keys")
  })
  it("renders a duration in the unit that reads", () => {
    expect(durationText(undefined)).toBe("")
    expect(durationText(250)).toBe("250 ms")
    expect(durationText(2500)).toBe("2.5 s")
    expect(durationText(45_000)).toBe("45 s")
    expect(durationText(300_000)).toBe("5 min")
  })
})
