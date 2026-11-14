/** The pure half of the record page's provenance: sources fold by mapping, dedupe by
 * record and sort by title; an actor string reads as the thing it is. */

import { describe, expect, it, beforeEach } from "vitest"
import { seedActors } from "@/lib/actors.test-fixtures"

import type { LinkedRecord, SubstrateRecord } from "@/lib/api/types"
import {
  contributesOf,
  groupSources,
  mappingLabel,
  mappingOfSource,
  sourceTitles,
  departsFromDefault,
  differsLabel,
  everyValueYours,
  holderOf,
  standingExplanation,
  standingLabel,
  standingOf,
  unionMembers,
} from "./provenance"
import { actorLink } from "@/lib/actors.test-fixtures"

beforeEach(() => seedActors())

const BEEPER = "providers.substrate.reamde.dev/beeper/user"
const GITHUB = "providers.substrate.reamde.dev/github/user"
const BEEPER_MAPPING = "ada.example.com/people/beeperuserperson"
const GITHUB_MAPPING = "ada.example.com/people/githubuserperson"

function link(
  kind: string,
  id: string,
  title: string,
  mapping: string
): LinkedRecord {
  return { ref: `${kind}/${id}`, kind, title, property: "person", mapping }
}

function mapping(
  id: string,
  title: string,
  from: string,
  map: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: "substrate.reamde.dev/core/recordmapping",
    properties: {
      title,
      description: `Converges ${from} onto a person.`,
      from: { ref: `substrate.reamde.dev/core/kind/${from}` },
      to: {
        ref: "substrate.reamde.dev/core/kind/ada.example.com/people/person",
      },
      property: "person",
      map,
    },
    labels: {},
    version: 3,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  }
}

describe("groupSources", () => {
  const links = [
    link(BEEPER, "u3", "Zed", BEEPER_MAPPING),
    link(BEEPER, "u1", "Ada", BEEPER_MAPPING),
    link(BEEPER, "u1", "Ada", BEEPER_MAPPING), // the index kept two rows
    link(BEEPER, "u2", "Ada", BEEPER_MAPPING), // same title, another record
    link(GITHUB, "gh1", "ada", GITHUB_MAPPING),
  ]
  const declared = [
    mapping(BEEPER_MAPPING, "beeperuserperson", BEEPER, {
      name: { path: "displayName" },
      phones: { path: "phone", merge: "union" },
    }),
    mapping(GITHUB_MAPPING, "githubuserperson", GITHUB, {
      name: { path: "name" },
      emails: { path: "email", merge: "union" },
    }),
  ]

  it("folds the links into one group per mapping, headed by the declaration", () => {
    const groups = groupSources(links, declared)
    expect(groups.map((g) => g.mapping)).toEqual([
      BEEPER_MAPPING,
      GITHUB_MAPPING,
    ])
    const beeper = groups[0]
    expect(beeper.title).toBe("beeperuserperson")
    expect(beeper.from).toBe(BEEPER)
    expect(beeper.property).toBe("person")
    expect(beeper.contributes).toEqual(["name", "phones"])
    expect(beeper.declared).toBe(true)
    expect(beeper.description).toContain("Converges")
  })

  it("deduplicates members by record and sorts them by title, then id", () => {
    const [beeper] = groupSources(links, declared)
    expect(beeper.members.map((m) => m.id)).toEqual(["u1", "u2", "u3"])
    expect(beeper.members.map((m) => m.title)).toEqual(["Ada", "Ada", "Zed"])
    expect(beeper.members[0].ref).toBe(`${BEEPER}/u1`)
  })

  it("still lists sources whose mapping declaration is missing, and says so", () => {
    const [group] = groupSources([links[4]], [])
    expect(group.declared).toBe(false)
    // The local name of the mapping identity stands in for its title, the
    // link's own kind for the declaration's `from`, and nothing is claimed
    // about what it contributes.
    expect(group.title).toBe("githubuserperson")
    expect(group.from).toBe(GITHUB)
    expect(group.contributes).toEqual([])
  })

  it("answers nothing for nothing", () => {
    expect(groupSources([], declared)).toEqual([])
  })
})

describe("contributesOf", () => {
  it("reads the keys of the map rules in declaration order", () => {
    expect(
      contributesOf(
        mapping(GITHUB_MAPPING, "x", GITHUB, {
          name: { path: "name" },
          displayName: { path: "login" },
          emails: { path: "email", merge: "union" },
        })
      )
    ).toEqual(["name", "displayName", "emails"])
  })
  it("is empty for a link-only mapping and for no mapping", () => {
    expect(contributesOf(mapping(GITHUB_MAPPING, "x", GITHUB, {}))).toEqual([])
    expect(contributesOf(undefined)).toEqual([])
  })
})

describe("the source lookups", () => {
  const links = [link(BEEPER, "u1", "Ada", BEEPER_MAPPING)]
  it("title a source record off the links the record carries", () => {
    expect(sourceTitles(links).get(`${BEEPER}/u1`)).toBe("Ada")
    expect(sourceTitles(links).get(`${BEEPER}/u9`)).toBeUndefined()
  })
  it("name the mapping a source came through", () => {
    expect(mappingOfSource(links, `${BEEPER}/u1`)).toBe(BEEPER_MAPPING)
    expect(mappingOfSource(links, `${GITHUB}/gh1`)).toBeUndefined()
  })
})

describe("mappingLabel", () => {
  const PERSON = "ada.example.com/people/person"
  it("reads a mapping by its title where it has one", () => {
    const decl = mapping(BEEPER_MAPPING, "Beeper people", BEEPER, {})
    expect(mappingLabel(BEEPER_MAPPING, [decl], BEEPER, PERSON)).toBe(
      "Beeper people"
    )
  })

  it("says what it joins where it has none", () => {
    expect(mappingLabel(GITHUB_MAPPING, [], GITHUB, PERSON)).toBe(
      "User → Person"
    )
  })
})

const GOOGLE_SYNC = "act-fn-google-synccontacts"

describe("holderOf", () => {
  it("names the owner's hand You, a provider by its name, anything else by its own", () => {
    expect(
      holderOf({ pinned: true, setBy: actorLink("act-person") })
    ).toMatchObject({
      mark: "you",
      label: "You",
    })
    expect(holderOf({ setBy: actorLink(GOOGLE_SYNC) })).toMatchObject({
      mark: "provider",
      label: "Google",
    })
    expect(
      holderOf({ pinned: true, setBy: actorLink("act-agent-llm-substrate") })
    ).toMatchObject({ mark: "actor", label: "Substrate" })
    expect(holderOf({})).toBeUndefined()
  })
})

describe("differsLabel", () => {
  const alt = (ref: string) => ({
    derivedFrom: { ref },
    value: "x",
    updatedAt: "",
  })
  const work = "providers.substrate.reamde.dev/google/contact/work-c123"
  const home = "providers.substrate.reamde.dev/google/contact/home-c900"
  const linear = "providers.substrate.reamde.dev/linear/issue/l1"
  it("names the one provider that disagrees, or counts several", () => {
    expect(differsLabel({})).toBeUndefined()
    expect(differsLabel({ alternatives: [alt(work)] })).toBe("Google differs")
    // Two accounts of one provider are two alternatives, and one provider.
    expect(differsLabel({ alternatives: [alt(work), alt(home)] })).toBe(
      "Google differs"
    )
    expect(differsLabel({ alternatives: [alt(work), alt(linear)] })).toBe(
      "2 sources differ"
    )
  })
})

describe("standingOf", () => {
  it("reads pinned and the hand from the mark, never from a tier", () => {
    const say = (mark: Parameters<typeof standingOf>[0]) =>
      standingLabel(standingOf(mark))
    expect(say({ pinned: true, setBy: actorLink("act-person") })).toBe("Yours")
    expect(say({ derivedFrom: [{ ref: `${GITHUB}/gh1` }] })).toBe("Synced")
    expect(say({ pinned: true, setBy: actorLink(GOOGLE_SYNC) })).toBe(
      "Set by provider"
    )
    expect(
      say({ pinned: true, setBy: actorLink("act-agent-llm-substrate") })
    ).toBe("Set by an agent")
    // A package's own write pins nothing, and says so.
    expect(say({ setBy: actorLink("act-pkg-google") })).toBe("Not pinned")
    expect(say(undefined)).toBe("Written")
  })
})

describe("standingExplanation", () => {
  // The detail's head already says "You set this"; the sentence under it
  // says what that means, not the same words again.
  it("never repeats who set the person's value", () => {
    expect(standingExplanation("yours")).not.toMatch(/you set this/i)
  })
})

describe("unionMembers", () => {
  it("names the sources whose offer carries each item", () => {
    const members = unionMembers(["a@example.com", "b@example.com"], {
      derivedFrom: [{ ref: `${BEEPER}/u1` }],
      alternatives: [
        {
          derivedFrom: { ref: `${GITHUB}/gh1` },
          value: ["b@example.com"],
          updatedAt: "",
        },
      ],
    })
    expect(members).toEqual([
      { item: "a@example.com", sources: [`${BEEPER}/u1`] },
      { item: "b@example.com", sources: [`${BEEPER}/u1`, `${GITHUB}/gh1`] },
    ])
  })

  it("says nothing for a scalar or when no source offers a list", () => {
    expect(unionMembers("x", { alternatives: [] })).toEqual([])
    expect(unionMembers(["x"], {})).toEqual([])
  })
})

describe("departsFromDefault", () => {
  it("is quiet for the owner's own value", () => {
    expect(
      departsFromDefault({ pinned: true, setBy: actorLink("act-person") })
    ).toBe(false)
    expect(departsFromDefault(undefined)).toBe(false)
  })

  it("speaks for a provider, an agent, or a source that differs", () => {
    expect(
      departsFromDefault({
        setBy: actorLink("act-fn-google-sync"),
      })
    ).toBe(true)
    expect(
      departsFromDefault({ setBy: actorLink("act-agent-llm-scribe") })
    ).toBe(true)
    expect(
      departsFromDefault({
        pinned: true,
        setBy: actorLink("act-person"),
        alternatives: [
          {
            derivedFrom: { ref: "x.example.com/p/k/1" },
            value: 1,
            updatedAt: "",
          },
        ],
      })
    ).toBe(true)
  })
})

describe("everyValueYours", () => {
  const rec = (
    properties: Record<string, unknown>,
    marks: SubstrateRecord["marks"]
  ): SubstrateRecord => ({
    id: "r",
    kind: "ada.example.com/tasks/task",
    properties,
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
    marks,
  })

  it("holds when every filled value is the owner's own", () => {
    expect(
      everyValueYours(
        rec(
          { name: "A", note: "" },
          {
            name: { pinned: true, setBy: actorLink("act-person") },
            note: { setBy: actorLink("act-agent-llm-scribe") },
          }
        )
      )
    ).toBe(true)
  })

  it("fails on one value somebody else holds", () => {
    expect(
      everyValueYours(
        rec(
          { name: "A", note: "B" },
          {
            name: { pinned: true, setBy: actorLink("act-person") },
            note: { setBy: actorLink("act-agent-llm-scribe") },
          }
        )
      )
    ).toBe(false)
  })

  it("claims nothing for a record that says nothing of its holders", () => {
    expect(everyValueYours(rec({ name: "A" }, undefined))).toBe(false)
  })
})
