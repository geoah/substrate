/** An app's grant, resolved against the registry. The record names kinds and
 * traits; the host checks calls by kind IDENTITY, so the traits are expanded
 * to the kinds implementing them here, once per registry change, and the
 * bridge asks the expanded set. A reference that resolves to nothing is
 * reported, never dropped silently: the launcher greys the app and names the
 * package it waits on. */

import type { KindInfo } from "@/lib/api/types"

/** The `permissions` block, references read down to identities (a kind's
 * `<authority>/<package>/<name>`, a function's or an agent's id, which is
 * its identity too). `lib/apps/spec.ts` decodes the record into this. */
export interface Grant {
  reads: { kinds: string[]; traits: string[] }
  writes: string[]
  call: string[]
  agents: string[]
}

export const EMPTY_GRANT: Grant = {
  reads: { kinds: [], traits: [] },
  writes: [],
  call: [],
  agents: [],
}

export interface ExpandedGrant {
  /** Every identity a read is held to: `reads.kinds` plus the implementors
   * of `reads.traits`, each present in the registry. */
  reads: string[]
  /** The same, as declarations, for the host context. */
  readKinds: KindInfo[]
  writes: string[]
  call: string[]
  agents: string[]
  /** Grant entries that resolve to no kind the registry holds, with the
   * path each sits at. */
  unresolved: { path: string; identity: string }[]
}

/** The name a `traits:` entry binds, without its arguments:
 * `temporal(point: dueAt)` → `temporal`. */
export function traitNameOf(entry: string): string {
  const open = entry.indexOf("(")
  return (open < 0 ? entry : entry.slice(0, open)).trim()
}

/** Whether a kind's declaration binds the trait. The wire carries the
 * declaration's own spelling, a bare name that the loader resolves against
 * the core package and the kind's own package
 * (`internal/vocabulary/load.go`, `bindCapability`); a full identity matches
 * exactly. */
export function implementsTrait(kind: KindInfo, trait: string): boolean {
  const def = (kind.definition ?? {}) as Record<string, unknown>
  const traits = def.traits
  if (!Array.isArray(traits)) return false
  const slash = trait.lastIndexOf("/")
  const bare = slash < 0 ? trait : trait.slice(slash + 1)
  const traitPackage = slash < 0 ? "" : trait.slice(0, slash)
  const own = kind.authority ? `${kind.authority}/${kind.package}` : ""
  return traits.some((entry) => {
    if (typeof entry !== "string") return false
    const name = traitNameOf(entry)
    if (name.includes("/")) return name === trait
    if (name !== bare) return false
    return (
      !traitPackage ||
      traitPackage === "substrate.reamde.dev/core" ||
      traitPackage === own
    )
  })
}

export function implementors(kinds: KindInfo[], trait: string): KindInfo[] {
  return kinds.filter((k) => implementsTrait(k, trait))
}

export function expandGrant(
  spec: { permissions: Grant },
  kinds: KindInfo[]
): ExpandedGrant {
  const { permissions } = spec
  const byIdentity = new Map(kinds.map((k) => [k.identity, k]))
  const reads = new Map<string, KindInfo>()
  const unresolved: ExpandedGrant["unresolved"] = []

  permissions.reads.kinds.forEach((identity, i) => {
    const kind = byIdentity.get(identity)
    if (kind) reads.set(identity, kind)
    else unresolved.push({ path: `permissions.reads.kinds[${i}]`, identity })
  })
  permissions.reads.traits.forEach((trait, i) => {
    const found = implementors(kinds, trait)
    if (!found.length) {
      unresolved.push({
        path: `permissions.reads.traits[${i}]`,
        identity: trait,
      })
    }
    for (const kind of found) reads.set(kind.identity, kind)
  })
  permissions.writes.forEach((identity, i) => {
    if (!byIdentity.has(identity)) {
      unresolved.push({ path: `permissions.writes[${i}]`, identity })
    }
  })

  return {
    reads: [...reads.keys()],
    readKinds: [...reads.values()],
    writes: [...permissions.writes],
    call: [...permissions.call],
    agents: [...permissions.agents],
    unresolved,
  }
}
