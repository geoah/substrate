/** What the Add account dialog asks, off the account kind's declaration and
 * nothing else. The fields are record-form's owner-writable set, so the OAuth
 * facility's hands (`writer: oauth`) and the connector's cursors, resume
 * state and identity references (`writer: connector`) never appear, because
 * the declaration marks them; the one subtraction made here is the `sync`
 * trait's two owner hands, whose controls are the page's Sync now and Pause
 * buttons. The result is split into the TOGGLES, each bool a thing to sync
 * and, on an OAuth provider, a scope the consent asks for; the PERMISSIONS,
 * the bools whose scopes the bundle marks `access: write`, each a thing the
 * owner's agents may do at the provider (decision record 0255); and the
 * SETTINGS (cadence, depth, a name, a filter), so the dialog can give each
 * group a heading a first-time user reads. */

import type { BundleFeature, KindInfo } from "@/lib/api/types"
import { buildFormFields, type FormField } from "@/lib/record-form"
import { SYNC_TRAIT } from "@/lib/core-kinds"
import { kindHasTrait, SYNC_OWNER_HANDS } from "@/lib/sync"
import type { UnitReader } from "@/lib/unit-reader"

export interface AccountFormGroups {
  /** The bool properties that bring something in: what to sync. */
  toggles: FormField[]
  /** The bool properties that let the owner's agents change things at the
   * provider: what they may do there. */
  permissions: FormField[]
  /** Everything else the owner sets. */
  settings: FormField[]
  /** All three, in declaration order: what the write is built from. */
  all: FormField[]
}

/** `write` names the toggles the provider's bundle marks `access: write`
 * (`writeToggles`); without it every bool is a thing to bring in. */
export function accountFormGroups(
  units: UnitReader,
  kind: KindInfo,
  write: ReadonlySet<string> = new Set()
): AccountFormGroups {
  const syncable = kindHasTrait(kind, SYNC_TRAIT)
  const all = buildFormFields(units, kind).filter(
    (f) => !(syncable && SYNC_OWNER_HANDS.includes(f.name))
  )
  const bools = all.filter((f) => f.control === "bool")
  return {
    all,
    toggles: bools.filter((f) => !write.has(f.name)),
    permissions: bools.filter((f) => write.has(f.name)),
    settings: all.filter((f) => f.control !== "bool"),
  }
}

/** The toggles a provider's bundle marks `access: write`, by name. */
export function writeToggles(
  features: readonly BundleFeature[] | undefined
): Set<string> {
  return new Set(
    (features ?? []).filter((f) => f.access === "write").map((f) => f.toggle)
  )
}

/** The toggles an account has on that its sign-in does not cover: each one
 * true on the record whose scopes are not all in the account's
 * `grantedScopes`, the scopes its last consent asked for. A switch turned on
 * after the account connected is one, as is every switch a later version of
 * the provider added a scope to. */
export function ungrantedToggles(
  properties: Record<string, unknown> | undefined,
  grantedScopes: readonly string[],
  features: readonly BundleFeature[] | undefined
): string[] {
  const granted = new Set(grantedScopes)
  return (features ?? [])
    .filter(
      (f) =>
        properties?.[f.toggle] === true &&
        f.scopes.some((scope) => !granted.has(scope))
    )
    .map((f) => f.toggle)
}
