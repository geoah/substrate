/** The generic config/account form's pure core: which schema properties become
 * editable fields (host-managed and title excluded), how a record seeds the
 * form (a secret NEVER seeds), and how the values coerce back to a properties
 * payload (blank secret omitted so the sealed value stands; a bool is always
 * sent; a list is parsed one-per-line). */

import { describe, expect, it } from "vitest"

import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  buildFormFields,
  humanizeName,
  initialValues,
  parseList,
  toProperties,
  validate,
} from "./record-form"

function typeWith(
  properties: Record<string, Record<string, unknown>>
): KindInfo {
  return {
    identity: "google.bundles.substrate.reamde.dev/account",
    name: "account",
    authority: "google.bundles.substrate.reamde.dev",
    version: "",
    plural: "accounts",
    source: "installed",
    definition: { properties },
  }
}

const accountType = typeWith({
  // Owner-writable: an explicit `writer: owner` (email) AND unset (the toggles,
  // cadence) both stay in the form — an unrestricted property is the owner's.
  email: { type: "email", writer: "owner" },
  enabledContacts: { type: "bool" },
  enabledGmail: { type: "bool" },
  enabledCalendar: { type: "bool" },
  syncFrequency: { type: "string" },
  backfillDepth: { type: "string" },
  // host-managed (writer: oauth) / connector state (writer: connector) — never
  // editable; excluded by the `writer:` role, not by name.
  tokenRef: { type: "secret", writer: "oauth" },
  tokenStatus: { type: "string", writer: "oauth" },
  grantedScopes: { type: "string", repeated: true, writer: "oauth" },
  syncToken: { type: "string", writer: "connector" },
  lastSyncedAt: { type: "datetime", writer: "connector" },
  syncStatus: { type: "string", writer: "connector" },
})

const configKind = typeWith({
  clientId: { type: "string" },
  clientSecret: { type: "secret" },
  scopes: { type: "string", repeated: true },
  authorizationEndpoint: { type: "url" },
})

// The post-finding-1/8 shape: clientId/clientSecret required, no endpoints or
// provider scopes, and an enum cadence on the account.
const typedConfigKind = typeWith({
  clientId: { type: "string", required: true },
  clientSecret: { type: "secret", required: true },
})

const typedAccountType = typeWith({
  email: { type: "email", required: true, writer: "owner" },
  enabledContacts: { type: "bool", displayName: "Sync contacts" },
  syncFrequency: {
    type: "enum",
    values: [
      { value: "off", label: "Off" },
      { value: "hourly", label: "Every hour" },
      { value: "daily", label: "Once a day" },
    ],
    displayName: "Sync cadence",
    default: "daily",
  },
  backfillDepth: { type: "string" },
  tokenRef: { type: "secret", writer: "oauth" },
  tokenStatus: { type: "string", writer: "oauth" },
})

describe("buildFormFields", () => {
  it("keeps owner-writable, drops every writer!=owner property (by role, not name)", () => {
    const names = buildFormFields(accountType).map((f) => f.name)
    // Owner fields only: explicit `writer: owner` (email) and unset (toggles,
    // cadence) both survive; every host/connector-managed property is gone.
    expect(names).toEqual([
      "backfillDepth",
      "email",
      "enabledCalendar",
      "enabledContacts",
      "enabledGmail",
      "syncFrequency",
    ])
    for (const managed of [
      "tokenRef",
      "tokenStatus",
      "grantedScopes",
      "syncToken",
      "lastSyncedAt",
      "syncStatus",
    ]) {
      expect(names).not.toContain(managed)
    }
  })

  it("excludes email + grantedScopes once the schema marks them writer: oauth", () => {
    // The OAuth facility owns the account address and the granted scope set
    // (the substrate flips both to `writer: oauth`); the Add-account form must
    // not offer them.
    const oauthOwnedAccount = typeWith({
      email: { type: "email", writer: "oauth" },
      grantedScopes: { type: "string", repeated: true, writer: "oauth" },
      enabledContacts: { type: "bool" },
      syncFrequency: {
        type: "enum",
        values: [
          { value: "off", label: "" },
          { value: "hourly", label: "" },
          { value: "daily", label: "" },
        ],
      },
      backfillDepth: { type: "string" },
    })
    const names = buildFormFields(oauthOwnedAccount).map((f) => f.name)
    expect(names).not.toContain("email")
    expect(names).not.toContain("grantedScopes")
    // Only the owner-writable feature toggles + cadence + backfill remain.
    expect(names).toEqual(["backfillDepth", "enabledContacts", "syncFrequency"])
  })

  it("maps kinds to controls: secret→secret, repeated→list, bool→bool, email→text", () => {
    const byName = Object.fromEntries(
      buildFormFields(configKind).map((f) => [f.name, f])
    )
    expect(byName.clientSecret.control).toBe("secret")
    expect(byName.scopes.control).toBe("list")
    expect(byName.clientId.control).toBe("text")
    expect(byName.authorizationEndpoint.inputType).toBe("url")
    const acct = Object.fromEntries(
      buildFormFields(accountType).map((f) => [f.name, f])
    )
    expect(acct.enabledContacts.control).toBe("bool")
    expect(acct.email.inputType).toBe("email")
  })
})

describe("initialValues", () => {
  it("seeds text/bool/list from the record but NEVER a secret", () => {
    const fields = buildFormFields(configKind)
    const record = {
      properties: {
        clientId: "123.apps.googleusercontent.com",
        clientSecret: "should-never-appear",
        scopes: ["a", "b"],
      },
    } as unknown as SubstrateRecord
    const values = initialValues(fields, record)
    expect(values.clientId).toBe("123.apps.googleusercontent.com")
    expect(values.scopes).toBe("a\nb")
    expect(values.clientSecret).toBe("")
  })

  it("starts every field empty/false when creating (no record)", () => {
    const values = initialValues(buildFormFields(accountType))
    expect(values.email).toBe("")
    expect(values.enabledContacts).toBe(false)
  })
})

describe("toProperties", () => {
  it("account create: bools always sent, blank text omitted, email carried", () => {
    const fields = buildFormFields(accountType)
    const values = initialValues(fields)
    values.email = "alice@example.com"
    values.enabledContacts = true
    values.syncFrequency = "daily"
    expect(toProperties(fields, values)).toEqual({
      email: "alice@example.com",
      enabledContacts: true,
      enabledGmail: false,
      enabledCalendar: false,
      syncFrequency: "daily",
    })
  })

  it("config: a blank secret is omitted (the sealed value stands), scopes parse to a list", () => {
    const fields = buildFormFields(configKind)
    const values = initialValues(fields)
    values.clientId = "id"
    values.clientSecret = ""
    values.scopes = "scope-a\nscope-b, scope-c"
    const props = toProperties(fields, values)
    expect(props).not.toHaveProperty("clientSecret")
    expect(props.scopes).toEqual(["scope-a", "scope-b", "scope-c"])
    expect(props.clientId).toBe("id")
  })

  it("config: a filled secret IS sent", () => {
    const fields = buildFormFields(configKind)
    const values = initialValues(fields)
    values.clientSecret = "s3cr3t"
    expect(toProperties(fields, values).clientSecret).toBe("s3cr3t")
  })
})

describe("parseList", () => {
  it("splits on newlines and commas, trims, drops blanks", () => {
    expect(parseList("a\nb,  c \n\n , d")).toEqual(["a", "b", "c", "d"])
    expect(parseList("   ")).toEqual([])
  })
})

describe("schema-driven controls", () => {
  it("maps an enum property (declared values) to a select carrying its options", () => {
    const byName = Object.fromEntries(
      buildFormFields(typedAccountType).map((f) => [f.name, f])
    )
    expect(byName.syncFrequency.control).toBe("select")
    expect(byName.syncFrequency.options).toEqual([
      { value: "off", label: "Off" },
      { value: "hourly", label: "Every hour" },
      { value: "daily", label: "Once a day" },
    ])
    expect(byName.email.required).toBe(true)
    expect(byName.backfillDepth.required).toBe(false)
  })

  it("carries each option's authored label off the `{value, label}` wire shape", () => {
    const byName = Object.fromEntries(
      buildFormFields(typedAccountType).map((f) => [f.name, f])
    )
    // The raw value stays the option value; the label is the human name.
    const labels = Object.fromEntries(
      (byName.syncFrequency.options ?? []).map((o) => [o.value, o.label])
    )
    expect(labels).toEqual({
      off: "Off",
      hourly: "Every hour",
      daily: "Once a day",
    })
  })

  it("keeps an option's label empty when the wire carries no authored label", () => {
    const plainEnum = typeWith({
      cadence: {
        type: "enum",
        values: [
          { value: "off", label: "" },
          { value: "on", label: "" },
        ],
      },
    })
    const byName = Object.fromEntries(
      buildFormFields(plainEnum).map((f) => [f.name, f])
    )
    expect(byName.cadence.options).toEqual([
      { value: "off", label: "" },
      { value: "on", label: "" },
    ])
  })

  it("marks required fields from the declared `required: true`", () => {
    const byName = Object.fromEntries(
      buildFormFields(typedConfigKind).map((f) => [f.name, f])
    )
    expect(byName.clientId.required).toBe(true)
    expect(byName.clientSecret.required).toBe(true)
  })
})

describe("field labels", () => {
  it("uses the schema `displayName` when declared", () => {
    const byName = Object.fromEntries(
      buildFormFields(typedAccountType).map((f) => [f.name, f])
    )
    expect(byName.syncFrequency.label).toBe("Sync cadence")
    expect(byName.enabledContacts.label).toBe("Sync contacts")
  })

  it("humanizes the camelCase id when no `displayName` is declared", () => {
    const byName = Object.fromEntries(
      buildFormFields(typedAccountType).map((f) => [f.name, f])
    )
    expect(byName.backfillDepth.label).toBe("Backfill depth")
    expect(byName.email.label).toBe("Email")
  })
})

describe("humanizeName", () => {
  it("splits camelCase and sentence-cases — never the raw id", () => {
    expect(humanizeName("backfillDepth")).toBe("Backfill depth")
    expect(humanizeName("enabledCalendar")).toBe("Enabled calendar")
    expect(humanizeName("syncFrequency")).toBe("Sync frequency")
    expect(humanizeName("email")).toBe("Email")
  })

  it("keeps an ALL-CAPS run: an acronym is what the author wrote", () => {
    expect(humanizeName("baseURL")).toBe("Base URL")
    expect(humanizeName("clientID")).toBe("Client ID")
  })
})

describe("default seeding", () => {
  it("create: seeds an enum field from its declared `default`", () => {
    const values = initialValues(buildFormFields(typedAccountType))
    expect(values.syncFrequency).toBe("daily")
  })

  it("patch: seeds from the stored value, ignoring the declared default", () => {
    const fields = buildFormFields(typedAccountType)
    const record = {
      properties: { email: "alice@example.com", syncFrequency: "hourly" },
    } as unknown as SubstrateRecord
    const values = initialValues(fields, record)
    expect(values.syncFrequency).toBe("hourly")
  })

  it("patch: a stored-absent default field stays empty (default seeds create only)", () => {
    const fields = buildFormFields(typedAccountType)
    const record = {
      properties: { email: "alice@example.com" },
    } as unknown as SubstrateRecord
    const values = initialValues(fields, record)
    expect(values.syncFrequency).toBe("")
  })
})

describe("validate", () => {
  it("create: a blank secret is a hard error (an empty config must NOT configure)", () => {
    const fields = buildFormFields(typedConfigKind)
    const values = initialValues(fields) // clientId "", clientSecret ""
    const errors = validate(fields, values, "create")
    const byName = Object.fromEntries(errors.map((e) => [e.name, e.message]))
    expect(byName.clientId).toBeDefined()
    expect(byName.clientSecret).toBeDefined()
  })

  it("create: a required secret is required even without a declared flag", () => {
    // configKind.clientSecret has no `required`, but a create still demands it.
    const fields = buildFormFields(configKind)
    const values = initialValues(fields)
    values.clientId = "id"
    const errors = validate(fields, values, "create")
    expect(errors.map((e) => e.name)).toContain("clientSecret")
  })

  it("create: a blank required email account is rejected", () => {
    const fields = buildFormFields(typedAccountType)
    const values = initialValues(fields)
    const errors = validate(fields, values, "create")
    expect(errors.map((e) => e.name)).toContain("email")
  })

  it("create: a filled required config passes", () => {
    const fields = buildFormFields(typedConfigKind)
    const values = initialValues(fields)
    values.clientId = "123.apps.googleusercontent.com"
    values.clientSecret = "s3cr3t"
    expect(validate(fields, values, "create")).toEqual([])
  })

  it("patch: a blank secret is fine (it preserves the sealed value)", () => {
    const fields = buildFormFields(typedConfigKind)
    const values = initialValues(fields)
    values.clientId = "id"
    values.clientSecret = "" // unchanged
    expect(validate(fields, values, "patch")).toEqual([])
  })

  it("patch: clearing a required field (null) is rejected", () => {
    const fields = buildFormFields(typedAccountType)
    const values = initialValues(fields)
    values.email = null // explicit clear
    const errors = validate(fields, values, "patch")
    expect(errors.map((e) => e.name)).toContain("email")
  })
})

describe("toProperties explicit clear", () => {
  it("patch: an explicitly-cleared text field sends null to delete the key", () => {
    const fields = buildFormFields(typedAccountType)
    const values = initialValues(fields)
    values.email = "alice@example.com"
    values.backfillDepth = null // cleared
    const props = toProperties(fields, values, "patch")
    expect(props.backfillDepth).toBeNull()
  })

  it("create: a cleared/blank field is omitted (nothing to delete)", () => {
    const fields = buildFormFields(typedAccountType)
    const values = initialValues(fields)
    values.email = "alice@example.com"
    values.backfillDepth = null
    const props = toProperties(fields, values, "create")
    expect(props).not.toHaveProperty("backfillDepth")
  })

  it("patch: a merely-blank (untouched) field is omitted, not cleared", () => {
    const fields = buildFormFields(typedAccountType)
    const values = initialValues(fields)
    values.email = "alice@example.com"
    values.backfillDepth = "" // left blank, never touched
    const props = toProperties(fields, values, "patch")
    expect(props).not.toHaveProperty("backfillDepth")
  })
})

/** A reference is edited as two halves and STORED as one: the record path
 * `<kind>/<id>`. These hold the join, both directions. */
describe("reference fields", () => {
  const pointerKind = typeWith({
    owner: { type: "reference", kind: "people.substrate.reamde.dev/person" },
    callable: { type: "reference", kind: "any" },
  })
  const fieldsOf = () => buildFormFields(pointerKind)

  it("seeds a stored path back into the kind and the id it joins", () => {
    const record = {
      properties: { owner: "people.substrate.reamde.dev/person/alice" },
    } as unknown as SubstrateRecord
    expect(initialValues(fieldsOf(), record).owner).toEqual({
      kind: "people.substrate.reamde.dev/person",
      id: "alice",
    })
  })

  it("completes a value short of a path from the declaration's pin", () => {
    const record = {
      properties: { owner: "alice" },
    } as unknown as SubstrateRecord
    expect(initialValues(fieldsOf(), record).owner).toEqual({
      kind: "people.substrate.reamde.dev/person",
      id: "alice",
    })
  })

  it("REFUSES a value that reads two ways, naming both readings", () => {
    // Under a pin, a value that parses as a path whose kind is NOT the pin is
    // a pointer at that kind, or an id the pin would complete: two records,
    // and picking one silently is how a pointer ends up at the wrong row.
    const fields = fieldsOf()
    const values = initialValues(fields)
    values.owner = {
      kind: "people.substrate.reamde.dev/person",
      id: "foo.bar/baz/qux",
    }
    const errors = validate(fields, values, "create")
    expect(errors.map((e) => e.name)).toEqual(["owner"])
    expect(errors[0].message).toContain("ambiguous")
    expect(errors[0].message).toContain("foo.bar/baz")
    expect(toProperties(fields, values)).not.toHaveProperty("owner")
  })

  it("refuses an id of nothing, and keeps a slash-bearing short form", () => {
    const fields = fieldsOf()
    const values = initialValues(fields)
    values.owner = { kind: "people.substrate.reamde.dev/person", id: "target/" }
    expect(validate(fields, values, "create")[0].message).toMatch(
      /has an empty segment/
    )
    // A slash-bearing value that parses as NO path is the ordinary short form,
    // and the pin completes it. This is what a tool entry writes.
    values.owner = {
      kind: "people.substrate.reamde.dev/person",
      id: "web.example.com/page",
    }
    expect(validate(fields, values, "create")).toEqual([])
    expect(toProperties(fields, values).owner).toBe(
      "people.substrate.reamde.dev/person/web.example.com/page"
    )
  })

  it("submits the two halves as ONE path, and a pasted path unchanged", () => {
    const fields = fieldsOf()
    const values = initialValues(fields)
    values.owner = { kind: "people.substrate.reamde.dev/person", id: "alice" }
    values.callable = {
      kind: "",
      id: "core.substrate.reamde.dev/function/f",
    }
    expect(toProperties(fields, values)).toEqual({
      owner: "people.substrate.reamde.dev/person/alice",
      // A whole path typed into the record box is already the value; the kind
      // box has nothing left to add to it.
      callable: "core.substrate.reamde.dev/function/f",
    })
  })

  it("keeps a declaration id whole: the kind joins in front of its slash", () => {
    const fields = buildFormFields(
      typeWith({
        of: { type: "reference", kind: "core.substrate.reamde.dev/kind" },
      })
    )
    const values = initialValues(fields)
    values.of = {
      kind: "core.substrate.reamde.dev/kind",
      id: "tasks.substrate.reamde.dev/task",
    }
    expect(toProperties(fields, values).of).toBe(
      "core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task"
    )
  })

  it("refuses a bare id with no kind to complete it, in the engine's words", () => {
    const fields = fieldsOf()
    const values = initialValues(fields)
    values.callable = { kind: "", id: "f" }
    const errors = validate(fields, values, "create")
    expect(errors.map((e) => e.name)).toEqual(["callable"])
    expect(errors[0].message).toMatch(/full "<kind>\/<id>" path/)
    expect(toProperties(fields, values)).not.toHaveProperty("callable")
  })

  it("says nothing when no record is named", () => {
    const fields = fieldsOf()
    const values = initialValues(fields)
    expect(toProperties(fields, values)).toEqual({})
    expect(validate(fields, values, "create")).toEqual([])
  })
})
