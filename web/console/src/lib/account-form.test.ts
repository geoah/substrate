/** The Add account form's fields, off the declaration alone: the owner's
 * toggles and settings are offered, the OAuth facility's and the connector's
 * hands are not, and the `sync` trait's two owner hands are left to the
 * page's buttons. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { accountFormGroups } from "./account-form"

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
  tokenRef: { type: "secret", writer: "oauth" },
  tokenStatus: { type: "string", writer: "oauth" },
  email: { type: "email", writer: "oauth" },
  address: { type: "reference", kind: "emailaddress", writer: "connector" },
  enabledContacts: { type: "bool", writer: "owner", displayName: "Contacts" },
  enabledGmail: { type: "bool", writer: "owner", displayName: "Gmail" },
  syncFrequency: {
    type: "enum",
    values: ["off", "hourly", "daily"],
    required: true,
    default: "daily",
    writer: "owner",
  },
  backfillDepth: {
    type: "enum",
    values: ["none", "last30d", "all"],
    required: true,
    default: "last30d",
    writer: "owner",
  },
  syncRequestedAt: { type: "datetime", writer: "owner" },
  syncPaused: { type: "bool", writer: "owner" },
  syncState: { type: "string", writer: "connector" },
  contactsResume: {
    type: "object",
    writer: "connector",
    fields: { pageToken: { type: "string" } },
  },
}

describe("accountFormGroups", () => {
  it("offers the owner's toggles and settings, and nothing any other hand writes", () => {
    const g = accountFormGroups(kind(["accountconfig", "sync"], GOOGLE_LIKE))
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
    const g = accountFormGroups(kind(["accountconfig", "sync"], GOOGLE_LIKE))
    const names = g.all.map((f) => f.name)
    expect(names).not.toContain("syncRequestedAt")
    expect(names).not.toContain("syncPaused")
  })

  it("offers those two names on a kind that does not bind the trait", () => {
    const g = accountFormGroups(kind(["accountconfig"], GOOGLE_LIKE))
    const names = g.all.map((f) => f.name)
    expect(names).toContain("syncRequestedAt")
    expect(names).toContain("syncPaused")
  })
})
