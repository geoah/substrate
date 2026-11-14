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
import {
  ApiError,
  type BundleUpgrade,
  type BundleUpgradeHolder,
  type KindInfo,
} from "@/lib/api/types"
import { shortDateTime } from "@/lib/format"
import {
  accountKindOf,
  bundleRecordRows,
  isProvider,
  packageRole,
  declaresProviderInterfaces,
  importFailureLines,
  installedKindRows,
  lossyStepLines,
  mergeBundles,
  missingRequirements,
  oauthConnectBlocked,
  presentPackages,
  previewFailed,
  FAILED_PREVIEW_BLOCKER,
  requirementsOf,
  requirementTree,
  missingChain,
  closureRows,
  importPlan,
  triggerRows,
  mappingLinksSentence,
  readyMappings,
  heldVersions,
  needsConfirmation,
  needsConsent,
  confirmationOf,
  changeLines,
  retentionShortLines,
  expectationOf,
  reviewNeeded,
  publisherReviewNeeded,
  sameShortenings,
  holderFreeBlockers,
  holderWords,
  publisherUnreadable,
  settingSetupCount,
  stepLines,
  upgradableBundleCount,
  upgradeBlocked,
  upgradeMotion,
  pendingShippedUpgrades,
  takeRunsOutsideCode,
  type BundleRow,
  type RequirementNode,
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
    keptBy: "publisher",
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

  it("reads who keeps the package from the catalog entry", () => {
    const rows = mergeBundles(
      [],
      [
        catalog({ id: "providers.substrate.reamde.dev/google" }),
        catalog({
          id: "samples.substrate.reamde.dev/tasks",
          name: "tasks",
          authority: "samples.substrate.reamde.dev",
          package: "tasks",
          keptBy: "repository",
        }),
      ]
    )
    const byId = Object.fromEntries(rows.map((r) => [r.id, r]))
    expect(byId["providers.substrate.reamde.dev/google"].keptBy).toBe(
      "publisher"
    )
    expect(byId["~/tasks"].keptBy).toBe("repository")
  })

  it("keys a sample by the id it LANDS under, so its status folds onto it", () => {
    const rows = mergeBundles(
      [
        status({
          id: "~/tasks",
          name: "tasks",
          authority: "~",
          package: "tasks",
        }),
      ],
      [
        catalog({
          id: "samples.substrate.reamde.dev/tasks",
          name: "tasks",
          authority: "samples.substrate.reamde.dev",
          package: "tasks",
          keptBy: "repository",
        }),
      ]
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].id).toBe("~/tasks")
    // The shipped id stays reachable: it is what the import door is called with.
    expect(rows[0].catalog?.id).toBe("samples.substrate.reamde.dev/tasks")
    expect(rows[0].installed).toBe(true)
  })

  describe("two publishers' samples of one package word", () => {
    // Both land at ~/tasks. The copy's origin stamp says which one it is;
    // the id alone would hand it to the later entry.
    const LOCAL = "~"
    const tasks = (authority: string, kind: string, over = {}) =>
      catalog({
        id: `${authority}/tasks`,
        name: "tasks",
        authority,
        package: "tasks",
        description: `Tasks as ${authority} ships them.`,
        keptBy: "repository",
        closure: {
          traits: null,
          triggers: null,
          functions: null,
          agents: null,
          mappings: null,
          records: null,
          kinds: [`${authority}/tasks/${kind}`],
        },
        ...over,
      })
    const copy = (origin?: string) =>
      status({
        id: `${LOCAL}/tasks`,
        name: "tasks",
        authority: LOCAL,
        package: "tasks",
        inputs: undefined,
        ...(origin && { origin }),
      })
    const UPGRADE: BundleUpgrade = {
      available: true,
      from: 1,
      to: 2,
      work: 1,
      lossy: false,
    }

    it.each([
      ["the earlier", "a.example.com", "z.example.com"],
      ["the later", "z.example.com", "a.example.com"],
    ])(
      "pairs a copy stamped with %s entry to it and lists the other as its own row",
      (_, stamped, other) => {
        const kinds = { "a.example.com": "task", "z.example.com": "todo" }
        const rows = mergeBundles(
          [copy(`${stamped}/tasks`)],
          [
            tasks("a.example.com", "task", {
              installed: stamped === "a.example.com",
              upgrade: stamped === "a.example.com" ? UPGRADE : undefined,
            }),
            tasks("z.example.com", "todo", {
              installed: stamped === "z.example.com",
              upgrade: stamped === "z.example.com" ? UPGRADE : undefined,
            }),
          ]
        )
        expect(rows).toHaveLength(2)
        const held = rows.find((r) => r.status)!
        const offered = rows.find((r) => !r.status)!
        expect(held.key).toBe(`${stamped}/tasks`)
        expect(held.id).toBe(`${LOCAL}/tasks`)
        expect(held.installed).toBe(true)
        expect(held.catalog?.id).toBe(`${stamped}/tasks`)
        expect(held.catalog?.description).toBe(
          `Tasks as ${stamped} ships them.`
        )
        expect(held.catalog?.closure.kinds).toEqual([
          `${LOCAL}/tasks/${kinds[stamped as keyof typeof kinds]}`,
        ])
        expect(held.upgrade).toEqual(UPGRADE)
        expect(offered.key).toBe(`${other}/tasks`)
        expect(offered.installed).toBe(false)
        expect(offered.catalog?.id).toBe(`${other}/tasks`)
        expect(offered.catalog?.closure.kinds).toEqual([
          `${LOCAL}/tasks/${kinds[other as keyof typeof kinds]}`,
        ])
        expect(offered.upgrade).toBeUndefined()
        expect(rows.filter((r) => r.keptBy === "repository")).toHaveLength(2)
      }
    )

    it("still pairs an unstamped copy by the id it landed at", () => {
      const rows = mergeBundles(
        [copy()],
        [tasks("a.example.com", "task", { installed: true })]
      )
      expect(rows).toHaveLength(1)
      expect(rows[0].status?.id).toBe(`${LOCAL}/tasks`)
      expect(rows[0].catalog?.id).toBe("a.example.com/tasks")
      expect(rows[0].installed).toBe(true)
    })

    it("pairs an unstamped copy either entry could claim with neither", () => {
      // Taken before the stamp existed: the id is all there is. Both entries
      // read as installed, as the server reports them, so neither offers an
      // import over the copy, and the copy is listed once with no entry, so
      // its own page offers no "Add again" of the wrong sample.
      const rows = mergeBundles(
        [copy()],
        [
          tasks("a.example.com", "task", { installed: true }),
          tasks("z.example.com", "todo", { installed: true }),
        ]
      )
      expect(rows.map((r) => [r.key, r.installed, Boolean(r.status)])).toEqual([
        ["a.example.com/tasks", true, false],
        [`held ${LOCAL}/tasks`, true, true],
        ["z.example.com/tasks", true, false],
      ])
      const own = rows.find((r) => r.status)!
      expect(own.catalog).toBeUndefined()
      expect(own.keptBy).toBeUndefined()
    })

    it("supplies a missing requirement from its own publisher and a met one from the held copy", () => {
      const pebble = (authority: string, over: Partial<CatalogItem> = {}) =>
        catalog({
          id: `${authority}/pebble`,
          name: "pebble",
          authority,
          package: "pebble",
          keptBy: "repository",
          requires: [`${authority}/tasks`],
          ...over,
        })
      const both = [
        tasks("a.example.com", "task"),
        tasks("z.example.com", "todo"),
      ]
      const planOf = (rows: ReturnType<typeof mergeBundles>, key: string) => {
        const row = rows.find((r) => r.key === key)!
        const tree = requirementTree(
          row,
          rows,
          presentPackages(rows),
          heldVersions(rows)
        )
        return { tree, plan: importPlan(row, tree) }
      }

      // Nothing held. a's tasks is the first row at the id, and z's pebble
      // needs z's.
      const offered = mergeBundles([], [pebble("z.example.com"), ...both])
      expect(
        planOf(offered, "z.example.com/pebble").plan.bundles.map((b) => b.key)
      ).toEqual(["z.example.com/tasks", "z.example.com/pebble"])

      // z's copy is held and meets a's pebble's requirement: it is the
      // supplier, and nothing is taken for it.
      const met = mergeBundles(
        [copy("z.example.com/tasks")],
        [pebble("a.example.com"), ...both]
      )
      const { tree } = planOf(met, "a.example.com/pebble")
      expect(tree[0].present).toBe(true)
      expect(tree[0].row?.key).toBe("z.example.com/tasks")

      // Held below a's floor: taking z's sample again would not meet it.
      const low = mergeBundles(
        [{ ...copy("z.example.com/tasks"), version: 3 }],
        [
          pebble("a.example.com", {
            requiresAtLeast: { "a.example.com/tasks": 7 },
          }),
          ...both,
        ]
      )
      expect(
        planOf(low, "a.example.com/pebble").plan.bundles.map((b) => b.key)
      ).toEqual(["a.example.com/tasks", "a.example.com/pebble"])
    })
  })

  it("lists a sample installed verbatim beside its imported copy as two rows", () => {
    const rows = mergeBundles(
      [
        status({
          id: "~/tasks",
          name: "tasks",
          authority: "~",
          package: "tasks",
          origin: "samples.substrate.reamde.dev/tasks",
        }),
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
          keptBy: "repository",
          installed: true,
        }),
      ]
    )
    expect(rows.map((r) => [r.key, r.id, r.keptBy])).toEqual([
      ["samples.substrate.reamde.dev/tasks", "~/tasks", "repository"],
      [
        "held samples.substrate.reamde.dev/tasks",
        "samples.substrate.reamde.dev/tasks",
        undefined,
      ],
    ])
  })

  it("folds a VERBATIM-installed sample onto its own catalog row", () => {
    // The sample was installed rather than imported, so it is held under the
    // SHIPPED id. Keying the row by the landed id alone showed it twice: once
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
          keptBy: "repository",
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
      ]
    )
    expect(rows).toHaveLength(1)
    expect(rows[0].id).toBe("samples.substrate.reamde.dev/tasks")
    expect(rows[0].keptBy).toBe("repository")
    expect(rows[0].installed).toBe(true)
    // Its closure is NOT moved into ~: the kinds it holds are under the
    // authority the tree spells, so a preview under ~ would link nowhere.
    expect(rows[0].catalog?.closure.kinds).toEqual([
      "samples.substrate.reamde.dev/tasks/task",
    ])
  })

  it("moves a sample's requires into ~, because that is what the server looks for", () => {
    const rows = mergeBundles(
      [],
      [
        catalog({
          id: "samples.substrate.reamde.dev/tasks",
          name: "tasks",
          authority: "samples.substrate.reamde.dev",
          package: "tasks",
          keptBy: "repository",
          requires: [
            "samples.substrate.reamde.dev/people",
            "substrate.reamde.dev/core",
          ],
        }),
      ]
    )
    expect(rows[0].requires).toEqual(["~/people", "substrate.reamde.dev/core"])
  })

  it("leaves a provider's id and requires exactly as published", () => {
    const rows = mergeBundles(
      [],
      [catalog({ requires: ["samples.substrate.reamde.dev/people"] })]
    )
    expect(rows[0].id).toBe("providers.substrate.reamde.dev/google")
    expect(rows[0].requires).toEqual(["samples.substrate.reamde.dev/people"])
  })

  it("an applied-only bundle claims no keeper: the catalog states it, nothing derives it", () => {
    const rows = mergeBundles([status({ id: "x.example.com/x" })], [])
    expect(rows[0].keptBy).toBeUndefined()
    expect(rows[0].requires).toEqual([])
  })
})

describe("packageRole", () => {
  const rows = mergeBundles(
    [status({ id: "x.example.com/x", name: "x" })],
    [
      catalog({ id: "providers.substrate.reamde.dev/google", provider: true }),
      catalog({
        id: "providers.substrate.reamde.dev/slacklive",
        provider: true,
        addonFor: "providers.substrate.reamde.dev/slack",
      }),
      catalog({
        id: "substrate.reamde.dev/clinical",
        brings: ["collections"],
      }),
      catalog({ id: "apps.example/coach", brings: ["agents"] }),
      catalog({
        id: "samples.substrate.reamde.dev/tasks",
        name: "tasks",
        authority: "samples.substrate.reamde.dev",
        package: "tasks",
        keptBy: "repository",
      }),
    ]
  )
  const role = (id: string) => packageRole(rows.find((r) => r.id === id)!).short

  // Decision record 0238: who keeps a package is not what it is for. What it
  // is for comes from the catalog, so a catalogue is no provider.
  it("names each package by what it is for, not by who keeps it", () => {
    expect(role("providers.substrate.reamde.dev/google")).toBe("Provider")
    expect(role("providers.substrate.reamde.dev/slacklive")).toBe("Add-on")
    expect(role("substrate.reamde.dev/clinical")).toBe("Standard collection")
    expect(role("apps.example/coach")).toBe("Published")
    expect(role("~/tasks")).toBe("Sample")
    expect(role("x.example.com/x")).toBe("Added by hand")
  })

  it("reads a held package with no catalog entry from its status", () => {
    const [held] = mergeBundles(
      [status({ id: "x.example.com/crm", name: "crm", provider: true })],
      []
    )
    expect(isProvider(held)).toBe(true)
    expect(packageRole(held).short).toBe("Provider")
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
              record: "~/firecrawl/apiKey",
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
              record: "~/web/baseURL",
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
      work: 9,
      lossy: true,
      planHash: "abc",
      changelogSeq: 41,
      steps: [
        {
          step: "rename",
          kind: "substrate.reamde.dev/llm/provider",
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
          step: "enter",
          kind: "geoah.example.com/shop/widget",
          property: "attention",
          to: "quiet",
          records: 2,
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
      "Renames label to displayLabel on 3 records in Providers (substrate.reamde.dev/llm/provider)",
      "Fills in size with its default on 1 record in Widgets (geoah.example.com/shop/widget)",
      "Sets attention to quiet on 2 records in Widgets (geoah.example.com/shop/widget)",
      "Changes status from active to open on 2 records in Widgets (geoah.example.com/shop/widget); afterwards the two can’t be told apart",
      "Removes color from 1 record in Widgets (geoah.example.com/shop/widget); the old values stay in History",
    ])
    // The dialog lists the lossy steps alone: what the click consents to.
    expect(lossyStepLines(plan)).toEqual([
      "Changes status from active to open on 2 records in Widgets (geoah.example.com/shop/widget); afterwards the two can’t be told apart",
      "Removes color from 1 record in Widgets (geoah.example.com/shop/widget); the old values stay in History",
    ])
  })

  it("names both ends of a move in full, since two packages may share a name", () => {
    expect(
      stepLines({
        work: 4,
        lossy: false,
        steps: [
          {
            step: "move",
            kind: "geoah.example.com/shop/widget",
            property: "",
            from: "geoah.example.com/old/widget",
            to: "geoah.example.com/shop/widget",
            records: 4,
          },
        ],
      })
    ).toEqual([
      "Moves 4 records from Widgets (geoah.example.com/old/widget) to Widgets (geoah.example.com/shop/widget); every reference follows",
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

describe("requirementsOf: what is taken first", () => {
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
  })
})

describe("requiresAtLeast: the floor under a requirement (decision record 0070)", () => {
  const present = new Set(["~/people", "~/scheduling"])
  const versions = new Map([
    ["~/people", 3],
    ["~/scheduling", 2],
  ])
  const row = {
    requires: ["~/people", "~/scheduling"],
    requiresAtLeast: { "~/people": 4 },
  }

  it("marks a package held below its floor missing, with both versions", () => {
    const reqs = requirementsOf(row, present, versions)
    expect(reqs).toEqual([
      {
        package: "~/people",
        present: false,
        atLeast: 4,
        held: 3,
      },
      { package: "~/scheduling", present: true, held: 2 },
    ])
  })

  it("is met at the floor and above it", () => {
    for (const held of [4, 5]) {
      const reqs = requirementsOf(row, present, new Map([["~/people", held]]))
      expect(reqs[0].present).toBe(true)
    }
  })

  it("takes a package the kind registry alone knows as meeting the floor", () => {
    // No bundle status reports a version for it, so the console cannot say
    // it is too old; the server's admission is the one that refuses.
    expect(requirementsOf(row, present, new Map())[0].present).toBe(true)
  })

  it("reads the held versions off the installed rows' statuses", () => {
    const rows = mergeBundles(
      [status({ id: "~/people", version: 3 })],
      [
        catalog({ id: "~/people", installed: true }),
        catalog({ id: "~/tasks", installed: false }),
      ]
    )
    expect([...heldVersions(rows)]).toEqual([["~/people", 3]])
  })
})

describe("a published upgrade's holders", () => {
  const workouts: BundleUpgradeHolder = {
    clientName: "Workouts",
    clientId: "https://workouts.example.com/client.json",
    softwareVersion: "2.1",
    checkedAt: "2026-11-03T10:12:00Z",
    floor: 3,
  }
  const held: BundleUpgrade = {
    available: true,
    work: 0,
    lossy: false,
    from: 3,
    to: 4,
    holders: [workouts],
    blockers: [
      'Workouts (https://workouts.example.com/client.json), build "2.1", asks for version 3; last checked 2026-11-03T10:12:00Z',
      "training/session: 3 records still hold nickname",
    ],
  }

  it("drops the blocker lines that restate a holder, and keeps the rest", () => {
    expect(holderFreeBlockers(held.blockers!, held.holders!)).toEqual([
      "training/session: 3 records still hold nickname",
    ])
    expect(holderFreeBlockers(held.blockers!, [])).toEqual(held.blockers)
  })

  it("says a holder in one line, with or without a build and a check-in", () => {
    expect(holderWords(workouts)).toBe(
      `Workouts build 2.1 asks for version 3; last checked in ${shortDateTime(
        "2026-11-03T10:12:00Z"
      )}`
    )
    expect(
      holderWords({
        clientName: "",
        clientId: "https://workouts.example.com/client.json",
        floor: 3,
      })
    ).toBe(
      "https://workouts.example.com/client.json asks for version 3; no check-in recorded"
    )
  })

  it("reads an unreadable publisher off the review flag and the line that names it, never off a holder", () => {
    const unread: BundleUpgrade = {
      available: true,
      work: 0,
      lossy: false,
      reviewRequired: true,
      blockers: ["the publisher no longer lists held version 3"],
    }
    expect(publisherUnreadable(unread)).toBe(true)
    expect(publisherUnreadable({ ...unread, holders: [workouts] })).toBe(false)
    expect(publisherUnreadable({ ...unread, reviewRequired: false })).toBe(
      false
    )
    expect(
      publisherUnreadable({
        ...unread,
        blockers: ["training/session: 3 records still hold nickname"],
      })
    ).toBe(false)
    expect(publisherUnreadable(undefined)).toBe(false)
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
      confirm: { planHash: "d15c", changelogSeq: 9 },
    })
    expect(confirmationOf(base)).toBeUndefined()
  })

  it("asks for a review where code changed or the server marked one, and binds it to the version reviewed", () => {
    const review: BundleUpgrade = {
      ...base,
      planHash: undefined,
      reviewRequired: true,
      candidateVersion: 4,
      candidateDigest: "sha256:abc",
    }
    expect(reviewNeeded(base)).toBe(false)
    expect(reviewNeeded(review)).toBe(true)
    expect(
      reviewNeeded({
        ...base,
        codeChanges: [{ kind: "function", id: "x/y/z", diff: "-a\n+b" }],
      })
    ).toBe(true)
    // A review is not a lossy confirmation, so the chain's consent dialog
    // does not take it for one.
    expect(needsConfirmation(review)).toBe(false)
    expect(needsConsent(review)).toBe(true)
    expect(expectationOf(review)).toEqual({ version: 4, digest: "sha256:abc" })
    expect(expectationOf(base)).toBeUndefined()
    expect(confirmationOf(review)).toEqual({
      expect: { version: 4, digest: "sha256:abc" },
    })
    // Both at once: the plan's consent and the reviewed version.
    expect(
      confirmationOf({ ...review, planHash: "d15c", lossy: true })
    ).toEqual({
      confirm: { planHash: "d15c", changelogSeq: 9 },
      expect: { version: 4, digest: "sha256:abc" },
    })
    // A review the preview did not version has nothing to bind to.
    expect(
      confirmationOf({ ...review, candidateVersion: undefined })
    ).toBeUndefined()
  })

  it("asks for a review of history kept for less time at every tier, and binds it by reading the preview again", () => {
    // A provider the binary ships: no publisher's digest, no plan.
    const shipped: BundleUpgrade = {
      available: true,
      work: 0,
      lossy: false,
      from: 3,
      to: 4,
      retentionShortens: [
        { kind: "providers.example/mail/message", from: "all", to: "P30D" },
      ],
    }
    expect(publisherReviewNeeded(shipped)).toBe(false)
    expect(reviewNeeded(shipped)).toBe(true)
    expect(needsConsent(shipped)).toBe(true)
    // The door takes no consent to a shortening: the click sends the bare
    // body, and nothing marks the review unbound.
    expect(confirmationOf(shipped)).toEqual({})
    // With a plan, its confirmation; with a publisher, the version reviewed.
    expect(
      confirmationOf({
        ...shipped,
        lossy: true,
        planHash: "d15c",
        changelogSeq: 9,
      })
    ).toEqual({ confirm: { planHash: "d15c", changelogSeq: 9 } })
    expect(
      confirmationOf({
        ...shipped,
        reviewRequired: true,
        candidateVersion: 4,
        candidateDigest: "sha256:abc",
      })
    ).toEqual({ expect: { version: 4, digest: "sha256:abc" } })
    // What binds it: the same kinds shortened the same way, at the same
    // version.
    expect(sameShortenings(shipped, { ...shipped })).toBe(true)
    expect(sameShortenings(shipped, { ...shipped, to: 5 })).toBe(false)
    expect(
      sameShortenings(shipped, {
        ...shipped,
        retentionShortens: [
          { kind: "providers.example/mail/message", from: "all", to: "P7D" },
        ],
      })
    ).toBe(false)
    expect(
      sameShortenings(shipped, { ...shipped, retentionShortens: undefined })
    ).toBe(false)
  })

  it("lists the declarations an upgrade moves, with their versions", () => {
    expect(
      changeLines({
        ...base,
        changes: [
          { kind: "kind", id: "x/training/session", from: 2, to: 3 },
          { kind: "function", id: "x/training/plan", to: 3 },
          { kind: "kind", id: "x/training/legacy", from: 2 },
        ],
      })
    ).toEqual([
      "kind x/training/session: version 2 → 3",
      "function x/training/plan: added at version 3",
      "kind x/training/legacy: removed",
    ])
    expect(changeLines(undefined)).toEqual([])
  })
})

describe("importFailureLines — the server's refusal, verbatim", () => {
  it("shows the admission problems, which name what to import first", () => {
    const error = new ApiError(
      "validation",
      "validation error: [bundle providers.substrate.reamde.dev/google: …]",
      422,
      [
        "bundle providers.substrate.reamde.dev/google: data.requires names samples.substrate.reamde.dev/people, which this repository does not have — import that authority's bundle first",
      ]
    )
    expect(importFailureLines(error)).toEqual([
      "bundle providers.substrate.reamde.dev/google: data.requires names samples.substrate.reamde.dev/people, which this repository does not have — import that authority's bundle first",
    ])
  })

  it("keeps every problem as its own line, in the server's order, and never drops one", () => {
    const error = new ApiError("validation", "validation error", 422, [
      "first problem",
      "second problem",
      "first problem",
    ])
    expect(importFailureLines(error)).toEqual([
      "first problem",
      "second problem",
    ])
  })

  it("falls back to the envelope message when there are no problems", () => {
    expect(
      importFailureLines(new ApiError("forbidden", "owner only", 403))
    ).toEqual(["owner only"])
    expect(importFailureLines(new Error("network error"))).toEqual([
      "network error",
    ])
    expect(importFailureLines(undefined)).toEqual(["It couldn’t be added."])
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
            { kind: "substrate.reamde.dev/llm/provider", id: "anthropic" },
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
        kind: "substrate.reamde.dev/llm/provider",
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
  // repository holds none of them. The wire says only the direct one. Each
  // is a sample, so each lands under ~ and names its requirements there.
  const sample = (pkg: string, over: Partial<CatalogItem> = {}) =>
    catalog({
      id: `s.example.com/${pkg}`,
      name: pkg,
      authority: "s.example.com",
      package: pkg,
      keptBy: "repository",
      ...over,
    })
  const rows = mergeBundles(
    [],
    [
      sample("pebble", { requires: ["s.example.com/tasks"] }),
      sample("tasks", {
        requires: ["s.example.com/people", "s.example.com/scheduling"],
      }),
      sample("people"),
      sample("scheduling"),
    ]
  )
  const pebble = rows.find((r) => r.id === "~/pebble")!

  it("walks the chain the wire does not carry, and names each supplier", () => {
    const tree = requirementTree(pebble, rows, new Set())
    expect(tree.map((n) => n.package)).toEqual(["~/tasks"])
    expect(tree[0].row?.name).toBe("tasks")
    expect(tree[0].requires.map((n) => n.package)).toEqual([
      "~/people",
      "~/scheduling",
    ])
  })

  it("orders the missing ones leaves first, each once", () => {
    const chain = missingChain(requirementTree(pebble, rows, new Set()))
    expect(chain.map((n) => n.package)).toEqual([
      "~/people",
      "~/scheduling",
      "~/tasks",
    ])
  })

  it("drops what this repository already holds", () => {
    const chain = missingChain(
      requirementTree(pebble, rows, new Set(["~/people"]))
    )
    expect(chain.map((n) => n.package)).toEqual(["~/scheduling", "~/tasks"])
  })

  it("takes a package held below its floor again, in the same pass", () => {
    const floored = mergeBundles(
      [],
      [
        sample("pebble", {
          requires: ["s.example.com/tasks"],
          requiresAtLeast: { "s.example.com/tasks": 4 },
        }),
        sample("tasks"),
      ]
    )
    const chain = missingChain(
      requirementTree(
        floored.find((r) => r.id === "~/pebble")!,
        floored,
        new Set(["~/tasks"]),
        new Map([["~/tasks", 3]])
      )
    )
    expect(chain.map((n) => n.package)).toEqual(["~/tasks"])
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
    const one = cyclic.find((r) => r.id === "a.example.com/one")!
    const tree = requirementTree(one, cyclic, new Set())
    // The walk stops where it comes back round, and says so.
    expect(tree[0].requires[0].cycle).toBe(true)
    const plan = importPlan(one, tree)
    expect(plan.bundles).toEqual([])
    expect(plan.refusal).toBe(
      "a.example.com/two and a.example.com/one require each other, so there is no order to add them in. Nothing is added."
    )
  })

  it("plans the chain leaves first with the bundle last, and only once", () => {
    const plan = importPlan(pebble, requirementTree(pebble, rows, new Set()))
    expect(plan.refusal).toBe("")
    expect(plan.bundles.map((b) => b.id)).toEqual([
      "~/people",
      "~/scheduling",
      "~/tasks",
      "~/pebble",
    ])
    // The add route is called with each one's shipped id.
    expect(plan.bundles.map((b) => b.catalog?.id)).toEqual([
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
        sample("pebble", {
          requires: ["s.example.com/tasks", "s.example.com/nowhere"],
        }),
        sample("tasks"),
      ]
    )
    const row = rows.find((r) => r.id === "~/pebble")!
    const plan = importPlan(row, requirementTree(row, rows, new Set()))
    // Nothing is imported: landing tasks and then refusing on the bundle the
    // reader actually asked for is a half-done job.
    expect(plan.bundles).toEqual([])
    expect(plan.refusal).toBe(
      "~/nowhere is not in the catalog, so it cannot be added from here. Nothing is added."
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
        keptBy: "repository",
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
        "Each link lands when that provider is installed and this sample is added again."
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

// Code a publisher outside this server published is `experimental` at 1.0,
// where a person takes it: a package a publisher's document published whose
// closure runs functions, or one that brings such a package first. The
// binary's own packages, a package with no code and a requirement already
// held are not labelled.
describe("takeRunsOutsideCode", () => {
  const publisher = {
    authority: "tools.example.com",
    documentUrl: "https://tools.example.com/.well-known/substrate.json",
  }
  const closure = (functions: string[] | null) => ({
    kinds: null,
    traits: null,
    functions,
    agents: null,
    mappings: null,
    records: null,
    triggers: null,
  })
  const row = (id: string, over: Partial<CatalogItem> = {}): BundleRow => ({
    key: id,
    id,
    name: id.split("/").pop()!,
    authority: id.split("/")[0],
    package: id.split("/").pop()!,
    installed: false,
    requires: [],
    catalog: {
      id,
      name: id.split("/").pop()!,
      authority: id.split("/")[0],
      package: id.split("/").pop()!,
      description: "",
      version: 1,
      keptBy: "publisher",
      installed: false,
      closure: closure(null),
      ...over,
    },
  })
  const node = (r: BundleRow, present: boolean): RequirementNode => ({
    package: r.id,
    present,
    row: r,
    requires: [],
  })
  const outside = row("tools.example.com/sync", {
    publisher,
    closure: closure(["tools.example.com/sync/pull"]),
  })
  const words = row("tools.example.com/words", { publisher })
  const shipped = row("providers.substrate.reamde.dev/google", {
    closure: closure(["providers.substrate.reamde.dev/google/sync"]),
  })

  it("labels a published package whose closure runs functions", () => {
    expect(takeRunsOutsideCode(outside, [])).toBe(true)
  })
  it("leaves a package with no functions, and one the binary ships", () => {
    expect(takeRunsOutsideCode(words, [])).toBe(false)
    expect(takeRunsOutsideCode(shipped, [])).toBe(false)
  })
  it("labels a package that brings such code first, and not one whose requirement is held", () => {
    expect(takeRunsOutsideCode(words, [node(outside, false)])).toBe(true)
    expect(takeRunsOutsideCode(words, [node(outside, true)])).toBe(false)
  })
})

describe("retentionShortLines", () => {
  it("says each kind an upgrade keeps for less time, in words", () => {
    const upgrade: BundleUpgrade = {
      available: true,
      work: 0,
      lossy: false,
      retentionShortens: [
        { kind: "publisher.example/journal/entry", from: "all", to: "P30D" },
      ],
    }
    expect(retentionShortLines(upgrade)).toEqual([
      "publisher.example/journal/entry: all of it → 30 days",
    ])
    expect(retentionShortLines(undefined)).toEqual([])
  })
})
