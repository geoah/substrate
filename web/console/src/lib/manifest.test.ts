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
  kind: "people.substrate.reamde.dev/person",
  properties: {
    name: "Tasos Aggelis",
    phones: ["+306973328908"],
    prominence: "known",
    title: "Tasos Aggelis",
  },
  labels: {},
  version: 3,
  createdAt: "2026-08-05T16:26:27.161544Z",
  updatedAt: "2026-08-05T16:26:27.310967Z",
  edges: {
    memberOf: [
      {
        id: "org1",
        kind: "people.substrate.reamde.dev/organization",
        title: "Acme",
      },
    ],
  },
  propertyMeta: {
    name: {
      manager: "people.google.bundles.substrate.reamde.dev",
      tier: "machine",
      updatedAt: "2026-08-05T16:26:27.161544Z",
    },
  },
}

describe("manifestOf", () => {
  it("folds the wire record back into the v1 document envelope", () => {
    const m = manifestOf(record)
    expect(Object.keys(m)).toEqual(["kind", "metadata", "data", "status"])
    expect(m.kind).toBe("people.substrate.reamde.dev/person")
    expect(m.metadata).toEqual({ id: "32llel6yd5bs" })
  })

  it("renders edges as rel/to rows carrying the whole {kind, id} reference", () => {
    const data = manifestOf(record).data as Record<string, unknown>
    expect(data.edges).toEqual([
      {
        rel: "memberOf",
        to: { kind: "people.substrate.reamde.dev/organization", id: "org1" },
      },
    ])
  })

  it("keeps status server-owned: version, stamps, §7.1 provenance", () => {
    const status = manifestOf(record).status as Record<string, unknown>
    expect(status.version).toBe(3)
    expect(status.properties).toBe(record.propertyMeta)
    expect(status).not.toHaveProperty("deletedAt")
  })

  it("omits empty labels (metadata) and empty edges (data) rather than printing {}", () => {
    const bare = manifestOf({ ...record, edges: {}, labels: {} })
    const metadata = bare.metadata as Record<string, unknown>
    const data = bare.data as Record<string, unknown>
    expect(metadata).not.toHaveProperty("labels")
    expect(data).not.toHaveProperty("edges")
  })
})

describe("manifestYAML", () => {
  it("serializes the envelope in document order", () => {
    const yaml = manifestYAML(record)
    const lines = yaml.split("\n")
    expect(lines[0]).toBe("kind: people.substrate.reamde.dev/person")
    expect(yaml.indexOf("metadata:")).toBeLessThan(yaml.indexOf("data:"))
    expect(yaml.indexOf("data:")).toBeLessThan(yaml.indexOf("status:"))
    expect(yaml).toContain("prominence: known")
    expect(yaml).toContain(
      "manager: people.google.bundles.substrate.reamde.dev"
    )
  })
})

const registry: KindInfo[] = [
  {
    identity: "people.substrate.reamde.dev/person",
    name: "person",
    authority: "people.substrate.reamde.dev",
    version: "",
    plural: "people",
    source: "builtin",
  },
  {
    identity: "people.substrate.reamde.dev/organization",
    name: "organization",
    authority: "people.substrate.reamde.dev",
    version: "",
    plural: "organizations",
    source: "builtin",
  },
  {
    identity: "calendar.substrate.reamde.dev/calendarevent",
    name: "calendarevent",
    authority: "calendar.substrate.reamde.dev",
    version: "",
    plural: "calendarevents",
    source: "builtin",
  },
]

describe("linkTargetsOf", () => {
  it("maps edge target ids and the referenced kind refs", () => {
    const t = linkTargetsOf(record, registry)
    expect(t.ids.org1).toBe(
      "/data/people.substrate.reamde.dev/organizations/org1"
    )
    expect(t.kinds["people.substrate.reamde.dev/organization"]).toBe(
      "/data/people.substrate.reamde.dev/organizations"
    )
    expect(t.kinds["people.substrate.reamde.dev/person"]).toBe(
      "/data/people.substrate.reamde.dev/people"
    )
  })

  it("maps canonicalId and formerIds into the record's own collection", () => {
    const t = linkTargetsOf(
      { ...record, canonicalId: "canon1", formerIds: ["old-a@x.io"] },
      registry
    )
    expect(t.ids.canon1).toBe("/data/people.substrate.reamde.dev/people/canon1")
    expect(t.ids["old-a@x.io"]).toBe(
      "/data/people.substrate.reamde.dev/people/old-a@x.io"
    )
  })

  it("knows every registry kind by its reference", () => {
    const t = linkTargetsOf(record, registry)
    expect(t.kinds["calendar.substrate.reamde.dev/calendarevent"]).toBe(
      "/data/calendar.substrate.reamde.dev/calendarevents"
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
              to: "people.substrate.reamde.dev/person",
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
        manager: "people.substrate.reamde.dev/person/boss1",
      },
    }
    const t = linkTargetsOf(pointing, withPointer)
    expect(t.ids["people.substrate.reamde.dev/person/boss1"]).toBe(
      "/data/people.substrate.reamde.dev/people/boss1"
    )
    // The id alone never appears in the document, so it is not a target.
    expect(t.ids.boss1).toBeUndefined()
  })

  it("does not link an edge target whose kind is not in the registry", () => {
    const stray: SubstrateRecord = {
      ...record,
      edges: {
        memberOf: [
          { id: "org2", kind: "crm.substrate.reamde.dev/organization" },
        ],
      },
    }
    const t = linkTargetsOf(stray, registry)
    expect(t.kinds["crm.substrate.reamde.dev/organization"]).toBeUndefined()
    // The id is only linked once its kind resolves — an unknown kind carries none.
    expect(t.ids.org2).toBeUndefined()
  })
})

/** The kind DEFINITION view's document: the same envelope as a record's, with
 * the meta-kind on top and the declaration as `data` — what a schema file
 * authors, re-rendered from the stored (key-order-less) definition. */
const llmprovider: KindInfo = {
  identity: "core.substrate.reamde.dev/llmprovider",
  name: "llmprovider",
  authority: "core.substrate.reamde.dev",
  version: "1",
  plural: "llmproviders",
  source: "builtin",
  definition: {
    // deliberately scrambled: jsonb lost the authored order.
    properties: { wire: { type: "string" } },
    names: { singular: "llmprovider", plural: "llmproviders" },
    displayTemplate: "{name}",
    authority: "core.substrate.reamde.dev",
    zzExtra: true,
  },
}

describe("kindManifestOf", () => {
  it("wraps the declaration in the meta-kind envelope, id = the reference", () => {
    const m = kindManifestOf(llmprovider)
    expect(Object.keys(m)).toEqual(["kind", "metadata", "data"])
    expect(m.kind).toBe("core.substrate.reamde.dev/kind")
    expect(m.metadata).toEqual({ id: "core.substrate.reamde.dev/llmprovider" })
  })

  it("re-imposes the authored reading order, unknown keys last", () => {
    const data = kindManifestOf(llmprovider).data as Record<string, unknown>
    expect(Object.keys(data)).toEqual([
      "authority",
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
    expect(yaml.split("\n")[0]).toBe("kind: core.substrate.reamde.dev/kind")
    expect(yaml).toContain("id: core.substrate.reamde.dev/llmprovider")
    expect(yaml.indexOf("authority:")).toBeLessThan(yaml.indexOf("properties:"))
    expect(yaml).toContain("    type: string")
  })
})

describe("kindLinkTargets", () => {
  it("knows every registry kind and claims no record ids", () => {
    const t = kindLinkTargets(registry)
    expect(t.kinds["people.substrate.reamde.dev/person"]).toBe(
      "/data/people.substrate.reamde.dev/people"
    )
    expect(t.kinds["calendar.substrate.reamde.dev/calendarevent"]).toBe(
      "/data/calendar.substrate.reamde.dev/calendarevents"
    )
    expect(t.ids).toEqual({})
  })
})
