import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import {
  describableSpan,
  keyDocsOf,
  linkableSpan,
  splitAround,
  type KeyDocs,
  type YamlLinkTargets,
} from "./yaml-annotations"

const docs: KeyDocs = {
  properties: {
    name: { type: "string", description: "the full name, one string" },
    authority: {
      type: "string",
      description: "a property that shares an envelope-ish name",
    },
    // a property the kind declares but never described: its TYPE is still an
    // answer, so it still earns a hover.
    raw: { type: "json" },
    memberOf: {
      type: "reference → organization[]",
      description: "the employer or workspace",
    },
  },
}

describe("describableSpan", () => {
  it("describes an indented property key", () => {
    expect(describableSpan("    name: Tasos", docs)).toEqual({
      text: "name",
      doc: { type: "string", description: "the full name, one string" },
    })
  })

  it("never describes envelope keys — depth guards the collision", () => {
    // A top-level key is the envelope's, even when a property shares the name.
    expect(
      describableSpan("authority: people.substrate.reamde.dev", docs)
    ).toBeNull()
    expect(describableSpan("    authority: x", docs)).toEqual({
      text: "authority",
      doc: {
        type: "string",
        description: "a property that shares an envelope-ish name",
      },
    })
  })

  it("describes a reference property by its key, like any other", () => {
    expect(describableSpan("    memberOf: org/o-1", docs)).toEqual({
      text: "memberOf",
      doc: {
        type: "reference → organization[]",
        description: "the employer or workspace",
      },
    })
    // `ref` is the reserved key INSIDE a reference value carrying link data,
    // never a property of the kind, so it earns no hover of its own.
    expect(describableSpan("      ref: org/o-1", docs)).toBeNull()
  })

  it("hovers a declared property that carries only a type", () => {
    expect(describableSpan("    raw: {}", docs)).toEqual({
      text: "raw",
      doc: { type: "json" },
    })
  })

  it("stays silent on undeclared keys and non-key lines", () => {
    expect(describableSpan("    other: 1", docs)).toBeNull()
    expect(describableSpan("      - plain item", docs)).toBeNull()
  })
})

describe("keyDocsOf", () => {
  const person: KindInfo = {
    identity: "people.substrate.reamde.dev/person",
    name: "person",
    authority: "people.substrate.reamde.dev",
    version: 0,
    plural: "people",
    source: "builtin",
    definition: {
      properties: {
        name: { type: "string", description: "the full name, one string" },
        emails: { type: "email", repeated: true },
        memberOf: {
          type: "reference",
          kind: "organization",
          repeated: true,
          description: "the employer",
        },
      },
    },
  }

  it("projects the declaration into type + one-liner, repeated marked", () => {
    const built = keyDocsOf(person)
    expect(built.properties.name).toEqual({
      type: "string",
      description: "the full name, one string",
    })
    expect(built.properties.emails).toEqual({
      type: "email[]",
      description: undefined,
    })
    expect(built.properties.memberOf).toEqual({
      type: "reference → organization[]",
      description: "the employer",
    })
  })

  it("is empty for a kind the registry has not resolved yet", () => {
    expect(keyDocsOf(undefined)).toEqual({ properties: {} })
  })
})

const targets: YamlLinkTargets = {
  ids: {
    "o-abc123": "/data/people.substrate.reamde.dev/organizations/o-abc123",
    "gcal-a@x.io-b@y.io":
      "/data/calendar.substrate.reamde.dev/calendars/gcal-a@x.io-b@y.io",
    // A reference property's value is keyed by the whole path, because that is
    // the text the document carries.
    "people.substrate.reamde.dev/person/p1":
      "/data/people.substrate.reamde.dev/people/p1",
  },
  kinds: {
    "people.substrate.reamde.dev/person":
      "/data/people.substrate.reamde.dev/people",
    "people.substrate.reamde.dev/organization":
      "/data/people.substrate.reamde.dev/organizations",
  },
}

describe("linkableSpan", () => {
  it("links a known id as the value of an id row", () => {
    expect(linkableSpan("      id: o-abc123", targets)).toEqual({
      text: "o-abc123",
      href: "/data/people.substrate.reamde.dev/organizations/o-abc123",
    })
    expect(linkableSpan("  canonicalId: o-abc123", targets)).toEqual({
      text: "o-abc123",
      href: "/data/people.substrate.reamde.dev/organizations/o-abc123",
    })
  })

  it("links @-bearing ids untouched", () => {
    expect(linkableSpan("      id: gcal-a@x.io-b@y.io", targets)).toEqual({
      text: "gcal-a@x.io-b@y.io",
      href: "/data/calendar.substrate.reamde.dev/calendars/gcal-a@x.io-b@y.io",
    })
  })

  it("links a kind reference value in its key position only", () => {
    // The top-level envelope `kind:` alone.
    expect(
      linkableSpan("kind: people.substrate.reamde.dev/person", targets)
    ).toEqual({
      text: "people.substrate.reamde.dev/person",
      href: "/data/people.substrate.reamde.dev/people",
    })
    expect(
      linkableSpan(
        "        kind: people.substrate.reamde.dev/organization",
        targets
      )
    ).toEqual({
      text: "people.substrate.reamde.dev/organization",
      href: "/data/people.substrate.reamde.dev/organizations",
    })
    // The same string as a plain value is a word, not a link.
    expect(
      linkableSpan("    title: people.substrate.reamde.dev/person", targets)
    ).toBeNull()
  })

  it("links a reference property's value under the property's own key", () => {
    // The key is whatever the declaration named the pointer, so the VALUE has
    // to say what it is: a record path the targets know.
    expect(
      linkableSpan("    owner: people.substrate.reamde.dev/person/p1", targets)
    ).toEqual({
      text: "people.substrate.reamde.dev/person/p1",
      href: "/data/people.substrate.reamde.dev/people/p1",
    })
    // A path nobody registered stays text, and so does a bare id in prose.
    expect(
      linkableSpan("    owner: people.substrate.reamde.dev/person/p9", targets)
    ).toBeNull()
    expect(linkableSpan("    title: o-abc123", targets)).toBeNull()
  })

  it("links manager and actor rows to the actor view — depth-guarded", () => {
    expect(
      linkableSpan(
        "      manager: people.google.bundles.substrate.reamde.dev",
        targets
      )
    ).toEqual({
      text: "people.google.bundles.substrate.reamde.dev",
      href: "/actors/people.google.bundles.substrate.reamde.dev",
    })
    expect(linkableSpan("        - actor: owner", targets)).toEqual({
      text: "owner",
      href: "/actors/owner",
    })
    // A data property named `actor` (depth 2) is data, not provenance.
    expect(linkableSpan("    actor: owner", targets)).toBeNull()
  })

  it("links a bare list item only when it is a known id (formerIds)", () => {
    expect(linkableSpan("    - o-abc123", targets)).toEqual({
      text: "o-abc123",
      href: "/data/people.substrate.reamde.dev/organizations/o-abc123",
    })
    expect(linkableSpan("    - not-a-known-id", targets)).toBeNull()
  })

  it("stays silent on unknown values and empty rows", () => {
    expect(linkableSpan("      id: unknown-id", targets)).toBeNull()
    expect(
      linkableSpan("kind: ghost.substrate.reamde.dev/thing", targets)
    ).toBeNull()
    expect(linkableSpan("    to:", targets)).toBeNull()
  })
})

describe("splitAround", () => {
  it("cuts the line into the runs an untinted render wraps", () => {
    expect(splitAround("    name: Tasos", ["name"])).toEqual([
      "    ",
      "name",
      ": Tasos",
    ])
  })

  it("cuts both annotations of one line, each exactly once", () => {
    expect(splitAround("      id: o-abc123", ["id", "o-abc123"])).toEqual([
      "      ",
      "id",
      ": ",
      "o-abc123",
    ])
  })

  it("takes the KEY, not the value that echoes it", () => {
    expect(splitAround("    name: name", ["name"])).toEqual([
      "    ",
      "name",
      ": name",
    ])
  })

  it("never cuts a mark out of the middle of a longer word", () => {
    // `id` lives inside `candidate` and inside `o-id-1`: neither is the span.
    expect(splitAround("    candidate: o-id-1", ["id"])).toEqual([
      "    candidate: o-id-1",
    ])
  })

  it("passes a line with nothing to mark straight through", () => {
    expect(splitAround("data:", [undefined])).toEqual(["data:"])
    expect(splitAround("", [])).toEqual([""])
  })
})
