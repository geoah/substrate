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
import { kindHasTrait, SYNC_OWNER_HANDS, SYNC_TRAIT_IDENTITY } from "@/lib/sync"

export interface AccountFormGroups {
  /** The bool properties: what to sync. */
  toggles: FormField[]
  /** Everything else the owner sets. */
  settings: FormField[]
  /** Both, in declaration order: what the write is built from. */
  all: FormField[]
}

export function accountFormGroups(kind: KindInfo): AccountFormGroups {
  const syncable = kindHasTrait(kind, SYNC_TRAIT_IDENTITY)
  const all = buildFormFields(kind).filter(
    (f) => !(syncable && SYNC_OWNER_HANDS.includes(f.name))
  )
  return {
    all,
    toggles: all.filter((f) => f.control === "bool"),
    settings: all.filter((f) => f.control !== "bool"),
  }
}
