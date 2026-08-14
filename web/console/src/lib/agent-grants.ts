/** The GRANT preconditions an agent declaration must satisfy, read off the
 * document before the substrate refuses it.
 *
 * An agent's grants live under ONE `permissions` object: what it may read
 * (`permissions.reads`) and what it may write (`permissions.writes`), the same
 * grouping a function's five take. Three of the four host functions are gated
 * by one of them, and the loader makes each a LOAD error rather than a dispatch
 * surprise (`internal/vocabulary/agent.go`, the switch over `t.Builtin`):
 * `query` is capability-scoped and needs `permissions.reads`, `propose` writes
 * one kind and needs it in `permissions.writes`, `mutate` writes whatever the
 * agent may write and needs a non-empty `permissions.writes`. `graphql` needs
 * none: it is read-only and repository-wide, and declaring the tool IS the
 * grant.
 *
 * The loader remains the enforcement. This is the same question asked early, so
 * the editor can say what is missing while the answer is still one control
 * away, and it is pure over the parsed document, so the form lens and the
 * problems panel read one answer. */

/** The four `runtime: host` function records, by identity. The source of truth
 * is `kinds/core.substrate.reamde.dev/hostfunctions.yaml`; a tool entry names
 * one of them under `function:` exactly as it names any other function. */
export const HOST_FUNCTION_QUERY = "core.substrate.reamde.dev/query"
export const HOST_FUNCTION_GRAPHQL = "core.substrate.reamde.dev/graphql"
export const HOST_FUNCTION_MUTATE = "core.substrate.reamde.dev/mutate"
export const HOST_FUNCTION_PROPOSE = "core.substrate.reamde.dev/propose"

/** The request kind `propose` lands, and the one an agent's write permission
 * must name before it may call the tool (`vocabulary.KindRecordPatchRequest`). */
export const RECORD_PATCH_REQUEST_KIND =
  "core.substrate.reamde.dev/recordpatchrequest"

/** The field one `tools:` entry names its function under. Not `callable`: an
 * entry admits only a function (a sub-agent is named on `agents:`), and
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
const KIND_KIND = "core.substrate.reamde.dev/kind"

/** The record ONE grant entry names, whichever way it is spelled.
 *
 * A grant's entries are pointers, so each is the flat path
 * `core.substrate.reamde.dev/kind/<identity>`; the pin supplies the prefix, so
 * an entry the author typed short arrives short and the server canonicalizes
 * it. BOTH spellings name one kind, and this is the only place that knows it. */
export function valueIdentity(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined
  const named = value.trim()
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
    HOST_FUNCTION_GRAPHQL,
    HOST_FUNCTION_MUTATE,
    HOST_FUNCTION_PROPOSE,
  ]
  const out: string[] = []
  const tools = Array.isArray(properties.tools) ? properties.tools : []
  for (const entry of tools) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) continue
    const held = (entry as Record<string, unknown>)[TOOL_FUNCTION_FIELD]
    if (typeof held !== "string") continue
    // The entry is a POINTER at a function, so it is the flat path
    // `core.substrate.reamde.dev/function/<identity>`; a short form the
    // server has not canonicalized yet names the same function.
    const prefix = "core.substrate.reamde.dev/function/"
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
            message: `query is capability-scoped: it needs data.${READS_GRANT} with at least one kind in its allowlist.`,
          })
        }
        break
      case HOST_FUNCTION_PROPOSE:
        if (!writes.includes(RECORD_PATCH_REQUEST_KIND)) {
          hints.push({
            function: named,
            property: WRITES_GRANT,
            message: `propose lands a change request: it needs ${RECORD_PATCH_REQUEST_KIND} in data.${WRITES_GRANT}.`,
          })
        }
        break
      case HOST_FUNCTION_MUTATE:
        if (writes.length === 0) {
          hints.push({
            function: named,
            property: WRITES_GRANT,
            message: `mutate writes records: it needs data.${WRITES_GRANT} to name which kinds this agent may create or change.`,
          })
        }
        break
    }
  }
  return hints
}
