/** The GRANT preconditions an agent declaration must satisfy, read off the
 * document before the substrate refuses it.
 *
 * An agent's grants live under ONE `permissions` object: what it may read
 * (`permissions.reads`) and what it may write (`permissions.writes`), the same
 * grouping a function's five take. Three of the host functions are gated by
 * one of them, and the loader makes each a LOAD error rather than a dispatch
 * surprise (`internal/vocabulary/agent.go`, the switch over `t.Builtin`):
 * `query` reads within `permissions.reads` and needs it, `propose` writes one
 * kind and needs it in `permissions.writes`, `write` writes whatever the agent
 * may write and needs a non-empty `permissions.writes`. `ask` needs none: it
 * writes nothing but a question.
 *
 * The loader remains the enforcement. This is the same question asked early, so
 * the editor can say what is missing while the answer is still one control
 * away, and it is pure over the parsed document, so the form lens and the
 * problems panel read one answer. */

import { readReference } from "@/lib/api/types"

/** The gated `runtime: host` function records, by identity. The source of
 * truth is `kinds/substrate.reamde.dev/core/hostfunctions.yaml`; a tool entry
 * names one of them under `function:` exactly as it names any other function. */
export const HOST_FUNCTION_QUERY = "substrate.reamde.dev/core/query"
export const HOST_FUNCTION_WRITE = "substrate.reamde.dev/core/write"
export const HOST_FUNCTION_PROPOSE = "substrate.reamde.dev/core/propose"

/** The request kind `propose` lands, and the one an agent's write permission
 * must name before it may call the tool (`vocabulary.KindRecordPatchRequest`). */
export const RECORD_PATCH_REQUEST_KIND =
  "substrate.reamde.dev/core/recordpatchrequest"

/** The field one `tools:` entry names its function under. Not `callable`: an
 * entry admits only a function (a sub-agent is named on `subagents:`), and
 * `callable` is the trigger's word, where a target really may be either. */
export const TOOL_FUNCTION_FIELD = "function"

/** The one object an agent's grants live under, and the two an agent carries.
 * Spelled here so the paths this reads and the paths the messages NAME cannot
 * drift apart. */
export const PERMISSIONS_PROPERTY = "permissions"
export const READS_GRANT = `${PERMISSIONS_PROPERTY}.reads`
export const WRITES_GRANT = `${PERMISSIONS_PROPERTY}.writes`

/** The kind whose documents these hints are about, named where the rest of the
 * declaration kinds are. */
export { AGENT_KIND } from "@/lib/declarations"

/** One unmet precondition: which host function asked for it, which grant
 * answers it, and the sentence to render. */
export interface GrantHint {
  /** The host function identity whose grant is unmet, spelled as a tool entry
   * spells it. */
  function: string
  /** The dotted path of the grant that satisfies it, as the document spells
   * it (`permissions.reads`, `permissions.writes`). */
  property: string
  message: string
}

/** The kind a grant's entries point at. Both grants name KINDS: which ones may
 * be written, and which ones may be read. */
const KIND_KIND = "substrate.reamde.dev/core/kind"

/** The record ONE grant entry names, whichever way it is spelled.
 *
 * A grant's entries are pointers, so each is the flat path
 * `substrate.reamde.dev/core/kind/<identity>`; the pin supplies the prefix, so
 * an entry the author typed short arrives short and the server canonicalizes
 * it. BOTH spellings name one kind, and this is the only place that knows it. */
export function valueIdentity(value: unknown): string | undefined {
  // A reference is served as `{ref: …}` and authored as the bare path, so
  // both arrive here; `readReference` is the one place that knows the pair.
  const named = readReference(value)?.path.trim()
  if (!named) return undefined
  const prefix = `${KIND_KIND}/`
  return named.startsWith(prefix) ? named.slice(prefix.length) : named
}

/** Every kind a repeated grant names, in order. */
function identitiesOf(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  const out: string[] = []
  for (const item of value) {
    const named = valueIdentity(item)
    if (named) out.push(named)
  }
  return out
}

/** The host functions an agent document's `tools:` names, in the order the
 * document lists them, deduplicated. A tool entry is `{function, name?,
 * description?}`; anything else in the list is another authority's function
 * and carries no host grant. */
export function hostToolsOf(properties: Record<string, unknown>): string[] {
  const known = [
    HOST_FUNCTION_QUERY,
    HOST_FUNCTION_WRITE,
    HOST_FUNCTION_PROPOSE,
  ]
  const out: string[] = []
  const tools = Array.isArray(properties.tools) ? properties.tools : []
  for (const entry of tools) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) continue
    const held = readReference(
      (entry as Record<string, unknown>)[TOOL_FUNCTION_FIELD]
    )?.path
    if (!held) continue
    // The entry is a POINTER at a function, so it is the flat path
    // `substrate.reamde.dev/core/function/<identity>`; a short form the
    // server has not canonicalized yet names the same function.
    const prefix = "substrate.reamde.dev/core/function/"
    const named = held.startsWith(prefix) ? held.slice(prefix.length) : held
    if (!known.includes(named) || out.includes(named)) continue
    out.push(named)
  }
  return out
}

/** The `permissions` object an agent's grants live under, or an empty one. */
function permissionsOf(
  properties: Record<string, unknown>
): Record<string, unknown> {
  const held = properties[PERMISSIONS_PROPERTY]
  return held && typeof held === "object" && !Array.isArray(held)
    ? (held as Record<string, unknown>)
    : {}
}

/** Whether `permissions.reads` says anything the loader would accept: its
 * `kinds` is required and non-empty wherever the grant appears at all
 * (`vocabulary.parseReads`), so an empty allowlist is no grant. */
function readsGranted(properties: Record<string, unknown>): boolean {
  const reads = permissionsOf(properties).reads
  if (!reads || typeof reads !== "object" || Array.isArray(reads)) return false
  return identitiesOf((reads as Record<string, unknown>).kinds).length > 0
}

/** Every unmet grant precondition in an agent document's properties. An empty
 * list means the declaration's tools are all paid for. */
export function grantHints(
  properties: Record<string, unknown> | undefined
): GrantHint[] {
  if (!properties) return []
  const writes = identitiesOf(permissionsOf(properties).writes)
  const hints: GrantHint[] = []
  for (const named of hostToolsOf(properties)) {
    switch (named) {
      case HOST_FUNCTION_QUERY:
        if (!readsGranted(properties)) {
          hints.push({
            function: named,
            property: READS_GRANT,
            message: `query reads within the read grant, so data.${READS_GRANT} must name at least one kind.`,
          })
        }
        break
      case HOST_FUNCTION_PROPOSE:
        if (!writes.includes(RECORD_PATCH_REQUEST_KIND)) {
          hints.push({
            function: named,
            property: WRITES_GRANT,
            message: `propose writes a change request, so data.${WRITES_GRANT} must name ${RECORD_PATCH_REQUEST_KIND}.`,
          })
        }
        break
      case HOST_FUNCTION_WRITE:
        if (writes.length === 0) {
          hints.push({
            function: named,
            property: WRITES_GRANT,
            message: `write writes records, so data.${WRITES_GRANT} must name the kinds this agent may create or change.`,
          })
        }
        break
    }
  }
  return hints
}

// ── editing the grants ──────────────────────────────────────────────────────

/** The two grants the console edits: what an agent may see and change. */
export type GrantSide = "reads" | "writes"

/** The kinds (identities or globs) one grant names, in order. On `writes`
 * the change-request kind is left out: it is `propose`'s own grant, not a
 * collection the person picks, and an edit carries it through untouched. */
export function grantKindsOf(
  properties: Record<string, unknown>,
  side: GrantSide
): string[] {
  const permissions = permissionsOf(properties)
  if (side === "writes") {
    return identitiesOf(permissions.writes).filter(
      (k) => k !== RECORD_PATCH_REQUEST_KIND
    )
  }
  const reads = permissions.reads
  if (!reads || typeof reads !== "object" || Array.isArray(reads)) return []
  return identitiesOf((reads as Record<string, unknown>).kinds)
}

/** One grant entry as the write path takes it: the kind record's flat path,
 * which the pin would complete from the identity anyway. */
function grantEntry(identity: string): string {
  return `${KIND_KIND}/${identity}`
}

/** The whole `permissions` object after one grant is set to `kinds`: the
 * other grant, read budgets and anything else it holds ride along. An empty
 * read grant is no grant (its `kinds` is required where it appears), so it
 * leaves `reads` out; the change-request kind stays on `writes` wherever it
 * was. */
export function permissionsWith(
  properties: Record<string, unknown>,
  side: GrantSide,
  kinds: string[]
): Record<string, unknown> {
  const permissions = { ...permissionsOf(properties) }
  if (side === "reads") {
    const held = permissions.reads
    const reads =
      held && typeof held === "object" && !Array.isArray(held)
        ? (held as Record<string, unknown>)
        : {}
    if (kinds.length === 0) delete permissions.reads
    else permissions.reads = { ...reads, kinds: kinds.map(grantEntry) }
    return permissions
  }
  const proposes = identitiesOf(permissions.writes).includes(
    RECORD_PATCH_REQUEST_KIND
  )
  const writes = [
    ...kinds.filter((k) => k !== RECORD_PATCH_REQUEST_KIND),
    ...(proposes ? [RECORD_PATCH_REQUEST_KIND] : []),
  ]
  if (writes.length === 0) delete permissions.writes
  else permissions.writes = writes.map(grantEntry)
  return permissions
}

/** Why a grant edit would break a tool the agent holds, in everyday words,
 * or undefined when it would not: the loader refuses an agent whose tools'
 * grants are unmet, so the console holds the edit back instead. */
export function grantEditProblem(
  properties: Record<string, unknown>,
  side: GrantSide,
  kinds: string[]
): string | undefined {
  const before = new Set(grantHints(properties).map((h) => h.function))
  const after = grantHints({
    ...properties,
    [PERMISSIONS_PROPERTY]: permissionsWith(properties, side, kinds),
  }).filter((h) => !before.has(h.function))
  if (after.some((h) => h.function === HOST_FUNCTION_QUERY)) {
    return "It looks things up, so it needs to see at least one collection."
  }
  if (after.some((h) => h.function === HOST_FUNCTION_WRITE)) {
    return "It makes changes, so it needs to be able to change at least one collection."
  }
  return undefined
}
