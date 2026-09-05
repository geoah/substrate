/** The bundles page folds two reads — the installed bundles' runtime status
 * and the shipped catalog — into one id-keyed row set. Installed wins the
 * counts, available closures surface first (they invite an action), and a
 * bundle present in both carries both. The TIER comes from the backend field
 * and decides the section and the door; a sample folds by the id it LANDS
 * under. The provider-copy gate reads the bundle's own declared traits. The requirement fold answers the one question the reader has
 * before importing into a fresh (core-only) repository: is anything this
 * closure declares against still missing, and what must be imported first. */

import { describe, expect, it } from "vitest"

import type { BundleStatus } from "@/lib/api/bundles"
import type { CatalogItem } from "@/lib/api/catalog"
import { ApiError, type KindInfo } from "@/lib/api/types"
import {
  accountKindOf,
  bundleRecordRows,
  bundleSections,
  declaresProviderInterfaces,
  importFailureText,
  installedKindRows,
  mergeBundles,
  missingRequirements,
  oauthConnectBlocked,
  presentPackages,
  requirementsOf,
  requiresHint,
  suggestedMappingHint,
  suggestedMappingsOf,
  upgradableBundleCount,
  upgradeBlocked,
  upgradeMotion,
} from "./bundles"

function status(over: Partial<BundleStatus> = {}): BundleStatus {
  return {
    id: "providers.substrate.reamde.dev/google",
    name: "google",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    installed: true,
    enabled: true,
    inputs: [
      {
        name: "client",
        kind: "providers.substrate.reamde.dev/google/config",
        record: "default",
        via: "default",
      },
    ],
    accounts: 2,
    functions: 1,
    kinds: 3,
    liveRecords: 42,
    ...over,
  }
}

function catalog(over: Partial<CatalogItem> = {}): CatalogItem {
  return {
    id: "providers.substrate.reamde.dev/google",
    name: "google",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    description: "Connects a Google account.",
    version: 1,
    inputs: {
      client: { kind: "providers.substrate.reamde.dev/google/config" },
    },
    closure: { kinds: ["a", "b"], functions: ["c"] },
    installed: false,
    tier: "provider",
    ...over,
  }
}

function kindInfo(over: Partial<KindInfo> = {}): KindInfo {
  return {
    identity: "samples.substrate.reamde.dev/people/person",
    name: "person",
    authority: "samples.substrate.reamde.dev",
    package: "people",
    version: 0,
    plural: "persons",
    source: "builtin",
    ...over,
  }
}

describe("mergeBundles", () => {
  it("folds a bundle in both reads into one row carrying both, status winning", () => {
    const rows = mergeBundles([status()], [catalog({ installed: true })])
    expect(rows).toHaveLength(1)
    expect(rows[0].status?.liveRecords).toBe(42)
    expect(rows[0].catalog?.description).toBe("Connects a Google account.")
    expect(rows[0].installed).toBe(true)
    expect(rows[0].authority).toBe("providers.substrate.reamde.dev")
    expect(rows[0].package).toBe("google")
  })

  it("keeps available closures and installed-not-in-catalog bundles both", () => {
    const rows = mergeBundles(
      [status({ id: "custom.example.com/custom", name: "custom" })],
      [catalog({ id: "slack.example.com/slack", name: "slack" })]
    )
    const byId = Object.fromEntries(rows.map((r) => [r.id, r]))
    expect(byId["custom.example.com/custom"].installed).toBe(true)
    expect(byId["slack.example.com/slack"].installed).toBe(false)
    expect(byId["slack.example.com/slack"].status).toBeUndefined()
  })

  it("orders available before installed", () => {
    const rows = mergeBundles(
      [status({ id: "b.example.com/b" })],
      [catalog({ id: "a.example.com/a", installed: false })]
    )
    expect(rows.map((r) => r.installed)).toEqual([false, true])
  })

  it("reads the tier from the catalog entry", () => {
    const rows = mergeBundles(
      [],
      [
        catalog({ id: "providers.substrate.reamde.dev/google" }),
        catalog({
          id: "samples.substrate.reamde.dev/tasks",
          name: "tasks",
          authority: "samples.substrate.reamde.dev",
          package: "tasks",
          tier: "sample",
        }),
      ],
      "ada.example.com"
    )
    const byId = Object.fromEntries(rows.map((r) => [r.id, r]))
    expect(byId["providers.substrate.reamde.dev/google"].tier).toBe("provider")
    expect(byId["ada.example.com/tasks"].tier).toBe("sample")
  })

  it("keys a sample by the id it LANDS under, so its status folds onto it", () => {
    const rows = mergeBundles(
      [
        status({
          id: "ada.example.com/tasks",
          name: "tasks",
          authority: "ada.example.com",
          package: "tasks",
        }),
      ],
      [
        catalog({
          id: "samples.substrate.reamde.dev/tasks",
          name: "tasks",
          authority: "samples.substrate.reamde.dev",
          package: "tasks",
          tier: "sample",
        }),
      ],
      "ada.example.com"
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].id).toBe("ada.example.com/tasks")
    // The shipped id stays reachable: it is what the import door is called with.
    expect(rows[0].catalog?.id).toBe("samples.substrate.reamde.dev/tasks")
    expect(rows[0].installed).toBe(true)
  })

  it("folds a VERBATIM-installed sample onto its own catalog row", () => {
    // The sample was installed rather than imported, so it is held under the
    // SHIPPED id. Keying the row by the rehomed id alone showed it twice: once
    // as an untaken sample, once as an applied-directly bundle.
    const rows = mergeBundles(
      [
        status({
          id: "samples.substrate.reamde.dev/tasks",
          name: "tasks",
          authority: "samples.substrate.reamde.dev",
          package: "tasks",
        }),
      ],
      [
        catalog({
          id: "samples.substrate.reamde.dev/tasks",
          name: "tasks",
          authority: "samples.substrate.reamde.dev",
          package: "tasks",
          tier: "sample",
          installed: true,
          closure: { kinds: ["samples.substrate.reamde.dev/tasks/task"] },
        }),
      ],
      "ada.example.com"
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].id).toBe("samples.substrate.reamde.dev/tasks")
    expect(rows[0].tier).toBe("sample")
    expect(rows[0].installed).toBe(true)
    // Its closure is NOT rehomed: the kinds it holds are under the authority
    // the tree spells, so a rehomed preview would link nowhere.
    expect(rows[0].catalog?.closure.kinds).toEqual([
      "samples.substrate.reamde.dev/tasks/task",
    ])
  })

  it("rehomes a sample's requires, because that is what the server looks for", () => {
    const rows = mergeBundles(
      [],
      [
        catalog({
          id: "samples.substrate.reamde.dev/tasks",
          name: "tasks",
          authority: "samples.substrate.reamde.dev",
          package: "tasks",
          tier: "sample",
          requires: [
            "samples.substrate.reamde.dev/people",
            "substrate.reamde.dev/core",
          ],
        }),
      ],
      "ada.example.com"
    )
    expect(rows[0].requires).toEqual([
      "ada.example.com/people",
      "substrate.reamde.dev/core",
    ])
  })

  it("leaves a provider's id and requires exactly as published", () => {
    const rows = mergeBundles(
      [],
      [catalog({ requires: ["samples.substrate.reamde.dev/people"] })],
      "ada.example.com"
    )
    expect(rows[0].id).toBe("providers.substrate.reamde.dev/google")
    expect(rows[0].requires).toEqual(["samples.substrate.reamde.dev/people"])
  })

  it("an applied-only bundle claims no tier: the catalog states it, nothing derives it", () => {
    const rows = mergeBundles([status({ id: "x.example.com/x" })], [])
    expect(rows[0].tier).toBeUndefined()
    expect(rows[0].requires).toEqual([])
  })
})

describe("bundleSections", () => {
  const rows = mergeBundles(
    [status({ id: "x.example.com/x", name: "x" })],
    [
      catalog({ id: "providers.substrate.reamde.dev/google" }),
      catalog({
        id: "samples.substrate.reamde.dev/tasks",
        name: "tasks",
        authority: "samples.substrate.reamde.dev",
        package: "tasks",
        tier: "sample",
      }),
    ],
    "ada.example.com"
  )

  it("splits the rows by tier and lists an untiered bundle on its own", () => {
    const sections = bundleSections(rows)
    expect(sections.providers.map((r) => r.id)).toEqual([
      "providers.substrate.reamde.dev/google",
    ])
    expect(sections.samples.map((r) => r.id)).toEqual(["ada.example.com/tasks"])
    expect(sections.applied.map((r) => r.id)).toEqual(["x.example.com/x"])
  })
})

describe("the upgrade preview helpers", () => {
  it("counts installed bundles whose closure moved, and only those", () => {
    expect(
      upgradableBundleCount([
        catalog({ id: "a", installed: true }),
        catalog({
          id: "b",
          installed: true,
          upgrade: { available: true, to: 2 },
        }),
        // Not installed: nothing to upgrade, whatever the preview would say.
        catalog({ id: "c", installed: false }),
        // Blocked still counts: it needs the reader's hand.
        catalog({
          id: "d",
          installed: true,
          upgrade: { available: true, to: 2, blockers: ["live rows"] },
        }),
      ])
    ).toBe(2)
  })

  it("blocked means available AND the server named blockers", () => {
    expect(upgradeBlocked({ upgrade: undefined })).toBe(false)
    expect(upgradeBlocked({ upgrade: { available: true, to: 2 } })).toBe(false)
    expect(
      upgradeBlocked({
        upgrade: { available: true, to: 2, blockers: ["a guard line"] },
      })
    ).toBe(true)
  })

  it("renders the version motion, tolerating a store with no version", () => {
    expect(upgradeMotion({ available: true, from: 1, to: 2 })).toBe("1 → 2")
    expect(upgradeMotion({ available: true, to: 2 })).toBe("2")
    // 0 is the wire's absent (omitempty), so it reads exactly like undefined.
    expect(upgradeMotion({ available: true, from: 0, to: 2 })).toBe("2")
    expect(upgradeMotion({ available: true, from: 0, to: 0 })).toBe("")
  })

  it("states one version when the authority did not move", () => {
    // A kind's own bump, or a kind the closure ADDED, upgrades without the
    // authority version moving — both legal. "3 → 3" would read as a bug, so
    // it collapses to the version itself.
    expect(upgradeMotion({ available: true, from: 1, to: 1 })).toBe("1")
  })
})

describe("presentAuthorities — what this repository already holds", () => {
  it("counts an imported bundle's authority and every reconciled kind's", () => {
    const rows = mergeBundles(
      [
        status({
          id: "samples.substrate.reamde.dev/people",
          authority: "samples.substrate.reamde.dev",
          package: "people",
        }),
      ],
      [
        catalog({
          id: "samples.substrate.reamde.dev/people",
          authority: "samples.substrate.reamde.dev",
          package: "people",
          installed: true,
        }),
        catalog({
          id: "providers.substrate.reamde.dev/google",
          installed: false,
        }),
      ]
    )
    const present = presentPackages(rows, [
      kindInfo({
        identity: "substrate.reamde.dev/core/bundle",
        authority: "substrate.reamde.dev",
        package: "core",
      }),
    ])
    expect(present.has("samples.substrate.reamde.dev/people")).toBe(true)
    expect(present.has("substrate.reamde.dev/core")).toBe(true)
    // Shipped in the catalog but never imported — the schema is not there.
    expect(present.has("providers.substrate.reamde.dev/google")).toBe(false)
  })

  it("does not count an uninstalled or quarantined bundle's package", () => {
    const rows = mergeBundles(
      [
        status({
          id: "samples.substrate.reamde.dev/people",
          authority: "samples.substrate.reamde.dev",
          package: "people",
          installed: false,
          enabled: false,
        }),
      ],
      []
    )
    expect(
      presentPackages(rows).has("samples.substrate.reamde.dev/people")
    ).toBe(false)
  })
})

describe("requirementsOf / requiresHint — what to import first", () => {
  const present = new Set([
    "samples.substrate.reamde.dev/people",
    "substrate.reamde.dev/core",
  ])

  it("marks each declared package present or missing, in declaration order", () => {
    const reqs = requirementsOf(
      {
        requires: [
          "samples.substrate.reamde.dev/people",
          "samples.substrate.reamde.dev/messaging",
        ],
      },
      present
    )
    expect(reqs).toEqual([
      { package: "samples.substrate.reamde.dev/people", present: true },
      { package: "samples.substrate.reamde.dev/messaging", present: false },
    ])
    expect(missingRequirements(reqs).map((r) => r.package)).toEqual([
      "samples.substrate.reamde.dev/messaging",
    ])
  })

  it("a closure that declares against nothing is never blocked", () => {
    expect(requirementsOf({ requires: [] }, new Set())).toEqual([])
    expect(requiresHint([])).toBe("")
  })

  it("names one missing package, then several, the way the server names them", () => {
    expect(
      requiresHint(
        missingRequirements(
          requirementsOf(
            { requires: ["samples.substrate.reamde.dev/tasks"] },
            present
          )
        )
      )
    ).toBe(
      "Import samples.substrate.reamde.dev/tasks first — this bundle declares against it."
    )
    expect(
      requiresHint(
        missingRequirements(
          requirementsOf(
            {
              requires: [
                "samples.substrate.reamde.dev/people",
                "samples.substrate.reamde.dev/messaging",
                "samples.substrate.reamde.dev/calendar",
              ],
            },
            present
          )
        )
      )
    ).toBe(
      "Import samples.substrate.reamde.dev/messaging and samples.substrate.reamde.dev/calendar first — this bundle declares against them."
    )
  })
})

describe("importFailureText — the server's refusal, verbatim", () => {
  it("shows the admission problems, which name what to import first", () => {
    const error = new ApiError(
      "validation",
      "validation error: [bundle providers.substrate.reamde.dev/google: …]",
      422,
      [
        "bundle providers.substrate.reamde.dev/google: data.requires names samples.substrate.reamde.dev/people, which this repository does not have — import that authority's bundle first",
      ]
    )
    expect(importFailureText(error)).toBe(
      "bundle providers.substrate.reamde.dev/google: data.requires names samples.substrate.reamde.dev/people, which this repository does not have — import that authority's bundle first"
    )
  })

  it("joins several problems and never drops one", () => {
    const error = new ApiError("validation", "validation error", 422, [
      "first problem",
      "second problem",
    ])
    expect(importFailureText(error)).toBe("first problem second problem")
  })

  it("falls back to the envelope message when there are no problems", () => {
    expect(
      importFailureText(new ApiError("forbidden", "owner only", 403))
    ).toBe("owner only")
    expect(importFailureText(new Error("network error"))).toBe("network error")
    expect(importFailureText(undefined)).toBe("The import was refused.")
  })
})

describe("installedKindRows — the Kinds table", () => {
  const configKind = kindInfo({
    identity: "providers.substrate.reamde.dev/google/config",
    name: "config",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    plural: "configs",
    definition: { traits: ["oauth2"] },
  })
  const accountKind = kindInfo({
    identity: "providers.substrate.reamde.dev/google/account",
    name: "account",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    plural: "accounts",
    definition: { traits: ["accountconfig"] },
  })
  const contactKind = kindInfo({
    identity: "providers.substrate.reamde.dev/google/contact",
    name: "contact",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    plural: "contacts",
  })
  const registry = [configKind, accountKind, contactKind]

  it("takes the closure's declared kinds from the catalog and resolves each route", () => {
    const rows = installedKindRows(
      status(),
      registry,
      catalog({
        closure: { kinds: [contactKind.identity, configKind.identity] },
      })
    )
    // sorted by display name: config < contact
    expect(rows.map((r) => r.identity)).toEqual([
      configKind.identity,
      contactKind.identity,
    ])
    const contact = rows.find((r) => r.identity === contactKind.identity)!
    expect(contact.authority).toBe("providers.substrate.reamde.dev")
    expect(contact.package).toBe("google")
    expect(contact.name).toBe("contact")
  })

  it("marks the input and account kinds by role", () => {
    const rows = installedKindRows(
      status(),
      registry,
      catalog({
        closure: { kinds: registry.map((k) => k.identity) },
      })
    )
    const byId = Object.fromEntries(rows.map((r) => [r.identity, r]))
    expect(byId[configKind.identity].role).toBe("input")
    expect(byId[accountKind.identity].role).toBe("account")
    expect(byId[contactKind.identity].role).toBeUndefined()
  })

  it("marks an input's kind from the catalog declaration alone (not yet imported)", () => {
    const rows = installedKindRows(
      { id: "providers.substrate.reamde.dev/google", inputs: undefined },
      registry,
      catalog({ closure: { kinds: registry.map((k) => k.identity) } })
    )
    const byId = Object.fromEntries(rows.map((r) => [r.identity, r]))
    expect(byId[configKind.identity].role).toBe("input")
  })

  it("falls back to the registry's owned-package kinds when there is no catalog entry", () => {
    const rows = installedKindRows(status(), registry, undefined)
    expect(rows.map((r) => r.identity).sort()).toEqual(
      registry.map((k) => k.identity).sort()
    )
  })

  // The hover has to work BEFORE the import — which is the one moment the
  // registry cannot answer, because nothing of the bundle is in it yet.
  it("describes a catalog-only kind from the closure, the registry once imported", () => {
    const rows = installedKindRows(
      status(),
      [],
      catalog({
        closure: {
          kinds: [contactKind.identity],
          kindDescriptions: { [contactKind.identity]: "What Google holds." },
        },
      })
    )
    expect(rows[0].description).toBe("What Google holds.")

    const imported = installedKindRows(
      status(),
      [{ ...contactKind, description: "The reconciled one." }],
      catalog({
        closure: {
          kinds: [contactKind.identity],
          kindDescriptions: { [contactKind.identity]: "What Google holds." },
        },
      })
    )
    expect(imported[0].description).toBe("The reconciled one.")
  })

  it("keeps a kind unresolved by the registry — identity only, no route", () => {
    const rows = installedKindRows(
      status(),
      registry,
      catalog({
        closure: { kinds: ["providers.substrate.reamde.dev/google/ghost"] },
      })
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].name).toBe("ghost")
    expect(rows[0].authority).toBeUndefined()
  })
})

describe("bundleRecordRows — the Records table", () => {
  it("carries every declaration under its own core kind, names split from refs", () => {
    const rows = bundleRecordRows(
      catalog({
        closure: {
          kinds: ["providers.substrate.reamde.dev/google/t"],
          functions: ["providers.substrate.reamde.dev/google/syncgoogle"],
          agents: ["providers.substrate.reamde.dev/google/summarize"],
        },
      })
    )
    expect(rows).toEqual([
      {
        kind: "substrate.reamde.dev/core/function",
        id: "providers.substrate.reamde.dev/google/syncgoogle",
        name: "syncgoogle",
      },
      {
        kind: "substrate.reamde.dev/core/agent",
        id: "providers.substrate.reamde.dev/google/summarize",
        name: "summarize",
      },
    ])
  })

  // The rows an install WRITES are half of what a bundle does — the llm
  // example's whole point is the provider row you then go and key — so they
  // ride the same table, carrying the kind they are records of.
  it("carries the shipped data records with their own kinds", () => {
    const rows = bundleRecordRows(
      catalog({
        closure: {
          kinds: [],
          records: [
            { kind: "substrate.reamde.dev/core/trigger", id: "ongooglesync" },
            { kind: "substrate.reamde.dev/core/llmprovider", id: "anthropic" },
          ],
        },
      })
    )
    expect(rows).toEqual([
      {
        kind: "substrate.reamde.dev/core/trigger",
        id: "ongooglesync",
        name: "ongooglesync",
      },
      {
        kind: "substrate.reamde.dev/core/llmprovider",
        id: "anthropic",
        name: "anthropic",
      },
    ])
  })

  it("renders mappings when a catalog carries them, and omits them otherwise", () => {
    const withMappings = bundleRecordRows(
      catalog({
        closure: {
          kinds: [],
          mappings: ["providers.substrate.reamde.dev/google/m"],
        },
      })
    )
    expect(withMappings).toEqual([
      {
        kind: "substrate.reamde.dev/core/recordmapping",
        id: "providers.substrate.reamde.dev/google/m",
        name: "m",
      },
    ])
    expect(bundleRecordRows(catalog({ closure: { kinds: [] } }))).toEqual([])
  })

  it("is empty for a bundle with no catalog entry (applied, not shipped)", () => {
    expect(bundleRecordRows(undefined)).toEqual([])
  })
})

describe("declaresProviderInterfaces", () => {
  const accountKind = kindInfo({
    identity: "providers.substrate.reamde.dev/google/account",
    name: "account",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    plural: "accounts",
    definition: { traits: ["accountconfig"] },
  })
  const clientKind = kindInfo({
    identity: "providers.substrate.reamde.dev/google/config",
    name: "config",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    plural: "configs",
    definition: { traits: ["oauth2"] },
  })

  it("is true when the bundle package ships an accountconfig account kind", () => {
    expect(
      accountKindOf([accountKind], "providers.substrate.reamde.dev/google")
    ).toBe(accountKind)
    expect(
      declaresProviderInterfaces(
        { id: "providers.substrate.reamde.dev/google", inputs: [] },
        [accountKind]
      )
    ).toBe(true)
  })

  it("is true when a declared input's kind carries the oauth2 trait", () => {
    expect(
      declaresProviderInterfaces(
        {
          id: "providers.substrate.reamde.dev/google",
          inputs: [
            {
              name: "client",
              kind: "providers.substrate.reamde.dev/google/config",
            },
          ],
        },
        [clientKind]
      )
    ).toBe(true)
  })

  it("is false for a non-provider bundle (no account kind, no oauth2 input)", () => {
    const connectorKind = kindInfo({
      identity: "samples.substrate.reamde.dev/web/config",
      name: "config",
      authority: "samples.substrate.reamde.dev",
      package: "web",
      plural: "configs",
    })
    expect(
      declaresProviderInterfaces(
        {
          id: "samples.substrate.reamde.dev/web",
          inputs: [
            {
              name: "connector",
              kind: "samples.substrate.reamde.dev/web/config",
            },
          ],
        },
        [connectorKind]
      )
    ).toBe(false)
  })
})

describe("oauthConnectBlocked, the connect gate", () => {
  const clientKind = kindInfo({
    identity: "providers.substrate.reamde.dev/google/config",
    name: "config",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    plural: "configs",
    definition: { traits: ["oauth2"] },
  })
  const inputs = [
    { name: "client", kind: "providers.substrate.reamde.dev/google/config" },
  ]

  it("does not block while nothing stands", () => {
    expect(oauthConnectBlocked({ inputs, setup: [] }, [clientKind])).toBe(false)
    expect(oauthConnectBlocked({ inputs }, [clientKind])).toBe(false)
  })

  it("blocks on an oauth-client item, and on the client input's own problems", () => {
    expect(
      oauthConnectBlocked(
        {
          inputs,
          setup: [
            {
              code: "oauth-client",
              input: "client",
              record: "default",
              message: "m",
            },
          ],
        },
        [clientKind]
      )
    ).toBe(true)
    for (const code of ["missing", "ambiguous", "dangling"] as const) {
      expect(
        oauthConnectBlocked(
          { inputs, setup: [{ code, input: "client", message: "m" }] },
          [clientKind]
        )
      ).toBe(true)
    }
  })

  it("does not block on an unrelated input's step or a provider step", () => {
    const both = [
      ...inputs,
      {
        name: "connector",
        kind: "providers.substrate.reamde.dev/google/other",
      },
    ]
    expect(
      oauthConnectBlocked(
        {
          inputs: both,
          setup: [{ code: "missing", input: "connector", message: "m" }],
        },
        [clientKind]
      )
    ).toBe(false)
    expect(
      oauthConnectBlocked(
        {
          inputs,
          setup: [{ code: "provider", record: "openai", message: "m" }],
        },
        [clientKind]
      )
    ).toBe(false)
  })
})

describe("suggestedMappingHint", () => {
  /** One suggested-mapping row, as suggestedMappingsOf builds them. */
  function rows(states: ("landed" | "waiting")[], pkgs: string[]) {
    const catalog = {
      id: "samples.substrate.reamde.dev/people",
      suggestedMappings: states.map((state, i) => ({
        id: `samples.substrate.reamde.dev/people/m${i}`,
        from: `${pkgs[i]}/thing`,
        to: "samples.substrate.reamde.dev/people/person",
        package: pkgs[i],
        state,
      })),
    } as CatalogItem
    return suggestedMappingsOf({
      id: catalog.id,
      name: "people",
      authority: "samples.substrate.reamde.dev",
      package: "people",
      installed: false,
      requires: [],
      catalog,
    })
  }

  it("says nothing when every mapping landed", () => {
    expect(
      suggestedMappingHint(
        rows(["landed"], ["providers.substrate.reamde.dev/github"])
      )
    ).toBe("")
  })

  it("names the provider to install and the sample to import again", () => {
    expect(
      suggestedMappingHint(
        rows(["waiting"], ["providers.substrate.reamde.dev/linear"])
      )
    ).toBe("Install linear to enable this mapping, then import people again.")
  })

  it("counts several, and names each provider once", () => {
    expect(
      suggestedMappingHint(
        rows(
          ["waiting", "waiting", "landed"],
          [
            "providers.substrate.reamde.dev/linear",
            "providers.substrate.reamde.dev/google",
            "providers.substrate.reamde.dev/github",
          ]
        )
      )
    ).toBe(
      "Install linear and google to enable these 2 mappings, then import people again."
    )
  })
})
