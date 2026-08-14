/** The bundles page folds two reads — the installed bundles' runtime status
 * and the shipped catalog — into one id-keyed row set. Installed wins the
 * counts, available closures surface first (they invite an action), and a
 * bundle present in both carries both. The Integrations and Vocabulary facets
 * come from the backend flags; the provider-copy gate reads the bundle's own
 * declared traits. The requirement fold answers the one question the reader has
 * before importing into a fresh (core-only) repository: is anything this
 * closure declares against still missing, and what must be imported first. */

import { describe, expect, it } from "vitest"

import type { BundleStatus } from "@/lib/api/bundles"
import type { CatalogItem } from "@/lib/api/catalog"
import { ApiError, type KindInfo } from "@/lib/api/types"
import {
  accountKindOf,
  bundleResourceRows,
  declaresProviderInterfaces,
  filterBundles,
  importFailureText,
  installedKindRows,
  mergeBundles,
  missingRequirements,
  oauthConnectBlocked,
  presentAuthorities,
  requirementsOf,
  requiresHint,
  upgradableBundleCount,
  upgradeBlocked,
  upgradeMotion,
} from "./bundles"

function status(over: Partial<BundleStatus> = {}): BundleStatus {
  return {
    id: "google.bundles.substrate.reamde.dev",
    name: "google",
    authority: "google.bundles.substrate.reamde.dev",
    installed: true,
    enabled: true,
    inputs: [
      {
        name: "client",
        kind: "google.bundles.substrate.reamde.dev/config",
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
    id: "google.bundles.substrate.reamde.dev",
    name: "google",
    authority: "google.bundles.substrate.reamde.dev",
    description: "Connects a Google account.",
    version: "v1",
    inputs: {
      client: { kind: "google.bundles.substrate.reamde.dev/config" },
    },
    resources: { kinds: ["a", "b"], functions: ["c"] },
    installed: false,
    integration: true,
    ...over,
  }
}

function kindInfo(over: Partial<KindInfo> = {}): KindInfo {
  return {
    identity: "people.substrate.reamde.dev/person",
    name: "person",
    authority: "people.substrate.reamde.dev",
    version: "",
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
    expect(rows[0].authority).toBe("google.bundles.substrate.reamde.dev")
  })

  it("keeps available closures and installed-not-in-catalog bundles both", () => {
    const rows = mergeBundles(
      [status({ id: "custom.bundles.substrate.reamde.dev", name: "custom" })],
      [catalog({ id: "slack.bundles.substrate.reamde.dev", name: "slack" })]
    )
    const byId = Object.fromEntries(rows.map((r) => [r.id, r]))
    expect(byId["custom.bundles.substrate.reamde.dev"].installed).toBe(true)
    expect(byId["slack.bundles.substrate.reamde.dev"].installed).toBe(false)
    expect(byId["slack.bundles.substrate.reamde.dev"].status).toBeUndefined()
  })

  it("orders available before installed", () => {
    const rows = mergeBundles(
      [status({ id: "b.bundles.substrate.reamde.dev" })],
      [catalog({ id: "a.bundles.substrate.reamde.dev", installed: false })]
    )
    expect(rows.map((r) => r.installed)).toEqual([false, true])
  })

  it("reads the Integration facet from the catalog flag, defaulting false", () => {
    const rows = mergeBundles(
      [],
      [
        catalog({ id: "google.bundles.substrate.reamde.dev", integration: true }),
        catalog({
          id: "urlharvester.bundles.substrate.reamde.dev",
          name: "urlharvester",
          integration: false,
        }),
      ]
    )
    const byId = Object.fromEntries(rows.map((r) => [r.id, r]))
    expect(byId["google.bundles.substrate.reamde.dev"].integration).toBe(true)
    expect(byId["urlharvester.bundles.substrate.reamde.dev"].integration).toBe(false)
  })

  it("an installed-only bundle (no catalog entry) is non-integration", () => {
    const rows = mergeBundles([status({ id: "x.bundles.substrate.reamde.dev" })], [])
    expect(rows[0].integration).toBe(false)
  })

  it("keeps the catalog integration flag when a status folds over it", () => {
    const rows = mergeBundles(
      [status()],
      [catalog({ installed: true, integration: true })]
    )
    expect(rows[0].integration).toBe(true)
  })
})

describe("mergeBundles — the closure facets", () => {
  it("carries the vocabulary flag and the requires list off the catalog entry", () => {
    const rows = mergeBundles(
      [],
      [
        catalog({
          id: "people.substrate.reamde.dev/people",
          name: "people",
          authority: "people.substrate.reamde.dev",
          integration: false,
          vocabulary: true,
        }),
        catalog({ requires: ["people.substrate.reamde.dev", "messaging.substrate.reamde.dev"] }),
      ]
    )
    const byId = Object.fromEntries(rows.map((r) => [r.id, r]))
    expect(byId["people.substrate.reamde.dev/people"].vocabulary).toBe(true)
    expect(byId["people.substrate.reamde.dev/people"].requires).toEqual([])
    expect(byId["google.bundles.substrate.reamde.dev"].vocabulary).toBe(false)
    expect(byId["google.bundles.substrate.reamde.dev"].requires).toEqual([
      "people.substrate.reamde.dev",
      "messaging.substrate.reamde.dev",
    ])
  })

  it("keeps the catalog's facets when a status folds over the same id", () => {
    const rows = mergeBundles(
      [status({ id: "people.substrate.reamde.dev/people", authority: "people.substrate.reamde.dev" })],
      [
        catalog({
          id: "people.substrate.reamde.dev/people",
          authority: "people.substrate.reamde.dev",
          vocabulary: true,
          requires: ["core.substrate.reamde.dev"],
          installed: true,
        }),
      ]
    )
    expect(rows[0].vocabulary).toBe(true)
    expect(rows[0].requires).toEqual(["core.substrate.reamde.dev"])
  })

  it("an applied-only bundle claims neither facet — the catalog states them, nothing derives them", () => {
    const rows = mergeBundles([status({ id: "x.bundles.substrate.reamde.dev" })], [])
    expect(rows[0].vocabulary).toBe(false)
    expect(rows[0].requires).toEqual([])
  })
})

describe("filterBundles", () => {
  const rows = mergeBundles(
    [],
    [
      catalog({ id: "google.bundles.substrate.reamde.dev", integration: true }),
      catalog({
        id: "urlharvester.bundles.substrate.reamde.dev",
        name: "urlharvester",
        integration: false,
      }),
      catalog({
        id: "people.substrate.reamde.dev/people",
        name: "people",
        authority: "people.substrate.reamde.dev",
        integration: false,
        vocabulary: true,
      }),
    ]
  )

  it("all keeps every row", () => {
    expect(filterBundles(rows, "all")).toHaveLength(3)
  })

  it("integrations narrows to the integration rows", () => {
    const only = filterBundles(rows, "integrations")
    expect(only.map((r) => r.id)).toEqual(["google.bundles.substrate.reamde.dev"])
  })

  it("vocabulary narrows to the pure-vocabulary bundles", () => {
    const only = filterBundles(rows, "vocabulary")
    expect(only.map((r) => r.id)).toEqual(["people.substrate.reamde.dev/people"])
  })

  it("upgrades narrows to rows whose preview says the closure moved", () => {
    const moved = mergeBundles(
      [],
      [
        catalog({ id: "a.bundles.substrate.reamde.dev/a", name: "a", installed: true }),
        catalog({
          id: "b.bundles.substrate.reamde.dev/b",
          name: "b",
          installed: true,
          upgrade: { available: true, from: "v1alpha1", to: "v1alpha2" },
        }),
      ]
    )
    expect(filterBundles(moved, "upgrades").map((r) => r.id)).toEqual([
      "b.bundles.substrate.reamde.dev/b",
    ])
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
          upgrade: { available: true, to: "v1alpha2" },
        }),
        // Not installed: nothing to upgrade, whatever the preview would say.
        catalog({ id: "c", installed: false }),
        // Blocked still counts: it needs the reader's hand.
        catalog({
          id: "d",
          installed: true,
          upgrade: { available: true, to: "v2", blockers: ["live rows"] },
        }),
      ])
    ).toBe(2)
  })

  it("blocked means available AND the server named blockers", () => {
    expect(upgradeBlocked({ upgrade: undefined })).toBe(false)
    expect(
      upgradeBlocked({ upgrade: { available: true, to: "v2" } })
    ).toBe(false)
    expect(
      upgradeBlocked({
        upgrade: { available: true, to: "v2", blockers: ["a guard line"] },
      })
    ).toBe(true)
  })

  it("renders the version motion, tolerating a store with no version", () => {
    expect(
      upgradeMotion({ available: true, from: "v1alpha1", to: "v1alpha2" })
    ).toBe("v1alpha1 → v1alpha2")
    expect(upgradeMotion({ available: true, to: "v1alpha2" })).toBe("v1alpha2")
  })

  it("states one version when the authority did not move", () => {
    // A kind's own bump, or a kind the closure ADDED, upgrades without the
    // authority version moving — both legal. "v1alpha1 → v1alpha1" would read
    // as a bug, so it collapses to the version itself.
    expect(
      upgradeMotion({ available: true, from: "v1alpha1", to: "v1alpha1" })
    ).toBe("v1alpha1")
  })
})

describe("presentAuthorities — what this repository already holds", () => {
  it("counts an imported bundle's authority and every reconciled kind's", () => {
    const rows = mergeBundles(
      [status({ id: "people.substrate.reamde.dev/people", authority: "people.substrate.reamde.dev" })],
      [
        catalog({
          id: "people.substrate.reamde.dev/people",
          authority: "people.substrate.reamde.dev",
          installed: true,
          vocabulary: true,
        }),
        catalog({ id: "google.bundles.substrate.reamde.dev", installed: false }),
      ]
    )
    const present = presentAuthorities(rows, [
      kindInfo({ identity: "core.substrate.reamde.dev/bundle", authority: "core.substrate.reamde.dev" }),
    ])
    expect(present.has("people.substrate.reamde.dev")).toBe(true)
    expect(present.has("core.substrate.reamde.dev")).toBe(true)
    // Shipped in the catalog but never imported — the schema is not there.
    expect(present.has("google.bundles.substrate.reamde.dev")).toBe(false)
  })

  it("does not count an uninstalled or quarantined bundle's authority", () => {
    const rows = mergeBundles(
      [
        status({
          id: "people.substrate.reamde.dev/people",
          authority: "people.substrate.reamde.dev",
          installed: false,
          enabled: false,
        }),
      ],
      []
    )
    expect(presentAuthorities(rows).has("people.substrate.reamde.dev")).toBe(false)
  })
})

describe("requirementsOf / requiresHint — what to import first", () => {
  const present = new Set(["people.substrate.reamde.dev", "core.substrate.reamde.dev"])

  it("marks each declared authority present or missing, in declaration order", () => {
    const reqs = requirementsOf(
      { requires: ["people.substrate.reamde.dev", "messaging.substrate.reamde.dev"] },
      present
    )
    expect(reqs).toEqual([
      { authority: "people.substrate.reamde.dev", present: true },
      { authority: "messaging.substrate.reamde.dev", present: false },
    ])
    expect(missingRequirements(reqs).map((r) => r.authority)).toEqual([
      "messaging.substrate.reamde.dev",
    ])
  })

  it("a closure that declares against nothing is never blocked", () => {
    expect(requirementsOf({ requires: [] }, new Set())).toEqual([])
    expect(requiresHint([])).toBe("")
  })

  it("names one missing authority, then several, the way the server names them", () => {
    expect(
      requiresHint(missingRequirements(requirementsOf({ requires: ["tasks.substrate.reamde.dev"] }, present)))
    ).toBe("Import tasks.substrate.reamde.dev first — this bundle declares against it.")
    expect(
      requiresHint(
        missingRequirements(
          requirementsOf(
            {
              requires: [
                "people.substrate.reamde.dev",
                "messaging.substrate.reamde.dev",
                "calendar.substrate.reamde.dev",
              ],
            },
            present
          )
        )
      )
    ).toBe(
      "Import messaging.substrate.reamde.dev and calendar.substrate.reamde.dev first — this bundle declares against them."
    )
  })

})

describe("importFailureText — the server's refusal, verbatim", () => {
  it("shows the admission problems, which name what to import first", () => {
    const error = new ApiError(
      "validation",
      "validation error: [bundle google.bundles.substrate.reamde.dev: …]",
      422,
      [
        "bundle google.bundles.substrate.reamde.dev: data.requires names people.substrate.reamde.dev, which this repository does not have — import that authority's bundle first",
      ]
    )
    expect(importFailureText(error)).toBe(
      "bundle google.bundles.substrate.reamde.dev: data.requires names people.substrate.reamde.dev, which this repository does not have — import that authority's bundle first"
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
    expect(importFailureText(new ApiError("forbidden", "owner only", 403))).toBe(
      "owner only"
    )
    expect(importFailureText(new Error("network error"))).toBe("network error")
    expect(importFailureText(undefined)).toBe("The import was refused.")
  })
})

describe("installedKindRows — the Kinds table", () => {
  const configKind = kindInfo({
    identity: "google.bundles.substrate.reamde.dev/config",
    name: "config",
    authority: "google.bundles.substrate.reamde.dev",
    plural: "configs",
    definition: { traits: ["oauth2"] },
  })
  const accountKind = kindInfo({
    identity: "google.bundles.substrate.reamde.dev/account",
    name: "account",
    authority: "google.bundles.substrate.reamde.dev",
    plural: "accounts",
    definition: { traits: ["accountconfig"] },
  })
  const contactKind = kindInfo({
    identity: "google.bundles.substrate.reamde.dev/contact",
    name: "contact",
    authority: "google.bundles.substrate.reamde.dev",
    plural: "contacts",
  })
  const registry = [configKind, accountKind, contactKind]

  it("takes the closure's declared kinds from the catalog and resolves each route", () => {
    const rows = installedKindRows(
      status(),
      registry,
      catalog({
        resources: { kinds: [contactKind.identity, configKind.identity] },
      })
    )
    // sorted by display name: config < contact
    expect(rows.map((r) => r.identity)).toEqual([
      configKind.identity,
      contactKind.identity,
    ])
    const contact = rows.find((r) => r.identity === contactKind.identity)!
    expect(contact.authority).toBe("google.bundles.substrate.reamde.dev")
    expect(contact.plural).toBe("contacts")
  })

  it("marks the input and account kinds by role", () => {
    const rows = installedKindRows(status(), registry, catalog({
      resources: { kinds: registry.map((k) => k.identity) },
    }))
    const byId = Object.fromEntries(rows.map((r) => [r.identity, r]))
    expect(byId[configKind.identity].role).toBe("input")
    expect(byId[accountKind.identity].role).toBe("account")
    expect(byId[contactKind.identity].role).toBeUndefined()
  })

  it("marks an input's kind from the catalog declaration alone (not yet imported)", () => {
    const rows = installedKindRows(
      { authority: "google.bundles.substrate.reamde.dev", inputs: undefined },
      registry,
      catalog({ resources: { kinds: registry.map((k) => k.identity) } })
    )
    const byId = Object.fromEntries(rows.map((r) => [r.identity, r]))
    expect(byId[configKind.identity].role).toBe("input")
  })

  it("falls back to the registry's owned-authority kinds when there is no catalog entry", () => {
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
        resources: {
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
        resources: {
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
      catalog({ resources: { kinds: ["google.bundles.substrate.reamde.dev/ghost"] } })
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].name).toBe("ghost")
    expect(rows[0].authority).toBeUndefined()
    expect(rows[0].plural).toBeUndefined()
  })
})

describe("bundleResourceRows — the Resources table", () => {
  it("flattens functions, agents and triggers into kinded rows, names split from refs", () => {
    const rows = bundleResourceRows(
      catalog({
        resources: {
          kinds: ["google.bundles.substrate.reamde.dev/t"],
          functions: ["google.bundles.substrate.reamde.dev/syncgoogle"],
          agents: ["google.bundles.substrate.reamde.dev/summarize"],
          triggers: ["google.bundles.substrate.reamde.dev/ongooglesync"],
        },
      })
    )
    expect(rows).toEqual([
      { kind: "function", identity: "google.bundles.substrate.reamde.dev/syncgoogle", name: "syncgoogle" },
      { kind: "agent", identity: "google.bundles.substrate.reamde.dev/summarize", name: "summarize" },
      { kind: "trigger", identity: "google.bundles.substrate.reamde.dev/ongooglesync", name: "ongooglesync" },
    ])
  })

  it("renders mappings when a catalog carries them, and omits them otherwise", () => {
    const withMappings = bundleResourceRows(
      catalog({ resources: { kinds: [], mappings: ["google.bundles.substrate.reamde.dev/m"] } })
    )
    expect(withMappings).toEqual([
      { kind: "mapping", identity: "google.bundles.substrate.reamde.dev/m", name: "m" },
    ])
    expect(bundleResourceRows(catalog({ resources: { kinds: [] } }))).toEqual([])
  })

  it("is empty for a bundle with no catalog entry (applied, not shipped)", () => {
    expect(bundleResourceRows(undefined)).toEqual([])
  })
})

describe("declaresProviderInterfaces", () => {
  const accountKind = kindInfo({
    identity: "google.bundles.substrate.reamde.dev/account",
    name: "account",
    authority: "google.bundles.substrate.reamde.dev",
    plural: "accounts",
    definition: { traits: ["accountconfig"] },
  })
  const clientKind = kindInfo({
    identity: "google.bundles.substrate.reamde.dev/config",
    name: "config",
    authority: "google.bundles.substrate.reamde.dev",
    plural: "configs",
    definition: { traits: ["oauth2"] },
  })

  it("is true when the bundle authority ships an accountconfig account kind", () => {
    expect(accountKindOf([accountKind], "google.bundles.substrate.reamde.dev")).toBe(
      accountKind
    )
    expect(
      declaresProviderInterfaces(
        { authority: "google.bundles.substrate.reamde.dev", inputs: [] },
        [accountKind]
      )
    ).toBe(true)
  })

  it("is true when a declared input's kind carries the oauth2 trait", () => {
    expect(
      declaresProviderInterfaces(
        {
          authority: "google.bundles.substrate.reamde.dev",
          inputs: [
            { name: "client", kind: "google.bundles.substrate.reamde.dev/config" },
          ],
        },
        [clientKind]
      )
    ).toBe(true)
  })

  it("is false for a non-provider bundle (no account kind, no oauth2 input)", () => {
    const connectorKind = kindInfo({
      identity: "web.bundles.substrate.reamde.dev/config",
      name: "config",
      authority: "web.bundles.substrate.reamde.dev",
      plural: "configs",
    })
    expect(
      declaresProviderInterfaces(
        {
          authority: "web.bundles.substrate.reamde.dev",
          inputs: [
            { name: "connector", kind: "web.bundles.substrate.reamde.dev/config" },
          ],
        },
        [connectorKind]
      )
    ).toBe(false)
  })
})

describe("oauthConnectBlocked, the connect gate", () => {
  const clientKind = kindInfo({
    identity: "google.bundles.substrate.reamde.dev/config",
    name: "config",
    authority: "google.bundles.substrate.reamde.dev",
    plural: "configs",
    definition: { traits: ["oauth2"] },
  })
  const inputs = [
    { name: "client", kind: "google.bundles.substrate.reamde.dev/config" },
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
            { code: "oauth-client", input: "client", record: "default", message: "m" },
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
      { name: "connector", kind: "google.bundles.substrate.reamde.dev/other" },
    ]
    expect(
      oauthConnectBlocked(
        { inputs: both, setup: [{ code: "missing", input: "connector", message: "m" }] },
        [clientKind]
      )
    ).toBe(false)
    expect(
      oauthConnectBlocked(
        { inputs, setup: [{ code: "provider", record: "openai", message: "m" }] },
        [clientKind]
      )
    ).toBe(false)
  })
})
