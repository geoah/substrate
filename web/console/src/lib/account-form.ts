/** What the Add account dialog asks, off the account kind's declaration and
 * nothing else. The fields are record-form's owner-writable set, so the OAuth
 * facility's hands (`writer: oauth`) and the connector's cursors, resume
 * state and identity references (`writer: connector`) never appear, because
 * the declaration marks them; the one subtraction made here is the `sync`
 * trait's two owner hands, whose controls are the page's Sync now and Pause
 * buttons. The result is split into the TOGGLES, each bool a thing to sync
 * and, on an OAuth provider, a scope the consent asks for, and the SETTINGS
 * (cadence, depth, a name, a filter), so the dialog can give each group a
 * heading a first-time user reads. */

import type { KindInfo } from "@/lib/api/types"
import { buildFormFields, type FormField } from "@/lib/record-form"
import { SYNC_TRAIT } from "@/lib/core-kinds"
import { ownerWritable, propSpecs } from "@/lib/record-schema"
import { kindHasTrait, SYNC_OWNER_HANDS } from "@/lib/sync"
import type { UnitReader } from "@/lib/unit-reader"

export interface AccountFormGroups {
  /** The bool properties: what to sync. */
  toggles: FormField[]
  /** Everything else the owner sets. */
  settings: FormField[]
  /** Both, in declaration order: what the write is built from. */
  all: FormField[]
}

/** `buttons`: properties a control of the page writes instead of the form,
 * such as a record view's request button (`requestHands`). */
export function accountFormGroups(
  units: UnitReader,
  kind: KindInfo,
  buttons: readonly string[] = []
): AccountFormGroups {
  const syncable = kindHasTrait(kind, SYNC_TRAIT)
  const all = buildFormFields(units, kind).filter(
    (f) =>
      !(syncable && SYNC_OWNER_HANDS.includes(f.name)) &&
      !buttons.includes(f.name)
  )
  return {
    all,
    toggles: all.filter((f) => f.control === "bool"),
    settings: all.filter((f) => f.control !== "bool"),
  }
}

/** Whether the owner can create an account of this kind: not where the
 * kind requires a property only another hand writes and declares no
 * default for it, such as the login id a connector reports. The write path
 * refuses such a create from the owner either way (the requirement, or the
 * writer role), so the provider makes those accounts itself. */
export function ownerCreatesAccounts(kind: KindInfo | undefined): boolean {
  if (!kind) return true
  return !propSpecs(kind).some(
    (s) =>
      s.required &&
      !s.managed &&
      !ownerWritable(s) &&
      (s.default === undefined || s.default === null)
  )
}
