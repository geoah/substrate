import { describe, expect, it } from "vitest"

import type { SubstrateRecord, KindInfo } from "@/lib/api/types"
import {
  kindLinkTargets,
  kindManifestOf,
  kindManifestYAML,
  linkTargetsOf,
  manifestOf,
  manifestYAML,
} from "./manifest"

const record: SubstrateRecord = {
  id: "32llel6yd5bs",
  kind: "samples.substrate.reamde.dev/people/person",
  properties: {
    name: "Tasos Aggelis",
    phones: ["+306973328908"],
    prominence: "known",
    title: "Tasos Aggelis",
    // A reference carrying LINK DATA: the pointer under `ref`, the link's own
    // properties beside it.
    memberOf: [
      {
        ref: "samples.substrate.reamde.dev/people/organization/org1",
        role: "founder",
      },
    ],
  },
  labels: {},
  version: 3,
  createdAt: "2026-08-05T16:26:27.161544Z",
  updatedAt: "2026-08-05T16:26:27.310967Z",
  propertyMeta: {
    name: {
      manager: "people.providers.substrate.reamde.dev/google",
      tier: "machine",
      updatedAt: "2026-08-05T16:26:27.161544Z",
    },
  },
}

describe("manifestOf", () => {
  it("folds the wire record back into the v1 document envelope", () => {
    const m = manifestOf(record)
    expect(Object.keys(m)).toEqual(["kind", "metadata", "data", "status"])
    expect(m.kind).toBe("samples.substrate.reamde.dev/people/person")
    expect(m.metadata).toEqual({ id: "32llel6yd5bs" })
  })

  it("carries a pointer inside properties, never a block of its own", () => {
    const data = manifestOf(record).data as Record<string, unknown>
    expect(data).not.toHaveProperty("edges")
    const properties = data.properties as Record<string, unknown>
    expect(properties.memberOf).toEqual([
      {
        ref: "samples.substrate.reamde.dev/people/organization/org1",
        role: "founder",
      },
    ])
  })

  it("keeps status server-owned: version, stamps, §7.1 provenance", () => {
    const status = manifestOf(record).status as Record<string, unknown>
    expect(status.version).toBe(3)
    expect(status.properties).toBe(record.propertyMeta)
    expect(status).not.toHaveProperty("deletedAt")
  })

  it("omits empty labels and an empty data block rather than printing {}", () => {
    const bare = manifestOf({ ...record, properties: {}, labels: {} })
    const metadata = bare.metadata as Record<string, unknown>
    expect(metadata).not.toHaveProperty("labels")
    expect(bare).not.toHaveProperty("data")
  })
})

describe("manifestYAML", () => {
  it("serializes the envelope in document order", () => {
    const yaml = manifestYAML(record)
    const lines = yaml.split("\n")
    expect(lines[0]).toBe("kind: samples.substrate.reamde.dev/people/person")
    expect(yaml.indexOf("metadata:")).toBeLessThan(yaml.indexOf("data:"))
    expect(yaml.indexOf("data:")).toBeLessThan(yaml.indexOf("status:"))
    expect(yaml).toContain("prominence: known")
    expect(yaml).toContain(
      "manager: people.providers.substrate.reamde.dev/google"
    )
  })
})

const registry: KindInfo[] = [
  {
    identity: "samples.substrate.reamde.dev/people/person",
    name: "person",
    authority: "samples.substrate.reamde.dev",
    package: "people",
    version: 0,
    plural: "people",
    source: "builtin",
  },
  {
    identity: "samples.substrate.reamde.dev/people/organization",
    name: "organization",
    authority: "samples.substrate.reamde.dev",
    package: "people",
    version: 0,
    plural: "organizations",
    source: "builtin",
  },
  {
    identity: "samples.substrate.reamde.dev/calendar/calendarevent",
    name: "calendarevent",
    authority: "samples.substrate.reamde.dev",
    package: "calendar",
    version: 0,
    plural: "calendarevents",
    source: "builtin",
  },
]

describe("linkTargetsOf", () => {
  it("maps every registry kind by its reference", () => {
    const t = linkTargetsOf(record, registry)
    expect(t.kinds["samples.substrate.reamde.dev/people/organization"]).toBe(
      "/data/samples.substrate.reamde.dev/people/organization"
    )
    expect(t.kinds["samples.substrate.reamde.dev/people/person"]).toBe(
      "/data/samples.substrate.reamde.dev/people/person"
    )
    // The bare id is never in the document — a reference carries the whole
    // path — so nothing links it.
    expect(t.ids.org1).toBeUndefined()
  })

  it("maps canonicalId and formerIds into the record's own collection", () => {
    const t = linkTargetsOf(
      { ...record, canonicalId: "canon1", formerIds: ["old-a@x.io"] },
      registry
    )
    expect(t.ids.canon1).toBe(
      "/data/samples.substrate.reamde.dev/people/person/canon1"
    )
    expect(t.ids["old-a@x.io"]).toBe(
      "/data/samples.substrate.reamde.dev/people/person/old-a@x.io"
    )
  })

  it("knows every registry kind by its reference", () => {
    const t = linkTargetsOf(record, registry)
    expect(t.kinds["samples.substrate.reamde.dev/calendar/calendarevent"]).toBe(
      "/data/samples.substrate.reamde.dev/calendar/calendarevent"
    )
  })

  it("deep-links a reference property by the whole path the document carries", () => {
    const withPointer: KindInfo[] = [
      {
        ...registry[0],
        definition: {
          properties: {
            manager: {
              type: "reference",
              kind: "samples.substrate.reamde.dev/people/person",
            },
          },
        },
      },
      ...registry.slice(1),
    ]
    const pointing: SubstrateRecord = {
      ...record,
      properties: {
        ...record.properties,
        manager: "samples.substrate.reamde.dev/people/person/boss1",
      },
    }
    const t = linkTargetsOf(pointing, withPointer)
    expect(t.ids["samples.substrate.reamde.dev/people/person/boss1"]).toBe(
      "/data/samples.substrate.reamde.dev/people/person/boss1"
    )
    // The id alone never appears in the document, so it is not a target.
    expect(t.ids.boss1).toBeUndefined()
  })

  it("links a reference carrying link data by the path under `ref`", () => {
    const withPointer: KindInfo[] = [
      {
        ...registry[0],
        definition: {
          properties: {
            memberOf: {
              type: "reference",
              kind: "samples.substrate.reamde.dev/people/organization",
              repeated: true,
              properties: { role: { type: "string" } },
            },
          },
        },
      },
      ...registry.slice(1),
    ]
    const t = linkTargetsOf(record, withPointer)
    expect(t.ids["samples.substrate.reamde.dev/people/organization/org1"]).toBe(
      "/data/samples.substrate.reamde.dev/people/organization/org1"
    )
  })

  it("does not link a reference whose kind is not in the registry", () => {
    const stray: SubstrateRecord = {
      ...record,
      properties: {
        ...record.properties,
        manager: "crm.substrate.reamde.dev/organization/org2",
      },
    }
    const withPointer: KindInfo[] = [
      {
        ...registry[0],
        definition: {
          properties: { manager: { type: "reference" } },
        },
      },
      ...registry.slice(1),
    ]
    const t = linkTargetsOf(stray, withPointer)
    expect(t.kinds["crm.substrate.reamde.dev/organization"]).toBeUndefined()
    // The path is only linked once its kind resolves.
    expect(t.ids["crm.substrate.reamde.dev/organization/org2"]).toBeUndefined()
  })
})

/** The kind DEFINITION view's document: the same envelope as a record's, with
 * the meta-kind on top and the declaration as `data` — what a schema file
 * authors, re-rendered from the stored (key-order-less) definition. */
const llmprovider: KindInfo = {
  identity: "substrate.reamde.dev/core/llmprovider",
  name: "llmprovider",
  authority: "substrate.reamde.dev",
  package: "core",
  version: 1,
  plural: "llmproviders",
  source: "builtin",
  definition: {
    // deliberately scrambled: jsonb lost the authored order.
    properties: { wire: { type: "string" } },
    names: { singular: "llmprovider", plural: "llmproviders" },
    displayTemplate: "{name}",
    authority: "substrate.reamde.dev",
    package: "core",
    zzExtra: true,
  },
}

describe("kindManifestOf", () => {
  it("wraps the declaration in the meta-kind envelope, id = the reference", () => {
    const m = kindManifestOf(llmprovider)
    expect(Object.keys(m)).toEqual(["kind", "metadata", "data"])
    expect(m.kind).toBe("substrate.reamde.dev/core/kind")
    expect(m.metadata).toEqual({ id: "substrate.reamde.dev/core/llmprovider" })
  })

  it("re-imposes the authored reading order, unknown keys last", () => {
    const data = kindManifestOf(llmprovider).data as Record<string, unknown>
    expect(Object.keys(data)).toEqual([
      "authority",
      "package",
      "names",
      "displayTemplate",
      "properties",
      "zzExtra",
    ])
  })

  it("renders an empty data map for a kind with no stored declaration", () => {
    const bare = kindManifestOf({ ...llmprovider, definition: undefined })
    expect(bare.data).toEqual({})
  })
})

describe("kindManifestYAML", () => {
  it("serializes the declaration in document order", () => {
    const yaml = kindManifestYAML(llmprovider)
    expect(yaml.split("\n")[0]).toBe("kind: substrate.reamde.dev/core/kind")
    expect(yaml).toContain("id: substrate.reamde.dev/core/llmprovider")
    expect(yaml.indexOf("authority:")).toBeLessThan(yaml.indexOf("properties:"))
    expect(yaml).toContain("    type: string")
  })
})

describe("kindLinkTargets", () => {
  it("knows every registry kind and claims no record ids", () => {
    const t = kindLinkTargets(registry)
    expect(t.kinds["samples.substrate.reamde.dev/people/person"]).toBe(
      "/data/samples.substrate.reamde.dev/people/person"
    )
    expect(t.kinds["samples.substrate.reamde.dev/calendar/calendarevent"]).toBe(
      "/data/samples.substrate.reamde.dev/calendar/calendarevent"
    )
    expect(t.ids).toEqual({})
  })
})
