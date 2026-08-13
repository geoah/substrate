/** The Registry page's pure fold: it merges the two reads the page makes —
 * the imported bundles' runtime status and the shipped catalog closures — into
 * one id-keyed row set, plus the small domain helpers the registry list and the
 * bundle detail share (the Integrations facet filter and the provider-copy
 * gate). Kept out of the page component modules so the pages stay component-only
 * (react-refresh) and this stays unit-testable.
 *
 * Vocabulary: the surface listing them is the REGISTRY, the unit is an
 * EXTENSION, and adding one is an IMPORT (owner ruling); `bundle` stays the
 * schema/API term and `installed` stays the wire field the status carries. An
 * bundle that connects an external provider is an INTEGRATION — a catalog
 * FACET carried by the backend `integration` flag, never derived from OAuth or
 * account shape here. A VOCABULARY bundle is the other catalog facet
 * (backend `vocabulary`): a bare authority shipping kinds and nothing else,
 * which a fresh repository must import before anything can map onto it. */

import type { BundleStatus, InputStatus, SetupItem } from "@/lib/api/bundles"
import type { CatalogItem } from "@/lib/api/catalog"
import type { BundleUpgrade, KindInfo } from "@/lib/api/types"
import { kindByIdentity, splitKind } from "@/lib/definition"

/** One bundle row: the installed status (when the lifecycle knows it) and
 * the catalog entry (when it is a shipped closure) folded by bundle id. The
 * `integration` facet comes from the catalog metadata (backend flag). */
export interface BundleRow {
  id: string
  name: string
  authority: string
  status?: BundleStatus
  catalog?: CatalogItem
  installed: boolean
  /** Catalog facet: this bundle connects an external provider. */
  integration: boolean
  /** Catalog facet: a worked example — installable, readable, safe to run on
   * a fresh repository. */
  example: boolean
  /** Catalog facet: a pure-vocabulary bundle — kinds and nothing else. */
  vocabulary: boolean
  /** The authorities this closure declares against; the server refuses the
   * import while one of them is missing (catalog.Bundle.requires). */
  requires: string[]
  /** The upgrade preview, present only when the shipped closure moved past
   * what this repository stores (server-computed, catalog read). */
  upgrade?: BundleUpgrade
}

/** Fold the two reads into one id-keyed row set: imported bundles carry their
 * runtime status, not-yet-imported closures carry their catalog entry, and a
 * bundle in both carries both (status wins the count columns). */
export function mergeBundles(
  statuses: BundleStatus[],
  catalog: CatalogItem[]
): BundleRow[] {
  const byId = new Map<string, BundleRow>()
  for (const item of catalog) {
    byId.set(item.id, {
      id: item.id,
      name: item.name,
      authority: item.authority,
      catalog: item,
      installed: item.installed,
      integration: Boolean(item.integration),
      example: Boolean(item.example),
      vocabulary: Boolean(item.vocabulary),
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
      status,
      catalog: existing?.catalog,
      installed: status.installed,
      integration: existing?.integration ?? false,
      example: existing?.example ?? false,
      // The closure facets are the CATALOG's to state; a bundle applied
      // outside the shipped registry carries neither, and guessing one from
      // its authority's shape would be the derivation the backend refuses to
      // make.
      vocabulary: existing?.vocabulary ?? false,
      requires: existing?.requires ?? [],
      upgrade: existing?.upgrade,
    })
  }
  // Not-imported first (they invite an action), then imported; alpha in each.
  return [...byId.values()].sort(
    (a, b) =>
      Number(a.installed) - Number(b.installed) || a.id.localeCompare(b.id)
  )
}

/** The catalog-list facet, orthogonal to whether a row is imported: `all` shows
 * every bundle, `vocabulary` narrows to the pure-vocabulary bundles (what a
 * fresh repository imports first), `integrations` to provider integrations,
 * `examples` to the worked demonstrations — which is where a substrate with no
 * llmprovider row of its own is pointed — and `upgrades` to the imported
 * bundles whose shipped closure moved past what is stored (the sidebar badge's
 * set). */
export type BundleFacet =
  | "all"
  | "vocabulary"
  | "integrations"
  | "examples"
  | "upgrades"

export function filterBundles(
  rows: BundleRow[],
  facet: BundleFacet
): BundleRow[] {
  switch (facet) {
    case "integrations":
      return rows.filter((r) => r.integration)
    case "examples":
      return rows.filter((r) => r.example)
    case "vocabulary":
      return rows.filter((r) => r.vocabulary)
    case "upgrades":
      return rows.filter((r) => upgradeAvailable(r))
    default:
      return rows
  }
}

// ── upgrades: the shipped closure moved past the stored one ─────────────────

/** Whether a row has an upgrade to offer or to explain: the server attaches
 * the preview only to an installed bundle whose closure moved, so presence is
 * the signal. A blocked upgrade still counts (it needs the reader's hand). */
export function upgradeAvailable(
  row: Pick<BundleRow, "upgrade">
): boolean {
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

/** "v1alpha1 → v1alpha2", or just the target when the store held no version. */
export function upgradeMotion(upgrade: BundleUpgrade): string {
  if (upgrade.from && upgrade.to) return `${upgrade.from} → ${upgrade.to}`
  return upgrade.to ?? ""
}

// ── requirements: what must be imported first ───────────────────────────────

/** One entry of a closure's `requires:` — an AUTHORITY it declares against —
 * resolved against what this repository already holds. */
export interface Requirement {
  /** The required authority, exactly as the closure names it. */
  authority: string
  /** This repository already has it, so admission will not refuse for it. */
  present: boolean
}

/** The authorities this repository HOLDS, from the two reads the registry
 * page already makes: every imported bundle's owned authority, and every
 * authority the kind registry has reconciled (which covers core and anything
 * applied outside the shipped catalog). This is the console's read of the
 * check `schema.resolveBundle` runs server-side — a bundle whose status says
 * `installed: false` (uninstalled or quarantined) is NOT in the live registry,
 * so its authority does not count. */
export function presentAuthorities(
  rows: BundleRow[],
  kinds: KindInfo[] = []
): Set<string> {
  const out = new Set<string>()
  for (const row of rows) if (row.installed) out.add(row.authority)
  for (const kind of kinds) if (kind.authority) out.add(kind.authority)
  return out
}

/** One row's requirements, each marked present or missing. */
export function requirementsOf(
  row: Pick<BundleRow, "requires">,
  present: ReadonlySet<string>
): Requirement[] {
  return row.requires.map((authority) => ({
    authority,
    present: present.has(authority),
  }))
}

export function missingRequirements(
  requirements: Requirement[]
): Requirement[] {
  return requirements.filter((r) => !r.present)
}

/** "people.substrate.reamde.dev", "people.substrate.reamde.dev and tasks.substrate.reamde.dev", "a, b and c". */
function andList(names: string[]): string {
  if (names.length <= 1) return names[0] ?? ""
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`
}

/** The refusal stated BEFORE the server states it: what to import first, named
 * the way the server names it (authorities, not bundle ids). Empty string when
 * nothing is missing — the caller shows no hint at all. */
export function requiresHint(missing: Requirement[]): string {
  if (!missing.length) return ""
  const names = andList(missing.map((r) => r.authority))
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

/** The bundle's account-config kind: the kind in its owned authority that
 * implements the `accountconfig` trait (the host writes tokens onto its
 * records). Its presence is the signal that the bundle has provider
 * accounts to connect. */
export function accountKindOf(
  kinds: KindInfo[],
  bundleAuthority: string
): KindInfo | undefined {
  return kinds.find(
    (k) => k.authority === bundleAuthority && hasTrait(k, "accountconfig")
  )
}

// ── bundle-detail closure inventory (the Kinds + Resources tables) ────────

/** One row of the bundle's Kinds table: an kind its closure
 * installed, resolved against the registry for its collection route, plus its
 * role when the host treats it specially (a kind some declared input resolves
 * records of, the `account` kind the connect flow writes tokens onto).
 * `authority`/`plural` are absent only when the registry has not (yet)
 * reconciled the kind. */
export interface KindRow {
  identity: string
  name: string
  authority?: string
  plural?: string
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
 * table. Identities come from the shipped closure's catalog resources when the
 * bundle is a catalog entry; otherwise from the registry itself — every
 * reconciled kind in the bundle's owned authority. Sorted by display name. */
export function installedKindRows(
  bundle: Pick<BundleStatus, "authority" | "inputs">,
  kinds: KindInfo[],
  catalog?: CatalogItem
): KindRow[] {
  const fromCatalog = catalog?.resources.kinds ?? []
  const identities =
    fromCatalog.length > 0
      ? fromCatalog
      : kinds
          .filter((k) => k.authority === bundle.authority)
          .map((k) => k.identity)
  const accountKind = accountKindOf(kinds, bundle.authority)
  const inputKinds = inputKindsOf(bundle.inputs, catalog)
  // A bundle the repository has NOT imported has no registry entry for any of
  // its kinds, so the closure's own descriptions are the only ones there are.
  const described = catalog?.resources.kindDescriptions ?? {}
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
      plural: k?.plural,
      role,
      description: k?.description || described[identity],
    }
  })
  return rows.sort((a, b) => a.name.localeCompare(b.name))
}

/** The non-kind members of an bundle's closure — functions, agents,
 * triggers — for the Resources table. Read from the shipped catalog closure, so
 * a bundle with no catalog entry yields an empty list rather than a guess. */
export type ResourceKind = "function" | "agent" | "trigger" | "mapping"

export interface ResourceRow {
  kind: ResourceKind
  identity: string
  name: string
}

export function bundleResourceRows(catalog?: CatalogItem): ResourceRow[] {
  if (!catalog) return []
  const r = catalog.resources
  const rows: ResourceRow[] = []
  const push = (kind: ResourceKind, ids?: string[]) => {
    for (const identity of ids ?? []) {
      rows.push({ kind, identity, name: splitKind(identity).name })
    }
  }
  push("function", r.functions)
  push("agent", r.agents)
  push("trigger", r.triggers)
  push("mapping", r.mappings)
  return rows
}

/** Whether the bundle actually declares the provider interfaces that earn
 * the OAuth/callback/connect copy on its detail page. True when it ships an
 * `accountconfig` account kind in its owned authority, or one of its declared
 * inputs resolves records of an `oauth2`-trait kind (the OAuth client input).
 * Derived from the bundle's own declared traits, never from names or authority
 * suffixes. */
export function declaresProviderInterfaces(
  bundle: Pick<BundleStatus, "authority" | "inputs">,
  kinds: KindInfo[]
): boolean {
  if (accountKindOf(kinds, bundle.authority)) return true
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
