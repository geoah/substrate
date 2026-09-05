/** The Registry page's pure fold: it merges the two reads the page makes —
 * the imported bundles' runtime status and the shipped catalog closures — into
 * one id-keyed row set, plus the small domain helpers the registry list and the
 * bundle detail share (the two tier sections and the provider-copy gate).
 * Kept out of the page component modules so the pages stay component-only
 * (react-refresh) and this stays unit-testable.
 *
 * Vocabulary: the surface listing them is the REGISTRY and `bundle` stays the
 * schema/API term. The catalog has two TIERS (decision record 0048): a
 * PROVIDER is INSTALLED under the authority that publishes it, a SAMPLE is
 * IMPORTED under this repository's own authority and is the repository's to
 * edit afterwards. The tier is the backend `tier` field, never derived from an
 * authority's shape here. A bundle owns exactly one PACKAGE (decision 0047),
 * and its id IS that package identity. */

import type { BundleStatus, InputStatus, SetupItem } from "@/lib/api/bundles"
import { landedCatalog, landedId, type CatalogItem } from "@/lib/api/catalog"
import { CORE_PACKAGE } from "@/lib/api/http"
import type { BundleUpgrade, CatalogTier, KindInfo } from "@/lib/api/types"
import { kindByIdentity, kindPackage, splitKind } from "@/lib/definition"

/** One bundle row: the installed status (when the lifecycle knows it) and the
 * catalog entry (when it is a shipped closure) folded by the id the bundle
 * has HERE. `tier` comes from the catalog (backend field). */
export interface BundleRow {
  /** The bundle's id IN THIS REPOSITORY: the package identity it owns once
   * it lands. A provider keeps its published id; an imported sample carries
   * this repository's authority, so this is not always the catalog's id. The
   * two doors are addressed by `catalog.id`. */
  id: string
  name: string
  authority: string
  /** The owned package's own word. */
  package: string
  status?: BundleStatus
  catalog?: CatalogItem
  installed: boolean
  /** Which door this row takes. Absent on a bundle applied outside the
   * shipped catalog: the tier is the catalog's to state, and guessing one
   * from an authority's shape is the derivation the backend refuses to make. */
  tier?: CatalogTier
  /** The packages this closure declares against, named as they will be HERE:
   * a sample's are rehomed onto this repository's authority, because that is
   * what the server will look for. Admission refuses while one is missing
   * (catalog.Bundle.requires). */
  requires: string[]
  /** The upgrade preview, present only when the shipped closure moved past
   * what this repository stores (server-computed, catalog read). A sample is
   * never offered one. */
  upgrade?: BundleUpgrade
}

/** Fold the two reads into one row set, keyed by the id each bundle has here:
 * bundles this repository holds carry their runtime status, closures it has
 * not taken yet carry their catalog entry, and one in both carries both
 * (status wins the count columns). `home` is this repository's own authority,
 * which is where a sample lands. Without it an imported sample would never
 * meet its catalog entry. */
export function mergeBundles(
  statuses: BundleStatus[],
  catalog: CatalogItem[],
  home = ""
): BundleRow[] {
  const byId = new Map<string, BundleRow>()
  // A sample can be here under EITHER id: the import lands the rehomed one,
  // and installing it verbatim (still a door until the providers stop
  // requiring sample packages) lands the shipped one. The row takes whichever
  // this repository actually holds, so a verbatim install folds onto its own
  // catalog entry instead of showing up twice.
  const held = new Set(statuses.map((s) => s.id))
  for (const raw of catalog) {
    const landed = landedId(raw, home)
    const id = !held.has(landed) && held.has(raw.id) ? raw.id : landed
    // The entry is REHOMED only when the row is: a closure the repository
    // holds verbatim has its kinds under the authority the tree spells, and
    // previewing them rehomed would link nowhere.
    const item = id === raw.id ? raw : landedCatalog(raw, home)
    byId.set(id, {
      id,
      name: item.name,
      authority: item.authority,
      package: item.package,
      catalog: item,
      installed: item.installed,
      tier: item.tier,
      requires: item.requires ?? [],
      upgrade: item.upgrade,
    })
  }
  for (const status of statuses) {
    const existing = byId.get(status.id)
    byId.set(status.id, {
      id: status.id,
      name: status.name,
      authority: status.authority,
      package: status.package,
      status,
      catalog: existing?.catalog,
      installed: status.installed,
      tier: existing?.tier,
      requires: existing?.requires ?? [],
      upgrade: existing?.upgrade,
    })
  }
  // Not taken first (they invite an action), then held; alpha in each.
  return [...byId.values()].sort(
    (a, b) =>
      Number(a.installed) - Number(b.installed) || a.id.localeCompare(b.id)
  )
}

/** The Registry's sections: the published packages, the copyable ones, and
 * anything applied outside the shipped catalog, which has no tier to sit
 * under and so is listed on its own rather than guessed into one. */
export interface BundleSections {
  providers: BundleRow[]
  samples: BundleRow[]
  applied: BundleRow[]
}

export function bundleSections(rows: BundleRow[]): BundleSections {
  return {
    providers: rows.filter((r) => r.tier === "provider"),
    samples: rows.filter((r) => r.tier === "sample"),
    applied: rows.filter((r) => !r.tier),
  }
}

// ── upgrades: the shipped closure moved past the stored one ─────────────────

/** Whether a row has an upgrade to offer or to explain: the server attaches
 * the preview only to an installed bundle whose closure moved, so presence is
 * the signal. A blocked upgrade still counts (it needs the reader's hand). */
export function upgradeAvailable(row: Pick<BundleRow, "upgrade">): boolean {
  return Boolean(row.upgrade?.available)
}

/** A blocked upgrade: the server's refuse-breakage guards would refuse the
 * re-import, so the console shows the guard lines and no button. */
export function upgradeBlocked(row: Pick<BundleRow, "upgrade">): boolean {
  return Boolean(row.upgrade?.available && row.upgrade.blockers?.length)
}

/** The sidebar badge's number: installed bundles whose shipped closure moved,
 * computed straight off the catalog read so the sidebar needs no second
 * endpoint. */
export function upgradableBundleCount(catalog: CatalogItem[]): number {
  return catalog.filter((item) => item.installed && item.upgrade?.available)
    .length
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
 * resolved against what this repository already holds. */
export interface Requirement {
  /** The required package identity, exactly as the closure names it. */
  package: string
  /** This repository already has it, so admission will not refuse for it. */
  present: boolean
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

/** One row's requirements, each marked present or missing. */
export function requirementsOf(
  row: Pick<BundleRow, "requires">,
  present: ReadonlySet<string>
): Requirement[] {
  return row.requires.map((identity) => ({
    package: identity,
    present: present.has(identity),
  }))
}

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

/** The refusal stated BEFORE the server states it: what to import first, named
 * the way the server names it (packages, which are also the bundle ids). Empty
 * string when nothing is missing — the caller shows no hint at all. */
export function requiresHint(missing: Requirement[]): string {
  if (!missing.length) return ""
  const names = andList(missing.map((r) => r.package))
  return missing.length === 1
    ? `Import ${names} first — this bundle declares against it.`
    : `Import ${names} first — this bundle declares against them.`
}

/** The server's OWN words for a refused import. Admission answers with a
 * validation envelope whose `problems` name exactly what to import first; the
 * envelope's `message` is only the sentinel wrapping them. Show the problems
 * verbatim when there are any, and fall back to the message otherwise, so a
 * race (a requirement torn down between the read and the click) reads as the
 * refusal it is rather than a generic failure. */
export function importFailureText(error: unknown): string {
  const problems = (error as { problems?: unknown } | undefined)?.problems
  if (Array.isArray(problems)) {
    const lines = problems.filter((p): p is string => typeof p === "string")
    if (lines.length) return lines.join(" ")
  }
  const message = (error as { message?: unknown } | undefined)?.message
  return typeof message === "string" && message
    ? message
    : "The import was refused."
}

/** A kind carries a trait when its reconciled declaration lists it. */
export function hasTrait(kind: KindInfo, trait: string): boolean {
  const traits = (kind.definition as { traits?: unknown } | undefined)?.traits
  return Array.isArray(traits) && traits.includes(trait)
}

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
export function inputKindsOf(
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
  const push = (kind: string, ids?: string[]) => {
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
 * (oauth-client, provider) stand on their own as warning rows. */
export const INPUT_SETUP_CODES = ["missing", "ambiguous", "dangling"] as const

export function isInputSetupCode(code: SetupItem["code"]): boolean {
  return (INPUT_SETUP_CODES as readonly string[]).includes(code)
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
