/** The Registry page's pure fold: it merges the two reads the page makes —
 * the imported bundles' runtime status and the shipped catalog closures — into
 * one row set, plus the small domain helpers the registry list and the
 * bundle detail share (the two keepers' sections and the provider-copy gate).
 * Kept out of the page component modules so the pages stay component-only
 * (react-refresh) and this stays unit-testable.
 *
 * Vocabulary: the surface listing them is the REGISTRY and `bundle` stays the
 * schema/API term. A catalog package has one of two KEEPERS (decision record
 * 0258): a PUBLISHED package is kept by its publisher and lands under the
 * authority that publishes it, a SAMPLE is kept by the repository, copied
 * into its local namespace `~` (decision record 0259) and the repository's to
 * edit afterwards. What a package is FOR is the catalog's
 * second answer (decision record 0238): a provider, an add-on for one, or a
 * standard collection that adds collections. Both are backend fields, never
 * derived from an authority's shape here. A bundle owns exactly one PACKAGE
 * (decision 0047), and its id IS that package identity. */

import type { BundleStatus, InputStatus, SetupItem } from "@/lib/api/bundles"
import {
  heldCopyOf,
  landedCatalog,
  landedId,
  rehomeAuthority,
  type CatalogItem,
} from "@/lib/api/catalog"
import { CORE_PACKAGE } from "@/lib/api/http"
import type {
  BundleInstallRequest,
  BundleUpgrade,
  BundleUpgradeHolder,
  CatalogKeeper,
  ConversionConfirm,
  ConversionPlan,
  KindInfo,
  ShippedUpgrade,
  SuggestedMapping,
} from "@/lib/api/types"
import { kindByIdentity, kindPackage, splitKind } from "@/lib/definition"
import { shortDateTime } from "@/lib/format"
import { displayPlural } from "@/lib/kind-names"
import { retentionText } from "@/lib/retention"
import { kindHasTrait } from "@/lib/sync"

/** One bundle row: the installed status (when the lifecycle knows it) and the
 * catalog entry (when it is a shipped closure) it is a copy of. `keptBy`
 * comes from the catalog (backend field). */
export interface BundleRow {
  /** What tells this row from every other, for list keys and lookups: the
   * catalog entry's shipped id, which the catalog never repeats, or the id
   * of a held bundle no entry claims. `id` is not unique: two publishers'
   * samples of one package word both land at `~/<package>`. */
  key: string
  /** The bundle's id IN THIS REPOSITORY: the package identity it owns once
   * it lands. A provider keeps its published id; an added sample sits in the
   * local namespace (`~/<package>`), so this is not always the catalog's id.
   * The add route is addressed by `catalog.id`. */
  id: string
  name: string
  authority: string
  /** The owned package's own word. */
  package: string
  status?: BundleStatus
  catalog?: CatalogItem
  installed: boolean
  /** Who keeps this row's package. Absent on a bundle applied outside the
   * shipped catalog: the keeper is the catalog's to state, and guessing one
   * from an authority's shape is the derivation the backend refuses to make. */
  keptBy?: CatalogKeeper
  /** The packages this closure declares against, named as they will be HERE:
   * a sample's own move into `~`, because that is what the server will look
   * for. Admission refuses while one is missing
   * (catalog.Bundle.requires). */
  requires: string[]
  /** The floor under each required package (decision record 0070), keyed as
   * `requires` names them: the least version that satisfies the requirement.
   * Absent where the closure declares none. */
  requiresAtLeast?: Record<string, number>
  /** The upgrade preview (server-computed, catalog read): present when the
   * shipped closure moved past what this repository stores, when the server
   * could not preview it, and, for a sample copy edited since it was
   * imported, so the re-import can confirm it (decision record 0070). */
  upgrade?: BundleUpgrade
}

/** Fold the two reads into one row set: each catalog entry is one row,
 * carrying the copy this repository holds of it when there is one (status
 * wins the count columns), and each held bundle no entry claims is a row of
 * its own. A copy is paired with its entry by its origin stamp, and by id
 * only when it has none (heldCopyOf). */
export function mergeBundles(
  statuses: BundleStatus[],
  catalog: CatalogItem[]
): BundleRow[] {
  const rows: BundleRow[] = []
  const matches = catalog.map(
    (raw) => [raw, heldCopyOf(raw, statuses)] as const
  )
  // An unstamped copy at an id two entries land at could be either one's,
  // and nothing says which. The server reads both entries as installed, so
  // both rows say so; the copy pairs with neither, so no page offers to
  // import one of them over it, and it is listed once, on its own row.
  const claims = new Map<string, number>()
  for (const [, copy] of matches) {
    if (copy) claims.set(copy.id, (claims.get(copy.id) ?? 0) + 1)
  }
  const paired = new Set<string>()
  for (const [raw, copy] of matches) {
    const status = copy && claims.get(copy.id) === 1 ? copy : undefined
    if (status) paired.add(status.id)
    // A sample can be here under EITHER id: the import lands it under `~`,
    // and installing it verbatim (still a door until the providers stop
    // requiring sample packages) lands the shipped one. The entry is moved
    // into `~` only when the row is: a closure held verbatim has its kinds
    // under the authority the tree spells, and previewing them under `~`
    // would link nowhere.
    const id = copy?.id ?? landedId(raw)
    const item = id === raw.id ? raw : landedCatalog(raw)
    rows.push({
      key: raw.id,
      id,
      name: status?.name ?? item.name,
      authority: status?.authority ?? item.authority,
      package: status?.package ?? item.package,
      ...(status && { status }),
      catalog: item,
      installed: copy ? copy.installed : item.installed,
      keptBy: item.keptBy,
      requires: item.requires ?? [],
      requiresAtLeast: item.requiresAtLeast ?? {},
      upgrade: item.upgrade,
    })
  }
  for (const status of statuses) {
    if (paired.has(status.id)) continue
    rows.push({
      // A held bundle's id can be a catalog id too (a sample installed
      // verbatim beside its imported copy), so the two keys never share a
      // spelling.
      key: `held ${status.id}`,
      id: status.id,
      name: status.name,
      authority: status.authority,
      package: status.package,
      status,
      installed: status.installed,
      requires: [],
      requiresAtLeast: {},
    })
  }
  // Not taken first (they invite an action), then held; alpha in each.
  return rows.sort(
    (a, b) =>
      Number(a.installed) - Number(b.installed) ||
      a.id.localeCompare(b.id) ||
      a.key.localeCompare(b.key)
  )
}

// ── what a package is for (decision record 0238) ────────────────────────────

/** A provider: the package connects the substrate to an outside service, as
 * its catalog entry says, or, held with no entry (applied by hand), as its
 * status says from the declarations stored. Providers lists it, unless it
 * is an add-on. */
export function isProvider(
  row: Pick<BundleRow, "catalog" | "status">
): boolean {
  return Boolean(row.catalog?.provider ?? row.status?.provider)
}

/** The provider an add-on extends: its page lists the add-on, and Providers
 * does not. */
export function addonOf(
  row: Pick<BundleRow, "catalog" | "status">
): string | undefined {
  return (
    (row.catalog ? row.catalog.addonFor : row.status?.addonFor) || undefined
  )
}

/** A standard collection: published, neither a provider nor an add-on, and it
 * adds collections or metrics (`health`, `labs`, `clinical`, an app's own
 * package). Add a collection lists it beside the samples. */
export function isStandard(
  row: Pick<BundleRow, "keptBy" | "catalog" | "status">
): boolean {
  const brings = row.catalog?.brings ?? []
  return (
    row.keptBy === "publisher" &&
    !isProvider(row) &&
    !addonOf(row) &&
    (brings.includes("collections") || brings.includes("metrics"))
  )
}

/** A held package the Providers page lists as a provider of its own, read
 * off its status alone: what the counts beside Providers count. */
export function listedProvider(
  status: Pick<BundleStatus, "provider" | "addonFor">
): boolean {
  return Boolean(status.provider && !status.addonFor)
}

/** What a package is, in the reader's words: `short` for a table cell,
 * `long` for a page's line. One answer for every page, so a package reads
 * the same wherever it is listed. */
export function packageRole(
  row: Pick<BundleRow, "keptBy" | "catalog" | "status">
): {
  short: string
  long: string
} {
  if (row.keptBy === "repository")
    return { short: "Sample", long: "A sample you added" }
  if (addonOf(row)) return { short: "Add-on", long: "An add-on for a provider" }
  if (isProvider(row)) return { short: "Provider", long: "A provider" }
  if (isStandard(row))
    return {
      short: "Standard collection",
      long: "A standard collection, kept up to date for you",
    }
  if (row.keptBy === "publisher")
    return { short: "Published", long: "Kept up to date by its publisher" }
  return { short: "Added by hand", long: "Added by hand" }
}

// ── upgrades: the shipped closure moved past the stored one ─────────────────

/** Whether a row has an upgrade to offer or to explain: the server attaches
 * the preview only to an installed bundle whose closure moved, so presence is
 * the signal. A blocked upgrade still counts (it needs the reader's hand). */
export function upgradeAvailable(row: Pick<BundleRow, "upgrade">): boolean {
  return Boolean(row.upgrade?.available)
}

/** A blocked upgrade: the server named blockers, so the console shows the
 * guard lines and no button. Usually the refuse-breakage guards on a moved
 * closure (`available` too); a preview the server could not run is the other
 * case, one fixed line and no motion, and it is stated the same way rather
 * than dropped. */
export function upgradeBlocked(row: Pick<BundleRow, "upgrade">): boolean {
  return Boolean(row.upgrade?.blockers?.length)
}

/** The one blocker line the server leaves when the preview itself failed
 * (api `failedPreviewBlocker`, fixed text so no error names the deployment).
 * Keyed on verbatim: the disclosure must not say live records block an
 * upgrade nobody could preview. */
export const FAILED_PREVIEW_BLOCKER =
  "the upgrade preview failed; see the server log"

/** A blocker line as the reader sees it: the server's fixed failed-preview
 * line is keyed on verbatim, so it is reworded here, never on the wire. */
export function blockerWords(line: string): string {
  return line === FAILED_PREVIEW_BLOCKER
    ? "The update couldn’t be checked."
    : line
}

export function previewFailed(row: Pick<BundleRow, "upgrade">): boolean {
  return row.upgrade?.blockers?.includes(FAILED_PREVIEW_BLOCKER) ?? false
}

/** The connected app builds holding a published package's upgrade back
 * (decision 0214): each still asks for the version the repository holds,
 * so taking the upgrade would leave it on a package it does not know. */
export function upgradeHolders(
  upgrade: BundleUpgrade | undefined
): BundleUpgradeHolder[] {
  return upgrade?.holders ?? []
}

/** The blocker lines that are not a holder restated: the server writes one
 * line per holder beside the structured list, naming the build by its
 * client id, so a note that lists the builds drops them.
 *
 * This keys on the server's sentence (catalog refresh.go), as
 * `publisherUnreadable` does: a reworded line slips through unnoticed. Both
 * should read a structured reason on the wire instead. */
export function holderFreeBlockers(
  blockers: readonly string[],
  holders: readonly BundleUpgradeHolder[]
): string[] {
  return blockers.filter(
    (line) => !holders.some((h) => line.includes(`(${h.clientId})`))
  )
}

/** The preview could not check what the repository holds against the
 * publisher's document (catalog refresh.go, heldClosure): the publisher no
 * longer lists the held version, changed its digest, or the package stopped
 * following it. The server says so in a blocker line naming the publisher
 * and marks the upgrade for review, with no structured field, so the line
 * is keyed on. */
export function publisherUnreadable(
  upgrade: BundleUpgrade | undefined
): boolean {
  return (
    Boolean(upgrade?.reviewRequired) &&
    upgradeHolders(upgrade).length === 0 &&
    (upgrade?.blockers ?? []).some((line) => /publisher/i.test(line))
  )
}

/** One holder as a line: "Workouts build 2.1 asks for version 3; last
 * checked in 3 Nov 2026, 10:12". */
export function holderWords(holder: BundleUpgradeHolder): string {
  const name = holder.clientName || holder.clientId
  const build = holder.softwareVersion ? ` build ${holder.softwareVersion}` : ""
  const checked = holder.checkedAt
    ? `last checked in ${shortDateTime(holder.checkedAt)}`
    : "no check-in recorded"
  return `${name}${build} asks for version ${holder.floor}; ${checked}`
}

/** The shipped packages whose upgrade has not landed here: what the Registry
 * states above its sections. `available` is the whole test: the binary ships
 * a newer declaration than the repository stores. With blockers the boot
 * refused it and the guard lines say what to migrate; without them the
 * upgrade is admitted, but the boot runs at a repository's first open under
 * a binary, so it lands only when the server starts again. Filtering on the
 * blockers alone would drop the notice the moment the last blocking record
 * is migrated, while the stored declarations stay old. */
export function pendingShippedUpgrades(
  items: ShippedUpgrade[]
): ShippedUpgrade[] {
  return items.filter((item) => item.upgrade.available)
}

/** The sidebar badge's number: installed bundles whose shipped closure moved
 * or whose upgrade the server blocks, computed straight off the catalog read
 * so the sidebar needs no second endpoint. A blocked entry counts whether or
 * not it is `available`, so the badge and the row's chip agree. */
export function upgradableBundleCount(catalog: CatalogItem[]): number {
  return catalog.filter(
    (item) =>
      item.installed && (item.upgrade?.available || upgradeBlocked(item))
  ).length
}

/** One line per conversion step the upgrade runs (decision 0067), with the
 * live records it rewrites: the operator sees the rewrite the server will make
 * before it makes it, and a lossy step says so. Empty when nothing moves. */
export function stepLines(plan: ConversionPlan | undefined): string[] {
  // The friendly plural alone is ambiguous where two packages name a kind
  // alike, and these lines are what a lossy update's confirmation lists.
  const named = (ref: string) => `${displayPlural(ref)} (${ref})`
  return (plan?.steps ?? []).map((s) => {
    const n = `${s.records} ${s.records === 1 ? "record" : "records"}`
    const where = s.kind ? ` in ${named(s.kind)}` : ""
    switch (s.step) {
      case "move":
        return `Moves ${n} from ${named(s.from ?? "")} to ${named(s.to ?? "")}; every reference follows`
      case "rename":
        return `Renames ${s.from} to ${s.to} on ${n}${where}`
      case "backfill":
        return `Fills in ${s.property} with its default on ${n}${where}`
      case "enter":
        return `Sets ${s.property} to ${s.to} on ${n}${where}`
      case "remap":
        return `Changes ${s.property} from ${s.from} to ${s.to} on ${n}${where}${
          s.lossy ? "; afterwards the two can’t be told apart" : ""
        }`
      default:
        return `Removes ${s.property} from ${n}${where}; the old values stay in History`
    }
  })
}

/** The steps that remove values from the fold: what the confirmation dialog
 * lists before it asks. */
export function lossyStepLines(plan: ConversionPlan | undefined): string[] {
  return stepLines(
    plan && { ...plan, steps: (plan.steps ?? []).filter((s) => s.lossy) }
  )
}

/** "2 → 3", or just the one version when there is no motion to show: the
 * store held none, or the AUTHORITY version did not move because what moved
 * was a kind's own version or a kind the closure added. Both are legal
 * upgrades (AGENTS.md), and "3 → 3" would read as a bug. Versions are
 * incremental integers; 0 and undefined both mean absent, because the wire
 * omits a zero. */
export function upgradeMotion(upgrade: BundleUpgrade): string {
  const from = upgrade.from || undefined
  const to = upgrade.to || undefined
  if (from !== undefined && to !== undefined && from !== to) {
    return `${from} → ${to}`
  }
  const one = to ?? from
  return one === undefined ? "" : String(one)
}

// ── requirements: what must be imported first ───────────────────────────────

/** One entry of a closure's `requires:` — a PACKAGE it declares against —
 * resolved against what this repository already holds, and against the floor
 * the closure puts under it (`requiresAtLeast`, decision record 0070). */
export interface Requirement {
  /** The required package identity, exactly as the closure names it. */
  package: string
  /** This repository has it at a version that satisfies the floor, so
   * admission will not refuse for it. */
  present: boolean
  /** The least version the closure declares against, when it pins one. */
  atLeast?: number
  /** The version this repository holds the package at, when its bundle
   * status says; absent for a package known only to the kind registry. */
  held?: number
}

/** The stored version of each package this repository holds through a
 * bundle, by identity: what a floor is compared against. A package the kind
 * registry knows but no bundle status reports has no version here, and a
 * floor on it is taken as met, since the server is the one that refuses. */
export function heldVersions(rows: BundleRow[]): Map<string, number> {
  const out = new Map<string, number>()
  for (const row of rows) {
    if (row.installed && row.status?.version)
      out.set(row.id, row.status.version)
  }
  return out
}

/** The packages this repository HOLDS, from the two reads the registry page
 * already makes: every imported bundle's owned package, and every package the
 * kind registry has reconciled (which covers core and anything applied outside
 * the shipped catalog). This is the console's read of the check
 * `schema.resolveBundle` runs server-side — a bundle whose status says
 * `installed: false` (uninstalled or quarantined) is NOT in the live registry,
 * so its package does not count. */
export function presentPackages(
  rows: BundleRow[],
  kinds: KindInfo[] = []
): Set<string> {
  const out = new Set<string>()
  for (const row of rows) if (row.installed) out.add(row.id)
  for (const kind of kinds) {
    const identity = kindPackage(kind)
    if (identity) out.add(identity)
  }
  return out
}

/** One row's requirements, each marked present or missing. A package the
 * repository holds below the closure's floor is missing too: the server
 * refuses the same import, naming both versions. */
export function requirementsOf(
  row: Pick<BundleRow, "requires" | "requiresAtLeast">,
  present: ReadonlySet<string>,
  versions: ReadonlyMap<string, number> = new Map()
): Requirement[] {
  return row.requires.map((identity) => {
    const atLeast = row.requiresAtLeast?.[identity]
    const held = versions.get(identity)
    const tooOld = atLeast !== undefined && held !== undefined && held < atLeast
    return {
      package: identity,
      present: present.has(identity) && !tooOld,
      ...(atLeast !== undefined && { atLeast }),
      ...(held !== undefined && { held }),
    }
  })
}

/** The entries a bundle's requirements list that this repository does not
 * satisfy: what the detail page's note and the chain hint are about. */
export function missingRequirements(
  requirements: Requirement[]
): Requirement[] {
  return requirements.filter((r) => !r.present)
}

/** "samples.substrate.reamde.dev/people", "samples.substrate.reamde.dev/people and samples.substrate.reamde.dev/tasks", "a, b and c". */
function andList(names: string[]): string {
  if (names.length <= 1) return names[0] ?? ""
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`
}

/** One node of the TRANSITIVE requirement closure: a required package, what it
 * requires in turn, and the catalog row that supplies it. The wire's
 * `requires` is direct only, so the chain is walked here: importing a bundle
 * whose requirement itself requires two more is one action, not three the
 * reader has to discover one refusal at a time. */
export interface RequirementNode extends Requirement {
  /** The catalog row that would supply this package. Absent when the shipped
   * catalog has no closure for it, which is a requirement nothing here can
   * take: the server's refusal is the one that names it. */
  row?: BundleRow
  /** This package is already on the path that reached it, so the closure
   * requires its way back round. `cycleWith` is the package that names it
   * here, which is the other end of the loop. */
  cycle?: boolean
  cycleWith?: string
  requires: RequirementNode[]
}

/** The row that supplies requirement `req` to `requirer`, matched on the id
 * each row has HERE, which is what a requirement names (a sample's are moved
 * into `~` by landedCatalog before they get this far). One id can name two
 * rows, since two publishers' samples of one package word both land at
 * `~/<package>`. A satisfied requirement is supplied by the copy this
 * repository holds, which is what admission checked. A missing one, absent
 * or held below its floor, is supplied by the entry the requirement named as
 * shipped, before the requirer's own authority moved into `~`: taking
 * another publisher's sample would not satisfy it. */
function supplierOf(
  req: Requirement,
  requirer: BundleRow,
  rows: readonly BundleRow[]
): BundleRow | undefined {
  const at = rows.filter((r) => r.id === req.package)
  if (at.length < 2) return at[0]
  const held = at.find((r) => r.status)
  const item = requirer.catalog
  const publisher = item?.id.slice(0, item.id.lastIndexOf("/"))
  const shipped =
    item && publisher
      ? rehomeAuthority(req.package, item.authority, publisher)
      : req.package
  const named = at.find((r) => r.catalog?.id === shipped)
  return (req.present ? (held ?? named) : (named ?? held)) ?? at[0]
}

/** The requirement closure under one row, walked across catalog entries.
 * `rows` is every row the page folded, and each requirement's supplier is
 * read from it (supplierOf).
 *
 * The row's OWN id starts the walk as seen, so a closure that requires its way
 * back to it stops there and is marked a cycle rather than listing the bundle
 * among the things to import before itself. The closure is data, and data can
 * say anything. */
export function requirementTree(
  row: BundleRow,
  rows: readonly BundleRow[],
  present: ReadonlySet<string>,
  versions: ReadonlyMap<string, number> = new Map(),
  seen: ReadonlySet<string> = new Set([row.id])
): RequirementNode[] {
  return requirementsOf(row, present, versions).map((req) => {
    const supplier = supplierOf(req, row, rows)
    const cycle = seen.has(req.package)
    const next = new Set([...seen, req.package])
    return {
      ...req,
      ...(supplier && { row: supplier }),
      ...(cycle && { cycle: true, cycleWith: row.id }),
      requires:
        supplier && !cycle
          ? requirementTree(supplier, rows, present, versions, next)
          : [],
    }
  })
}

/** Every missing package in the closure, LEAVES FIRST and each named once:
 * the order the imports have to run in, since a bundle is refused while
 * anything it declares against is absent. */
export function missingChain(nodes: RequirementNode[]): RequirementNode[] {
  const out: RequirementNode[] = []
  const seen = new Set<string>()
  const walk = (list: RequirementNode[]) => {
    for (const node of list) {
      walk(node.requires)
      if (node.present || seen.has(node.package)) continue
      seen.add(node.package)
      out.push(node)
    }
  }
  walk(nodes)
  return out
}

/** Whether taking the row runs code a publisher outside this server
 * published: its own closure's functions, or those of a package the take
 * brings first (missingChain). A publisher's document is what marks an entry
 * as another's, the fact the approval page reads too (runsOutsideCode in
 * lib/app-setup). Such code is `experimental` at 1.0 whatever the package's
 * own label says (docs/stability.md). */
export function takeRunsOutsideCode(
  row: BundleRow,
  chain: RequirementNode[]
): boolean {
  const outside = (r: BundleRow | undefined) =>
    Boolean(r?.catalog?.publisher) &&
    (r?.catalog?.closure.functions ?? []).length > 0
  return outside(row) || missingChain(chain).some((node) => outside(node.row))
}

/** What one press of the row's button will do: the bundles to take, leaves
 * first and the row itself last, or the one sentence saying why nothing can
 * be taken at all. Nothing is ever half-planned: a chain that cannot finish
 * imports nothing, rather than landing the leaves and refusing on what the
 * reader actually asked for. */
export interface ImportPlan {
  /** Each bundle to take, in order. Empty when the plan is refused. */
  bundles: BundleRow[]
  /** Why nothing can be taken, in one sentence. Empty when it can. */
  refusal: string
}

export function importPlan(
  row: BundleRow,
  chain: RequirementNode[]
): ImportPlan {
  const loops: string[] = []
  const walk = (nodes: RequirementNode[]) => {
    for (const node of nodes) {
      if (node.cycle && node.cycleWith) {
        loops.push(`${node.cycleWith} and ${node.package}`)
      }
      walk(node.requires)
    }
  }
  walk(chain)
  if (loops.length) {
    return {
      bundles: [],
      refusal: `${andList([...new Set(loops)])} require each other, so there is no order to add them in. Nothing is added.`,
    }
  }
  const missing = missingChain(chain)
  const absent = missing.filter((node) => !node.row).map((node) => node.package)
  if (absent.length) {
    return {
      bundles: [],
      refusal: `${andList(absent)} ${absent.length === 1 ? "is" : "are"} not in the catalog, so ${absent.length === 1 ? "it" : "they"} cannot be added from here. Nothing is added.`,
    }
  }
  return {
    bundles: [...missing.flatMap((node) => (node.row ? [node.row] : [])), row],
    refusal: "",
  }
}

/** Whether taking the upgrade needs the reader's consent first: the plan
 * removes values from the fold (decision 0067) or replaces a sample copy the
 * reader edited (decision record 0070). Either way the click sends the
 * preview's `planHash` and `changelogSeq`, never a bare yes. */
export function needsConfirmation(upgrade: BundleUpgrade | undefined): boolean {
  return Boolean(upgrade?.planHash && (upgrade.lossy || upgrade.discardsEdits))
}

/** Whether the publisher's changes need the reader's review before the
 * upgrade is taken (decision 0220): code that changed, shown as diffs, or
 * what the server marked for review without a diff, a kind removed, a
 * function's reach grown, a held version the document no longer lists. The
 * install binds such a review to the version and digest the preview named. */
export function publisherReviewNeeded(
  upgrade: BundleUpgrade | undefined
): boolean {
  return Boolean(upgrade?.codeChanges?.length || upgrade?.reviewRequired)
}

/** Whether the upgrade needs the reader's review before it is taken: the
 * publisher's changes, or history the upgrade would keep for less time,
 * whatever the package's tier (decision 0282). A provider or a sample the
 * binary ships has no publisher's digest to bind the review to, so its
 * review is bound by reading the preview again at the click
 * (`sameShortenings`). */
export function reviewNeeded(upgrade: BundleUpgrade | undefined): boolean {
  return (
    publisherReviewNeeded(upgrade) ||
    Boolean(upgrade?.retentionShortens?.length)
  )
}

/** Whether a click on the upgrade asks first, for either reason. */
export function needsConsent(upgrade: BundleUpgrade | undefined): boolean {
  return needsConfirmation(upgrade) || reviewNeeded(upgrade)
}

/** What a review binds the install to: the version the preview was of and
 * the digest its publisher listed, so a document that moved since answers
 * 409 instead of landing something nobody reviewed. Undefined where the
 * preview did not say. */
export function expectationOf(
  upgrade: BundleUpgrade | undefined
): BundleInstallRequest["expect"] {
  if (!upgrade?.candidateVersion || !upgrade.candidateDigest) return undefined
  return { version: upgrade.candidateVersion, digest: upgrade.candidateDigest }
}

/** The body the door takes with an upgrade that asks first: the plan's
 * confirmation where it hands one out, and the reviewed version where the
 * preview names one. An upgrade that asks only because it keeps history for
 * less time answers an empty body where the preview has neither, which the
 * door takes as the bare POST: it takes no consent to a shortening, and the
 * click binds it instead (`sameShortenings`). Undefined when the upgrade
 * asks nothing, or when it asks for a confirmation or a publisher's review
 * and the preview gave nothing to bind the answer to. */
export function confirmationOf(
  upgrade: BundleUpgrade | undefined
): BundleInstallRequest | undefined {
  if (!needsConsent(upgrade)) return undefined
  const confirm: ConversionConfirm | undefined = upgrade?.planHash
    ? { planHash: upgrade.planHash, changelogSeq: upgrade.changelogSeq ?? 0 }
    : undefined
  const expect = expectationOf(upgrade)
  if (!confirm && !expect) {
    return needsConfirmation(upgrade) || publisherReviewNeeded(upgrade)
      ? undefined
      : {}
  }
  return { ...(confirm && { confirm }), ...(expect && { expect }) }
}

/** Whether a fresh preview keeps the same history for less time as the one
 * the reader reviewed, at the same version: what binds the review of a
 * shortening where no publisher's digest does. */
export function sameShortenings(
  reviewed: BundleUpgrade | undefined,
  fresh: BundleUpgrade | undefined
): boolean {
  return (
    reviewed?.to === fresh?.to &&
    retentionShortLines(reviewed).join("\n") ===
      retentionShortLines(fresh).join("\n")
  )
}

/** The kinds whose history an upgrade would keep for less time, one line
 * each, for the review that asks first because of them (decision 0282):
 * "example.com/journal/entry: all of it → 30 days". */
export function retentionShortLines(
  upgrade: BundleUpgrade | undefined
): string[] {
  return (upgrade?.retentionShortens ?? []).map(
    (c) => `${c.kind}: ${retentionText(c.from)} → ${retentionText(c.to)}`
  )
}

/** The declarations an upgrade moves, one line each, for a review that
 * carries no code diff: "kind example.com/training/session: version 2 → 3",
 * "function …: added at version 3", "kind …: removed". */
export function changeLines(upgrade: BundleUpgrade | undefined): string[] {
  return (upgrade?.changes ?? []).map((c) => {
    const motion =
      c.from && c.to
        ? `version ${c.from} → ${c.to}`
        : c.to
          ? `added at version ${c.to}`
          : "removed"
    return `${c.kind} ${c.id}: ${motion}`
  })
}

// ── suggested mappings (decision record 0049) ──────────────────────────────

/** The package's own word, the last segment of a package identity. */
function packageWord(pkg: string): string {
  const parts = pkg.split("/")
  return parts[parts.length - 1] ?? pkg
}

/** THE COST OF THE FIX, said wherever the fix is offered: a re-import replaces
 * the package rather than merging into it (decision record 0048), so a kind or
 * a property the reader added since is dropped by it. */
export const REIMPORT_WARNING =
  "Adding it again replaces the package and may remove your changes."

/** What a SAMPLE's mappings do, in one sentence: which provider's records it
 * links onto which of its own kinds, and what makes one land. A provider
 * declares no mapping at all (decision record 0049), so this is empty for one,
 * and empty for a closure that ships none.
 *
 * The provider is named by its package's own word, read off the source kind's
 * package, because that is the bundle the reader would install. */
export function mappingLinksSentence(row: BundleRow): string {
  const mappings = row.catalog?.suggestedMappings ?? []
  if (!mappings.length) return ""
  const sources = andList([
    ...new Set(
      mappings.map((m) => `${packageWord(m.package)} ${splitKind(m.from).name}`)
    ),
  ])
  const targets = andList([
    ...new Set(mappings.map((m) => splitKind(m.to).name)),
  ])
  return (
    `Links ${sources} records onto ${targets}. ` +
    `Each link lands when that provider is installed and this sample is added again.`
  )
}

/** The mappings adding a sample AGAIN would land: their provider is here and
 * they fit it, and only the add is missing. This is what earns a held sample
 * an "Add again" on its own page (decision record 0049); a moved closure is
 * offered the upgrade instead, which is the same route. */
export function readyMappings(row: {
  catalog?: Pick<CatalogItem, "suggestedMappings">
}): SuggestedMapping[] {
  return (row.catalog?.suggestedMappings ?? []).filter(
    (m) => m.state === "ready"
  )
}

/** The server's OWN words for a refused import, one problem per entry.
 * Admission answers with a validation envelope whose `problems` name exactly
 * what to import first, every problem at once; the envelope's `message` is
 * only the sentinel wrapping them. The problems come back verbatim and in
 * the server's order when there are any (a repeated line once), and the
 * message otherwise, so a race (a requirement torn down between the read and
 * the click) reads as the refusal it is rather than a generic failure. The
 * caller renders each entry on its own line (components/import-refusal):
 * joined into one paragraph, a refusal of four problems read as its first
 * clause alone (issue 616). */
export function importFailureLines(error: unknown): string[] {
  const problems = (error as { problems?: unknown } | undefined)?.problems
  if (Array.isArray(problems)) {
    const lines = [
      ...new Set(problems.filter((p): p is string => typeof p === "string")),
    ]
    if (lines.length) return lines
  }
  const message = (error as { message?: unknown } | undefined)?.message
  return [
    typeof message === "string" && message ? message : "It couldn’t be added.",
  ]
}

/** A kind carries a core trait, named in full or in the bare shipped
 * spelling (`lib/sync` kindHasTrait). */
const hasTrait = kindHasTrait

/** The bundle's account-config kind: the kind in its owned package that
 * implements the `accountconfig` trait (the host writes tokens onto its
 * records). Its presence is the signal that the bundle has provider
 * accounts to connect. */
export function accountKindOf(
  kinds: KindInfo[],
  bundlePackage: string
): KindInfo | undefined {
  return kinds.find(
    (k) => kindPackage(k) === bundlePackage && hasTrait(k, "accountconfig")
  )
}

/** One member of a closure as the registry lists it: its own word and the
 * prose its declaration carries. The name alone says nothing, and before a
 * bundle lands there is no registry entry to look it up in, so the catalog's
 * own descriptions are the only ones there are. */
export interface ClosureRow {
  identity: string
  name: string
  description?: string
}

export function closureRows(
  ids: string[] | null | undefined,
  described: Record<string, string> | undefined
): ClosureRow[] {
  return (ids ?? [])
    .map((identity) => ({
      identity,
      name: splitKind(identity).name,
      ...(described?.[identity] && { description: described[identity] }),
    }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

/** The trigger records a closure ships. A trigger's id is a plain record id,
 * not a reference, and core's trigger kind declares no description: what it
 * invokes is the whole of what there is to say about it. */
export function triggerRows(catalog?: CatalogItem): ClosureRow[] {
  const closure = catalog?.closure
  return (closure?.triggers ?? [])
    .map((id) => {
      // A callable is a REFERENCE value, a kind reference and an id, so its
      // last segment is the function's or the agent's own word.
      const callable = closure?.triggerCallables?.[id]
      const word = callable?.split("/").pop()
      return {
        identity: id,
        name: id,
        ...(word && { description: `runs ${word}` }),
      }
    })
    .sort((a, b) => a.name.localeCompare(b.name))
}

// ── bundle-detail closure inventory (the Kinds + Resources tables) ────────

/** One row of the bundle's Kinds table: an kind its closure
 * installed, resolved against the registry for its collection route, plus its
 * role when the host treats it specially (a kind some declared input resolves
 * records of, the `account` kind the connect flow writes tokens onto).
 * `authority` is absent only when the registry has not (yet) reconciled the
 * kind. */
export interface KindRow {
  identity: string
  name: string
  authority?: string
  /** The package's own word; absent with the authority. */
  package?: string
  role?: "input" | "account"
  /** The kind's declared description: a chip says what it is on hover. From
   * the registry once the bundle is imported, and from the catalog's own
   * closure before it is — which is when the reader most needs it. */
  description?: string
}

/** The kind identities a bundle's declared inputs resolve records of, from the
 * two places a declaration can be read: the computed status (once installed)
 * and the shipped catalog entry (before). */
function inputKindsOf(
  inputs?: InputStatus[],
  catalog?: CatalogItem
): Set<string> {
  const out = new Set<string>()
  for (const input of inputs ?? []) out.add(input.kind)
  for (const input of Object.values(catalog?.inputs ?? {})) out.add(input.kind)
  return out
}

/** The kinds an bundle installed, one row each, resolved for the Kinds
 * table. Identities come from the shipped closure when the bundle is a catalog
 * entry; otherwise from the registry itself — every reconciled kind in the
 * bundle's owned authority. Sorted by display name. */
export function installedKindRows(
  bundle: Pick<BundleStatus, "id" | "inputs">,
  kinds: KindInfo[],
  catalog?: CatalogItem
): KindRow[] {
  const fromCatalog = catalog?.closure.kinds ?? []
  const identities =
    fromCatalog.length > 0
      ? fromCatalog
      : kinds.filter((k) => kindPackage(k) === bundle.id).map((k) => k.identity)
  const accountKind = accountKindOf(kinds, bundle.id)
  const inputKinds = inputKindsOf(bundle.inputs, catalog)
  // A bundle the repository has NOT imported has no registry entry for any of
  // its kinds, so the closure's own descriptions are the only ones there are.
  const described = catalog?.closure.kindDescriptions ?? {}
  const rows = identities.map((identity): KindRow => {
    const k = kindByIdentity(kinds, identity)
    const role: KindRow["role"] = inputKinds.has(identity)
      ? "input"
      : accountKind && identity === accountKind.identity
        ? "account"
        : undefined
    return {
      identity,
      name: k?.name ?? splitKind(identity).name,
      authority: k?.authority,
      package: k?.package,
      role,
      description: k?.description || described[identity],
    }
  })
  return rows.sort((a, b) => a.name.localeCompare(b.name))
}

/** The records a bundle ships beside its kinds, one row each, for the Records
 * table: its functions, agents and mappings — declarations, which are records of
 * the core meta-kinds — and the data rows the install writes after them (an
 * extension's triggers, the llm example's provider rows). Read from the shipped
 * catalog closure, so a bundle with no catalog entry yields an empty list rather
 * than a guess.
 *
 * `kind` is the record's own kind, always a FULL identity, which is what the
 * table shows and what addresses the record. A declaration's id is its kind
 * reference; a data record's is a plain id, unique only within its kind. */
export interface ShippedRecordRow {
  kind: string
  id: string
  name: string
}

const CORE_DECLARATION_KINDS = {
  function: `${CORE_PACKAGE}/function`,
  agent: `${CORE_PACKAGE}/agent`,
  mapping: `${CORE_PACKAGE}/recordmapping`,
} as const

export function bundleRecordRows(catalog?: CatalogItem): ShippedRecordRow[] {
  if (!catalog) return []
  const c = catalog.closure
  const rows: ShippedRecordRow[] = []
  const push = (kind: string, ids: string[] | null) => {
    for (const id of ids ?? []) {
      rows.push({ kind, id, name: splitKind(id).name })
    }
  }
  push(CORE_DECLARATION_KINDS.function, c.functions)
  push(CORE_DECLARATION_KINDS.agent, c.agents)
  push(CORE_DECLARATION_KINDS.mapping, c.mappings)
  // The data rows carry their kind on the wire: a bundle may ship a record of
  // ANY kind, so nothing here enumerates them.
  for (const r of c.records ?? []) {
    rows.push({ kind: r.kind, id: r.id, name: r.id })
  }
  return rows
}

/** Whether the bundle actually declares the provider interfaces that earn
 * the OAuth/callback/connect copy on its detail page. True when it ships an
 * `accountconfig` account kind in its owned package, or one of its declared
 * inputs resolves records of an `oauth2`-trait kind (the OAuth client input).
 * Derived from the bundle's own declared traits, never from names or package
 * suffixes. */
export function declaresProviderInterfaces(
  bundle: Pick<BundleStatus, "id" | "inputs">,
  kinds: KindInfo[]
): boolean {
  if (accountKindOf(kinds, bundle.id)) return true
  return Boolean(oauthClientInput(bundle, kinds))
}

/** The OAuth client input: the declared input whose kind implements the core
 * `oauth2` trait (clientId + clientSecret). The status does not name it, so it
 * is read the way the loader validated it, off the input kinds' traits. */
export function oauthClientInput(
  bundle: Pick<BundleStatus, "inputs">,
  kinds: KindInfo[]
): InputStatus | undefined {
  return bundle.inputs?.find((input) => {
    const k = kindByIdentity(kinds, input.kind)
    return Boolean(k && hasTrait(k, "oauth2"))
  })
}

/** The setup codes that are an input's own resolution problems; the rest
 * (oauth-client, provider, setting) stand on their own. */
const INPUT_SETUP_CODES = ["missing", "ambiguous", "dangling"] as const

export function isInputSetupCode(code: SetupItem["code"]): boolean {
  return (INPUT_SETUP_CODES as readonly string[]).includes(code)
}

/** A setup item a settings FORM clears rather than a warning row: a required
 * `setting` or `secret` record with no value (decision record 0076). The form
 * marks the field itself, so the row beside it would say the same thing
 * twice. */
export function isSettingSetupCode(code: SetupItem["code"]): boolean {
  return code === "setting"
}

/** The Settings row's number in the sidebar: every empty required setting
 * across the bundles this repository holds, counted off the same status read
 * the Registry page makes. Nothing to fill in renders nothing. */
export function settingSetupCount(
  statuses: Pick<BundleStatus, "setup">[]
): number {
  return statuses.reduce(
    (total, b) =>
      total + (b.setup ?? []).filter((i) => isSettingSetupCode(i.code)).length,
    0
  )
}

/** Whether the connect flow should be gated on setup: the server refuses
 * `oauth/start` while the client input is unresolved or its record is missing
 * clientId/clientSecret, so the console refuses first. Only the CLIENT input's
 * problems block connecting; an unrelated input's step does not. */
export function oauthConnectBlocked(
  bundle: Pick<BundleStatus, "inputs" | "setup">,
  kinds: KindInfo[]
): boolean {
  const setup = bundle.setup ?? []
  if (setup.some((item) => item.code === "oauth-client")) return true
  const client = oauthClientInput(bundle, kinds)
  if (!client) return false
  return setup.some(
    (item) => item.input === client.name && isInputSetupCode(item.code)
  )
}
