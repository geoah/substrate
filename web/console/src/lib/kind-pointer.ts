/** What a stored reference at core's `kind` kind names, read before anything
 * draws it as a record.
 *
 * Such a reference points at a KIND DECLARATION, so its id is a kind
 * reference (`substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/tasks/task`
 * names the `task` kind). In a GRANT, an agent's or a function's
 * `permissions.reads.kinds` and `permissions.writes`, the id may instead be one
 * of the trigger selector's globs: `*`, `<authority>/*` or
 * `<authority>/<package>/*` (`vocabulary.grantEntryProblem`). The engine strips
 * the pin (`vocabulary.ReferentID`), so the stored
 * `substrate.reamde.dev/core/kind/*` is the grant entry `*`, every kind, and
 * never "every record of the kind kind". A glob is no record: `*` is outside
 * the record id alphabet. */

import { splitKind } from "@/lib/api/http"
import { AUTH_KINDS, KIND_KIND } from "@/lib/core-kinds"
import { splitRecordPath } from "@/lib/record-path"
import { listWords } from "@/lib/tools"

/** The record id alphabet, `vocabulary.reID`. An id outside it names no
 * record, so nothing may read a title for it or link to it. */
const RECORD_ID = /^[A-Za-z0-9][A-Za-z0-9._~:@/-]*$/

/** Whether a string can be a record id at all. */
export function isRecordId(id: string): boolean {
  return RECORD_ID.test(id)
}

/** Whether a grant entry is a pattern rather than one kind
 * (`vocabulary.IsTypeGlob`). */
export function isKindGlob(entry: string): boolean {
  return entry === "*" || entry.endsWith("/*")
}

/** What one reference at a kind declaration names: one kind, or a pattern over
 * kinds, spelled as the grant entry (the pin stripped). */
export type KindPointer =
  { shape: "kind"; kind: string } | { shape: "pattern"; pattern: string }

/** Read a stored reference path as a kind pointer, or `undefined` when it does
 * not point at core's `kind` kind (then it is an ordinary record path, or no
 * path at all). */
export function kindPointer(path: string): KindPointer | undefined {
  const target = splitRecordPath(path)
  if (!target || target.kind !== KIND_KIND) return undefined
  if (isKindGlob(target.id)) return { shape: "pattern", pattern: target.id }
  const { authority, pkg, name } = splitKind(target.id)
  if (authority && pkg && name) return { shape: "kind", kind: target.id }
  return undefined
}

/** A glob in plain words, naming the authority or package in full and the
 * auth kinds it leaves out, since a reader deciding what an agent may touch
 * needs both. */
export function kindPatternGrant(pattern: string): string {
  const prefix = pattern === "*" ? "" : pattern.slice(0, -1)
  const left = AUTH_KINDS.filter((k) => k.startsWith(prefix)).map(
    (k) => splitKind(k).name
  )
  const scope =
    pattern === "*"
      ? "Every kind"
      : prefix.slice(0, -1).includes("/")
        ? `Every kind in the package ${prefix.slice(0, -1)}`
        : `Every kind published by ${prefix.slice(0, -1)}`
  if (!left.length) return scope
  return `${scope}, except the ${listWords(left)} kinds`
}
