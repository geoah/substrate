/** The settings fold: which bundle owns a record (its id prefix, and nothing
 * else), and which control the `type` hint earns. */

import { describe, expect, it } from "vitest"

import { SECRET_KIND, SETTING_KIND } from "@/lib/api/settings"
import type { SubstrateRecord } from "@/lib/api/types"
import {
  bundleIdOf,
  groupSettings,
  settingField,
  settingNameOf,
  unset,
} from "@/lib/settings"

function record(
  id: string,
  properties: Record<string, unknown>,
  kind = SETTING_KIND
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  }
}

describe("ownership by id prefix", () => {
  it("reads the bundle and the name out of the id", () => {
    expect(bundleIdOf("ada.example.com/firecrawl/apiKey")).toBe(
      "ada.example.com/firecrawl"
    )
    expect(settingNameOf("ada.example.com/firecrawl/apiKey")).toBe("apiKey")
  })

  // The server admits exactly `<authority>/<package>/<name>` (engine
  // `settingName`), so anything else in the collection is not a bundle's
  // setting and is not shown as one.
  it("refuses an id that is not a bundle and one name", () => {
    for (const id of [
      "ada.example.com",
      "ada.example.com/firecrawl",
      "ada.example.com/firecrawl/nested/key",
      "ada.example.com//key",
    ]) {
      expect(settingNameOf(id)).toBe("")
      expect(bundleIdOf(id)).toBe("")
    }
  })

  it("leaves a record no bundle owns out of the groups", () => {
    const groups = groupSettings([
      record("ada.example.com/firecrawl/apiKey", {}),
      record("ada.example.com/firecrawl/nested/key", {}),
    ])
    expect(groups).toHaveLength(1)
    expect(groups[0].fields.map((f) => f.name)).toEqual(["apiKey"])
  })

  // A record is unique by KIND and id: one bundle may own a `setting` and a
  // `secret` both called apiKey, and nothing may confuse the two.
  it("keys a field by its kind as well as its id", () => {
    const setting = settingField(record("a.example.com/x/apiKey", {}))
    const secret = settingField(
      record("a.example.com/x/apiKey", {}, SECRET_KIND)
    )
    expect(setting.key).not.toBe(secret.key)
    expect(setting.field.name).not.toBe(secret.field.name)
  })
})

describe("groupSettings", () => {
  it("groups by bundle, sorting both the bundles and their fields", () => {
    const groups = groupSettings([
      record("ada.example.com/web/timeout", { type: "int", value: "30" }),
      record("ada.example.com/firecrawl/baseURL", {
        type: "url",
        value: "https://api.example.com",
      }),
      record("ada.example.com/firecrawl/apiKey", {}, SECRET_KIND),
    ])
    expect(groups.map((g) => g.bundle)).toEqual([
      "ada.example.com/firecrawl",
      "ada.example.com/web",
    ])
    expect(groups[0].fields.map((f) => f.name)).toEqual(["apiKey", "baseURL"])
  })
})

describe("the control a setting earns", () => {
  it("takes its label from displayName and falls back to the name", () => {
    expect(
      settingField(record("a.example.com/x/apiKey", { displayName: "API key" }))
        .field.label
    ).toBe("API key")
    expect(settingField(record("a.example.com/x/apiKey", {})).field.label).toBe(
      "Api key"
    )
  })

  it("reads each type hint as the datatype whose control fits it", () => {
    const control = (properties: Record<string, unknown>) =>
      settingField(record("a.example.com/x/s", properties)).field.control
    expect(control({ type: "string" })).toBe("text")
    expect(control({ type: "url" })).toBe("text")
    expect(control({ type: "int" })).toBe("number")
    expect(control({ type: "bool" })).toBe("bool")
    expect(control({ type: "enum", values: ["a", "b"] })).toBe("select")
    // An unknown hint is a string, because the value is stored as one anyway.
    expect(control({ type: "nonsense" })).toBe("text")
  })

  it("offers an enum's admitted values as its options", () => {
    const field = settingField(
      record("a.example.com/x/mode", { type: "enum", values: ["fast", "slow"] })
    )
    expect(field.field.options?.map((o) => o.value)).toEqual(["fast", "slow"])
  })

  // A sealed secret reads back as the redacted marker and an empty one reads
  // back empty, so a stored value is the only question the console answers
  // about one, and the form never seeds what it cannot read.
  it("never seeds a secret, and says whether one is set", () => {
    const sealed = settingField(
      record("a.example.com/x/apiKey", { value: "<redacted>" }, SECRET_KIND)
    )
    expect(sealed.secret).toBe(true)
    expect(sealed.value).toBe("")
    expect(sealed.set).toBe(true)
    const empty = settingField(
      record("a.example.com/x/apiKey", { value: "" }, SECRET_KIND)
    )
    expect(empty.set).toBe(false)
    const never = settingField(
      record("a.example.com/x/apiKey", {}, SECRET_KIND)
    )
    expect(never.set).toBe(false)
  })

  it("marks a required setting with no value", () => {
    expect(
      unset(settingField(record("a.example.com/x/s", { required: true })))
    ).toBe(true)
    expect(
      unset(
        settingField(
          record("a.example.com/x/s", { required: true, value: "v" })
        )
      )
    ).toBe(false)
    expect(unset(settingField(record("a.example.com/x/s", {})))).toBe(false)
  })
})
