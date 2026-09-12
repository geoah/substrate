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
import { ApiError, type BundleUpgrade, type KindInfo } from "@/lib/api/types"
import {
  accountKindOf,
  bundleRecordRows,
  bundleSections,
  declaresProviderInterfaces,
  importFailureText,
  installedKindRows,
  lossyStepLines,
  mergeBundles,
  missingRequirements,
  oauthConnectBlocked,
  presentPackages,
  previewFailed,
  FAILED_PREVIEW_BLOCKER,
  requirementsOf,
  chainHint,
  requirementTree,
  missingChain,
  closureRows,
  importPlan,
  triggerRows,
  mappingLinksSentence,
  readyMappings,
  heldVersions,
  needsConfirmation,
  confirmationOf,
  settingSetupCount,
  stepLines,
  upgradableBundleCount,
  upgradeBlocked,
  upgradeMotion,
  pendingShippedUpgrades,
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
    closure: {
      traits: null,
      triggers: null,
      agents: null,
      mappings: null,
      records: null,
      kinds: ["a", "b"],
      functions: ["c"],
    },
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
    source: "builtin",
    description: "",
    definition: {},
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
          closure: {
            traits: null,
            triggers: null,
            functions: null,
            agents: null,
            mappings: null,
            records: null,
            kinds: ["samples.substrate.reamde.dev/tasks/task"],
          },
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

describe("settingSetupCount", () => {
  it("counts the empty settings across every held bundle, and nothing else", () => {
    expect(
      settingSetupCount([
        {
          setup: [
            {
              code: "setting",
              kind: "substrate.reamde.dev/core/secret",
              record: "ada.example.com/firecrawl/apiKey",
              message: "API key is not set",
            },
            // An input's own problem belongs to the Registry's setup chip, not
            // to the settings badge.
            { code: "missing", input: "client", message: "no record yet" },
          ],
        },
        {
          setup: [
            {
              code: "setting",
              kind: "substrate.reamde.dev/core/setting",
              record: "ada.example.com/web/baseURL",
              message: "Base URL is not set",
            },
          ],
        },
        {},
      ])
    ).toBe(2)
  })

  it("is zero when nothing needs filling in", () => {
    expect(settingSetupCount([])).toBe(0)
    expect(settingSetupCount([{ setup: [] }])).toBe(0)
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
          upgrade: { available: true, to: 2, work: 0, lossy: false },
        }),
        // Not installed: nothing to upgrade, whatever the preview would say.
        catalog({ id: "c", installed: false }),
        // Blocked still counts: it needs the reader's hand.
        catalog({
          id: "d",
          installed: true,
          upgrade: {
            available: true,
            to: 2,
            work: 0,
            lossy: false,
            blockers: ["live rows"],
          },
        }),
        // A preview the server could not run: blocked without a motion. It
        // shows a chip on the row, so the badge counts it too.
        catalog({
          id: "e",
          installed: true,
          upgrade: {
            available: false,
            work: 0,
            lossy: false,
            blockers: [FAILED_PREVIEW_BLOCKER],
          },
        }),
      ])
    ).toBe(3)
  })

  it("a failed preview is keyed on its one fixed line", () => {
    expect(previewFailed({ upgrade: undefined })).toBe(false)
    expect(
      previewFailed({
        upgrade: {
          available: true,
          work: 0,
          lossy: false,
          blockers: ["live rows"],
        },
      })
    ).toBe(false)
    expect(
      previewFailed({
        upgrade: {
          available: false,
          work: 0,
          lossy: false,
          blockers: [FAILED_PREVIEW_BLOCKER],
        },
      })
    ).toBe(true)
  })

  it("blocked means the server named blockers", () => {
    expect(upgradeBlocked({ upgrade: undefined })).toBe(false)
    expect(
      upgradeBlocked({
        upgrade: { available: true, to: 2, work: 0, lossy: false },
      })
    ).toBe(false)
    expect(
      upgradeBlocked({
        upgrade: {
          available: true,
          to: 2,
          work: 0,
          lossy: false,
          blockers: ["a guard line"],
        },
      })
    ).toBe(true)
    // A preview the server could not run: no motion, one line with the error
    // text. Stated as blocked, never dropped.
    expect(
      upgradeBlocked({
        upgrade: {
          available: false,
          work: 0,
          lossy: false,
          blockers: [FAILED_PREVIEW_BLOCKER],
        },
      })
    ).toBe(true)
  })

  it("a pending shipped upgrade is one the binary ships and the store lacks", () => {
    const refused = {
      package: "substrate.reamde.dev/core",
      upgrade: {
        available: true,
        from: 16,
        to: 17,
        work: 0,
        lossy: false,
        blockers: ["a guard line"],
      },
    }
    // Admitted but not landed: the last blocking record was migrated and the
    // boot has not run again. Still news, or the owner never learns a restart
    // is what lands it.
    const admitted = {
      package: "substrate.reamde.dev/core",
      upgrade: { available: true, from: 16, to: 17, work: 0, lossy: false },
    }
    expect(
      pendingShippedUpgrades([
        {
          package: "substrate.reamde.dev/core",
          upgrade: { available: false, work: 0, lossy: false },
        },
        admitted,
        refused,
      ])
    ).toEqual([admitted, refused])
  })

  it("names each conversion step with the live records it rewrites", () => {
    expect(stepLines(undefined)).toEqual([])
    expect(stepLines({ work: 0, lossy: false })).toEqual([])
    const plan: BundleUpgrade = {
      available: true,
      work: 7,
      lossy: true,
      planHash: "abc",
      changelogSeq: 41,
      steps: [
        {
          step: "rename",
          kind: "substrate.reamde.dev/core/llmprovider",
          property: "displayLabel",
          from: "label",
          to: "displayLabel",
          records: 3,
        },
        {
          step: "backfill",
          kind: "geoah.example.com/shop/widget",
          property: "size",
          records: 1,
        },
        {
          step: "remap",
          kind: "geoah.example.com/shop/widget",
          property: "status",
          from: "active",
          to: "open",
          records: 2,
          lossy: true,
        },
        {
          step: "null",
          kind: "geoah.example.com/shop/widget",
          property: "color",
          records: 1,
          lossy: true,
        },
      ],
    }
    expect(stepLines(plan)).toEqual([
      "renames label to displayLabel on substrate.reamde.dev/core/llmprovider: 3 live records rewritten",
      "backfills size with its default on geoah.example.com/shop/widget: 1 live record rewritten",
      "rewrites status active to open on geoah.example.com/shop/widget: 2 live records rewritten (lossy: the records holding either value become one set)",
      "drops color on geoah.example.com/shop/widget: its value leaves 1 live record (lossy: the values stay in the changelog only)",
    ])
    // The dialog lists the lossy steps alone: what the click consents to.
    expect(lossyStepLines(plan)).toEqual([
      "rewrites status active to open on geoah.example.com/shop/widget: 2 live records rewritten (lossy: the records holding either value become one set)",
      "drops color on geoah.example.com/shop/widget: its value leaves 1 live record (lossy: the values stay in the changelog only)",
    ])
  })

  it("renders the version motion, tolerating a store with no version", () => {
    const plan = { available: true, work: 0, lossy: false }
    expect(upgradeMotion({ ...plan, from: 1, to: 2 })).toBe("1 → 2")
    expect(upgradeMotion({ ...plan, to: 2 })).toBe("2")
    // 0 is the wire's absent (omitempty), so it reads exactly like undefined.
    expect(upgradeMotion({ ...plan, from: 0, to: 2 })).toBe("2")
    expect(upgradeMotion({ ...plan, from: 0, to: 0 })).toBe("")
  })

  it("states one version when the authority did not move", () => {
    // A kind's own bump, or a kind the closure ADDED, upgrades without the
    // authority version moving — both legal. "3 → 3" would read as a bug, so
    // it collapses to the version itself.
    expect(
      upgradeMotion({ available: true, work: 0, lossy: false, from: 1, to: 1 })
    ).toBe("1")
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

describe("requirementsOf / chainHint: what is taken first", () => {
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
    expect(chainHint([], "Import", "tasks")).toBe("")
  })

  it("says what the one button will take first, in the order it takes them", () => {
    expect(
      chainHint(
        missingRequirements(
          requirementsOf(
            { requires: ["samples.substrate.reamde.dev/tasks"] },
            present
          )
        ),
        "Import",
        "pebble"
      )
    ).toBe(
      "Import all takes samples.substrate.reamde.dev/tasks first, in that order, then pebble."
    )
    expect(
      chainHint(
        missingRequirements(
          requirementsOf(
            {
              requires: [
                "samples.substrate.reamde.dev/people",
                "samples.substrate.reamde.dev/messaging",
              ],
            },
            present
          )
        ),
        "Install",
        "google"
      )
    ).toBe(
      "Install all takes samples.substrate.reamde.dev/messaging first, in that order, then google."
    )
  })
})

describe("requiresAtLeast: the floor under a requirement (decision record 0070)", () => {
  const present = new Set([
    "ada.example.com/people",
    "ada.example.com/scheduling",
  ])
  const versions = new Map([
    ["ada.example.com/people", 3],
    ["ada.example.com/scheduling", 2],
  ])
  const row = {
    requires: ["ada.example.com/people", "ada.example.com/scheduling"],
    requiresAtLeast: { "ada.example.com/people": 4 },
  }

  it("marks a package held below its floor missing, with both versions", () => {
    const reqs = requirementsOf(row, present, versions)
    expect(reqs).toEqual([
      {
        package: "ada.example.com/people",
        present: false,
        atLeast: 4,
        held: 3,
      },
      { package: "ada.example.com/scheduling", present: true, held: 2 },
    ])
    expect(chainHint(missingRequirements(reqs), "Import", "tasks")).toBe(
      "ada.example.com/people is here at version 3 and this bundle needs version 4 or later, so it is imported again."
    )
  })

  it("is met at the floor and above it", () => {
    for (const held of [4, 5]) {
      const reqs = requirementsOf(
        row,
        present,
        new Map([["ada.example.com/people", held]])
      )
      expect(reqs[0].present).toBe(true)
    }
  })

  it("takes a package the kind registry alone knows as meeting the floor", () => {
    // No bundle status reports a version for it, so the console cannot say
    // it is too old; the server's admission is the one that refuses.
    expect(requirementsOf(row, present, new Map())[0].present).toBe(true)
  })

  it("names an absent package and a too-old one in one hint", () => {
    const reqs = requirementsOf(
      row,
      new Set(["ada.example.com/people"]),
      versions
    )
    expect(chainHint(missingRequirements(reqs), "Import", "tasks")).toBe(
      "Import all takes ada.example.com/scheduling first, in that order, then tasks. " +
        "ada.example.com/people is here at version 3 and this bundle needs version 4 or later, so it is imported again."
    )
  })

  it("reads the held versions off the installed rows' statuses", () => {
    const rows = mergeBundles(
      [status({ id: "ada.example.com/people", version: 3 })],
      [
        catalog({ id: "ada.example.com/people", installed: true }),
        catalog({ id: "ada.example.com/tasks", installed: false }),
      ]
    )
    expect([...heldVersions(rows)]).toEqual([["ada.example.com/people", 3]])
  })
})

describe("needsConfirmation: what a click has to consent to", () => {
  const base: BundleUpgrade = {
    available: true,
    work: 0,
    lossy: false,
    planHash: "d15c",
    changelogSeq: 9,
  }

  it("asks for a lossy plan and for one that replaces edits, never otherwise", () => {
    expect(needsConfirmation(base)).toBe(false)
    expect(needsConfirmation({ ...base, lossy: true })).toBe(true)
    expect(needsConfirmation({ ...base, discardsEdits: true })).toBe(true)
    expect(needsConfirmation(undefined)).toBe(false)
    // A preview with no hash has nothing a confirmation could name.
    expect(
      needsConfirmation({ ...base, discardsEdits: true, planHash: undefined })
    ).toBe(false)
  })

  it("hands back exactly the previewed hash and head", () => {
    expect(confirmationOf({ ...base, discardsEdits: true })).toEqual({
      planHash: "d15c",
      changelogSeq: 9,
    })
    expect(confirmationOf(base)).toBeUndefined()
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
    definition: { traits: ["oauth2"] },
  })
  const accountKind = kindInfo({
    identity: "providers.substrate.reamde.dev/google/account",
    name: "account",
    authority: "providers.substrate.reamde.dev",
    package: "google",
    definition: { traits: ["accountconfig"] },
  })
  const contactKind = kindInfo({
    identity: "providers.substrate.reamde.dev/google/contact",
    name: "contact",
    authority: "providers.substrate.reamde.dev",
    package: "google",
  })
  const registry = [configKind, accountKind, contactKind]

  it("takes the closure's declared kinds from the catalog and resolves each route", () => {
    const rows = installedKindRows(
      status(),
      registry,
      catalog({
        closure: {
          traits: null,
          triggers: null,
          functions: null,
          agents: null,
          mappings: null,
          records: null,
          kinds: [contactKind.identity, configKind.identity],
        },
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
        closure: {
          traits: null,
          triggers: null,
          functions: null,
          agents: null,
          mappings: null,
          records: null,
          kinds: registry.map((k) => k.identity),
        },
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
      catalog({
        closure: {
          traits: null,
          triggers: null,
          functions: null,
          agents: null,
          mappings: null,
          records: null,
          kinds: registry.map((k) => k.identity),
        },
      })
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
          traits: null,
          triggers: null,
          functions: null,
          agents: null,
          mappings: null,
          records: null,
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
          traits: null,
          triggers: null,
          functions: null,
          agents: null,
          mappings: null,
          records: null,
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
        closure: {
          traits: null,
          triggers: null,
          functions: null,
          agents: null,
          mappings: null,
          records: null,
          kinds: ["providers.substrate.reamde.dev/google/ghost"],
        },
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
          traits: null,
          triggers: null,
          mappings: null,
          records: null,
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
          traits: null,
          triggers: null,
          functions: null,
          agents: null,
          mappings: null,
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
          traits: null,
          triggers: null,
          functions: null,
          agents: null,
          records: null,
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
    expect(
      bundleRecordRows(
        catalog({
          closure: {
            traits: null,
            triggers: null,
            functions: null,
            agents: null,
            mappings: null,
            records: null,
            kinds: [],
          },
        })
      )
    ).toEqual([])
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
    definition: { traits: ["accountconfig"] },
  })
  const clientKind = kindInfo({
    identity: "providers.substrate.reamde.dev/google/config",
    name: "config",
    authority: "providers.substrate.reamde.dev",
    package: "google",
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
      identity: "acme.example.com/reader/config",
      name: "config",
      authority: "acme.example.com",
      package: "reader",
    })
    expect(
      declaresProviderInterfaces(
        {
          id: "acme.example.com/reader",
          inputs: [
            {
              name: "connector",
              kind: "acme.example.com/reader/config",
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

describe("the requirement chain: what one button has to take", () => {
  // pebble requires tasks, tasks requires people and scheduling, and this
  // repository holds none of them. The wire says only the direct one.
  const rows = mergeBundles(
    [],
    [
      catalog({
        id: "s.example.com/pebble",
        name: "pebble",
        package: "pebble",
        tier: "sample",
        requires: ["s.example.com/tasks"],
      }),
      catalog({
        id: "s.example.com/tasks",
        name: "tasks",
        package: "tasks",
        tier: "sample",
        requires: ["s.example.com/people", "s.example.com/scheduling"],
      }),
      catalog({
        id: "s.example.com/people",
        name: "people",
        package: "people",
        tier: "sample",
      }),
      catalog({
        id: "s.example.com/scheduling",
        name: "scheduling",
        package: "scheduling",
        tier: "sample",
      }),
    ]
  )
  const byId = new Map(rows.map((r) => [r.id, r]))
  const pebble = byId.get("s.example.com/pebble")!

  it("walks the chain the wire does not carry, and names each supplier", () => {
    const tree = requirementTree(pebble, byId, new Set())
    expect(tree.map((n) => n.package)).toEqual(["s.example.com/tasks"])
    expect(tree[0].row?.name).toBe("tasks")
    expect(tree[0].requires.map((n) => n.package)).toEqual([
      "s.example.com/people",
      "s.example.com/scheduling",
    ])
  })

  it("orders the missing ones leaves first, each once", () => {
    const chain = missingChain(requirementTree(pebble, byId, new Set()))
    expect(chain.map((n) => n.package)).toEqual([
      "s.example.com/people",
      "s.example.com/scheduling",
      "s.example.com/tasks",
    ])
  })

  it("drops what this repository already holds", () => {
    const chain = missingChain(
      requirementTree(pebble, byId, new Set(["s.example.com/people"]))
    )
    expect(chain.map((n) => n.package)).toEqual([
      "s.example.com/scheduling",
      "s.example.com/tasks",
    ])
  })

  it("takes a package held below its floor again, in the same pass", () => {
    const floored = mergeBundles(
      [],
      [
        catalog({
          id: "s.example.com/pebble",
          tier: "sample",
          requires: ["s.example.com/tasks"],
          requiresAtLeast: { "s.example.com/tasks": 4 },
        }),
        catalog({ id: "s.example.com/tasks", tier: "sample" }),
      ]
    )
    const map = new Map(floored.map((r) => [r.id, r]))
    const chain = missingChain(
      requirementTree(
        map.get("s.example.com/pebble")!,
        map,
        new Set(["s.example.com/tasks"]),
        new Map([["s.example.com/tasks", 3]])
      )
    )
    expect(chain.map((n) => n.package)).toEqual(["s.example.com/tasks"])
    expect(chain[0].held).toBe(3)
    expect(chain[0].atLeast).toBe(4)
  })

  it("refuses a cycle instead of importing a bundle before itself", () => {
    const cyclic = mergeBundles(
      [],
      [
        catalog({ id: "a.example.com/one", requires: ["a.example.com/two"] }),
        catalog({ id: "a.example.com/two", requires: ["a.example.com/one"] }),
      ]
    )
    const map = new Map(cyclic.map((r) => [r.id, r]))
    const one = map.get("a.example.com/one")!
    const tree = requirementTree(one, map, new Set())
    // The walk stops where it comes back round, and says so.
    expect(tree[0].requires[0].cycle).toBe(true)
    const plan = importPlan(one, tree)
    expect(plan.bundles).toEqual([])
    expect(plan.refusal).toBe(
      "a.example.com/two and a.example.com/one require each other, so there is no order to import them in. Nothing is imported."
    )
  })

  it("plans the chain leaves first with the bundle last, and only once", () => {
    const plan = importPlan(pebble, requirementTree(pebble, byId, new Set()))
    expect(plan.refusal).toBe("")
    expect(plan.bundles.map((b) => b.id)).toEqual([
      "s.example.com/people",
      "s.example.com/scheduling",
      "s.example.com/tasks",
      "s.example.com/pebble",
    ])
  })

  it("refuses up front when the catalog does not ship a requirement", () => {
    const rows = mergeBundles(
      [],
      [
        catalog({
          id: "s.example.com/pebble",
          name: "pebble",
          tier: "sample",
          requires: ["s.example.com/tasks", "s.example.com/nowhere"],
        }),
        catalog({ id: "s.example.com/tasks", tier: "sample" }),
      ]
    )
    const map = new Map(rows.map((r) => [r.id, r]))
    const row = map.get("s.example.com/pebble")!
    const plan = importPlan(row, requirementTree(row, map, new Set()))
    // Nothing is imported: landing tasks and then refusing on the bundle the
    // reader actually asked for is a half-done job.
    expect(plan.bundles).toEqual([])
    expect(plan.refusal).toBe(
      "s.example.com/nowhere is not in the catalog, so it cannot be imported from here. Nothing is imported."
    )
  })
})

describe("closure rows: a name and what it is", () => {
  it("reads each member's declared description, sorted by name", () => {
    const got = closureRows(
      ["x.example.com/p/zebra", "x.example.com/p/apple"],
      {
        "x.example.com/p/apple": "a fruit",
      }
    )
    expect(got).toEqual([
      {
        identity: "x.example.com/p/apple",
        name: "apple",
        description: "a fruit",
      },
      { identity: "x.example.com/p/zebra", name: "zebra" },
    ])
  })

  it("a trigger says what it runs, since it declares nothing else", () => {
    const item = catalog({
      closure: {
        kinds: null,
        traits: null,
        functions: null,
        agents: null,
        mappings: null,
        records: null,
        triggers: ["on-sync"],
        triggerCallables: {
          "on-sync": "substrate.reamde.dev/core/function/x.example.com/p/sync",
        },
      },
    })
    expect(triggerRows(item)).toEqual([
      { identity: "on-sync", name: "on-sync", description: "runs sync" },
    ])
    expect(triggerRows(undefined)).toEqual([])
  })
})

describe("what a sample's mappings link", () => {
  const mapped = mergeBundles(
    [],
    [
      catalog({
        id: "s.example.com/people",
        tier: "sample",
        suggestedMappings: [
          {
            id: "s.example.com/people/fromgithub",
            from: "p.example.com/github/user",
            to: "s.example.com/people/person",
            package: "p.example.com/github",
            state: "waiting",
          },
          {
            id: "s.example.com/people/fromlinear",
            from: "p.example.com/linear/user",
            to: "s.example.com/people/person",
            package: "p.example.com/linear",
            state: "ready",
          },
        ],
      }),
    ]
  )[0]

  it("says it in one sentence, with no state word in it", () => {
    expect(mappingLinksSentence(mapped)).toBe(
      "Links github user and linear user records onto person. " +
        "Each link lands when that provider is installed and this sample is imported again."
    )
  })

  it("says nothing for a closure that ships none", () => {
    expect(mappingLinksSentence(mergeBundles([], [catalog()])[0])).toBe("")
  })

  it("names the ones a re-import would land, and only those", () => {
    expect(readyMappings(mapped).map((m) => m.id)).toEqual([
      "s.example.com/people/fromlinear",
    ])
  })
})
