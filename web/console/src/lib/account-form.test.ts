/** The Add account form's fields, off the declaration alone: the owner's
 * toggles and settings are offered, the OAuth facility's and the connector's
 * hands are not, and the `sync` trait's two owner hands are left to the
 * page's buttons. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  accountFormGroups,
  ungrantedToggles,
  writeToggles,
} from "./account-form"
import { SHIPPED_UNITS } from "@/lib/unit-graph.test-fixtures"

function kind(traits: string[], props: Record<string, unknown>): KindInfo {
  return {
    identity: "providers.substrate.reamde.dev/google/account",
    name: "account",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    version: 1,
    source: "published",
    description: "",
    definition: { traits, properties: props },
  }
}

const GOOGLE_LIKE = {
  tokenRef: { type: "secret", writer: "system" },
  tokenStatus: { type: "string", writer: "system" },
  email: { type: "email", writer: "system" },
  address: {
    type: "reference",
    kind: "emailaddress",
    writer: ["function", "agent"],
  },
  enabledContacts: { type: "bool", writer: "person", displayName: "Contacts" },
  enabledGmail: { type: "bool", writer: "person", displayName: "Gmail" },
  syncFrequency: {
    type: "enum",
    values: ["off", "hourly", "daily"],
    required: true,
    default: "daily",
    writer: "person",
  },
  backfillDepth: {
    type: "enum",
    values: ["none", "last30d", "all"],
    required: true,
    default: "last30d",
    writer: "person",
  },
  syncRequestedAt: { type: "datetime", writer: "person" },
  syncPaused: { type: "bool", writer: "person" },
  syncState: { type: "string", writer: ["function", "agent"] },
  contactsResume: {
    type: "object",
    writer: ["function", "agent"],
    fields: { pageToken: { type: "string" } },
  },
}

describe("accountFormGroups", () => {
  it("offers the owner's toggles and settings, and nothing any other hand writes", () => {
    const g = accountFormGroups(
      SHIPPED_UNITS,
      kind(["accountconfig", "sync"], GOOGLE_LIKE)
    )
    expect(g.toggles.map((f) => f.name)).toEqual([
      "enabledContacts",
      "enabledGmail",
    ])
    expect(g.settings.map((f) => f.name)).toEqual([
      "backfillDepth",
      "syncFrequency",
    ])
    expect(g.all.map((f) => f.name)).not.toContain("address")
    expect(g.all.map((f) => f.name)).not.toContain("contactsResume")
    expect(g.all.map((f) => f.name)).not.toContain("email")
  })

  it("leaves the sync trait's two owner hands to the Sync now and Pause buttons", () => {
    const g = accountFormGroups(
      SHIPPED_UNITS,
      kind(["accountconfig", "sync"], GOOGLE_LIKE)
    )
    const names = g.all.map((f) => f.name)
    expect(names).not.toContain("syncRequestedAt")
    expect(names).not.toContain("syncPaused")
  })

  it("offers those two names on a kind that does not bind the trait", () => {
    const g = accountFormGroups(
      SHIPPED_UNITS,
      kind(["accountconfig"], GOOGLE_LIKE)
    )
    const names = g.all.map((f) => f.name)
    expect(names).toContain("syncRequestedAt")
    expect(names).toContain("syncPaused")
  })
})

describe("the switches that let agents act", () => {
  const props = {
    ...GOOGLE_LIKE,
    enabledGmailWrite: {
      type: "bool",
      writer: "person",
      displayName: "Gmail sending",
    },
  }
  const features = [
    {
      toggle: "enabledContacts",
      scopes: ["contacts.readonly"],
      access: "read",
    },
    { toggle: "enabledGmail", scopes: ["gmail.readonly"], access: "read" },
    {
      toggle: "enabledGmailWrite",
      scopes: ["gmail.modify", "userinfo.email"],
      access: "write",
    },
  ]

  it("stand apart from what the account brings in", () => {
    const g = accountFormGroups(
      SHIPPED_UNITS,
      kind(["accountconfig", "sync"], props),
      writeToggles(features)
    )
    expect(g.toggles.map((f) => f.name)).toEqual([
      "enabledContacts",
      "enabledGmail",
    ])
    expect(g.permissions.map((f) => f.name)).toEqual(["enabledGmailWrite"])
    expect(g.all.map((f) => f.name)).toContain("enabledGmailWrite")
  })

  it("are things to bring in when the bundle marks none", () => {
    const g = accountFormGroups(
      SHIPPED_UNITS,
      kind(["accountconfig", "sync"], props)
    )
    expect(g.permissions).toEqual([])
    expect(g.toggles.map((f) => f.name)).toContain("enabledGmailWrite")
  })

  it("name the switches an account has on that its sign-in does not cover", () => {
    const on = { enabledGmail: true, enabledGmailWrite: true }
    expect(ungrantedToggles(on, ["gmail.readonly"], features)).toEqual([
      "enabledGmailWrite",
    ])
    expect(
      ungrantedToggles(
        on,
        ["gmail.readonly", "gmail.modify", "userinfo.email"],
        features
      )
    ).toEqual([])
    // A switch that is off asks for nothing, granted or not.
    expect(ungrantedToggles({ enabledGmail: true }, [], features)).toEqual([
      "enabledGmail",
    ])
    expect(ungrantedToggles({}, [], features)).toEqual([])
  })
})
