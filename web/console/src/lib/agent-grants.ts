/** The GRANT preconditions an agent declaration must satisfy, read off the
 * document before the substrate refuses it.
 *
 * An agent's grants live under ONE `permissions` object: what it may read
 * (`permissions.reads`) and what it may write (`permissions.writes`), the same
 * grouping a function's five take. Each of the four host functions is gated by
 * one of them, and the loader makes each a LOAD error rather than a dispatch
 * surprise (`internal/vocabulary/agent.go`, the switch over `t.Builtin`):
 * `query` reads within `permissions.reads` and needs it, `propose` and `ask`
 * each write one kind and need it covered by `permissions.writes`, `write`
 * writes whatever the agent may write and needs a non-empty
 * `permissions.writes`.
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
export const HOST_FUNCTION_ASK = "substrate.reamde.dev/core/ask"

/** The request kind `propose` lands, and the one an agent's write permission
 * must name before it may call the tool (`vocabulary.KindRecordPatchRequest`). */
export const RECORD_PATCH_REQUEST_KIND =
  "substrate.reamde.dev/core/recordpatchrequest"

/** The question kind `ask` lands (`vocabulary.KindLLMInteraction`), and the
 * spelling before record 0077 moved it, which the loader still accepts on a
 * stored grant the boot upgrade has not rewritten yet. */
export const LLM_INTERACTION_KIND = "substrate.reamde.dev/llm/interaction"
const LLM_INTERACTION_KIND_PRE_MOVE = "substrate.reamde.dev/core/llminteraction"

/** The kinds a host tool writes on its own account: the write grant must
 * cover one of each tool's, or the loader refuses the agent. These are the
 * tool's grant, not a collection the person picks, so the editor neither
 * lists nor offers them and carries them through every edit. */
const TOOL_OWN_KINDS: ReadonlyArray<{ tool: string; kinds: string[] }> = [
  { tool: HOST_FUNCTION_PROPOSE, kinds: [RECORD_PATCH_REQUEST_KIND] },
  {
    tool: HOST_FUNCTION_ASK,
    kinds: [LLM_INTERACTION_KIND, LLM_INTERACTION_KIND_PRE_MOVE],
  },
]
const TOOL_OWNED = new Set(TOOL_OWN_KINDS.flatMap((t) => t.kinds))

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
    HOST_FUNCTION_ASK,
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
      // Covered, not named: the loader asks `EmitAllows`, so a `*` write
      // grant pays for propose and ask the way it pays for any other kind.
      case HOST_FUNCTION_PROPOSE:
        if (!writesCoverToolKinds(writes, named)) {
          hints.push({
            function: named,
            property: WRITES_GRANT,
            message: `propose writes a change request, so data.${WRITES_GRANT} must name ${RECORD_PATCH_REQUEST_KIND}.`,
          })
        }
        break
      case HOST_FUNCTION_ASK:
        if (!writesCoverToolKinds(writes, named)) {
          hints.push({
            function: named,
            property: WRITES_GRANT,
            message: `ask writes an interaction, so data.${WRITES_GRANT} must name ${LLM_INTERACTION_KIND}.`,
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

/** Whether a write grant covers one of the kinds a host tool writes on its
 * own account (any grant covers a tool that has none). */
function writesCoverToolKinds(writes: string[], tool: string): boolean {
  const own = TOOL_OWN_KINDS.find((t) => t.tool === tool)
  if (!own) return true
  return own.kinds.some((kind) => writes.some((p) => grantCovers(p, kind)))
}

// ── editing the grants ──────────────────────────────────────────────────────

/** The two grants the console edits: what an agent may see and change. */
export type GrantSide = "reads" | "writes"

/** Whether a write grant entry is a tool's own grant (the change-request
 * kind `propose` lands, the interaction kind `ask` lands) rather than a
 * collection the person picks. */
export function isToolOwnedKind(kind: string): boolean {
  return TOOL_OWNED.has(kind)
}

/** The kinds (identities or globs) one grant names, in order. On `writes`
 * the tools' own kinds are left out (`isToolOwnedKind`): an edit carries
 * them through untouched. */
export function grantKindsOf(
  properties: Record<string, unknown>,
  side: GrantSide
): string[] {
  const permissions = permissionsOf(properties)
  if (side === "writes") {
    return identitiesOf(permissions.writes).filter((k) => !isToolOwnedKind(k))
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
 * leaves `reads` out. A tool's own kind stays on `writes` wherever it was
 * named, and is named when the agent holds the tool and a glob the edit
 * drops was what covered it: the loader refuses `propose` or `ask` without
 * it. */
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
  const before = identitiesOf(permissions.writes)
  const writes = [
    ...kinds.filter((k) => !isToolOwnedKind(k)),
    ...before.filter(isToolOwnedKind),
  ]
  const tools = hostToolsOf(properties)
  for (const { tool, kinds: own } of TOOL_OWN_KINDS) {
    if (!tools.includes(tool)) continue
    if (
      writesCoverToolKinds(before, tool) &&
      !writesCoverToolKinds(writes, tool)
    ) {
      writes.push(own[0])
    }
  }
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

/** The grant entry that is every kind: "All your data". */
export const ALL_KINDS = "*"

/** The auth kinds no glob reaches: a grant that means one names it
 * (`vocabulary.AuthKinds`, record 0080). */
export const AUTH_KINDS: ReadonlySet<string> = new Set([
  "substrate.reamde.dev/core/token",
  "substrate.reamde.dev/core/credential",
  "substrate.reamde.dev/core/secret",
  "substrate.reamde.dev/core/recoverykey",
])

/** The auth kinds a write grant may not name even spelled out: the loader
 * refuses the whole agent (`vocabulary.ownerOnlyKinds`). */
export const OWNER_ONLY_KINDS: ReadonlySet<string> = new Set([
  "substrate.reamde.dev/core/token",
  "substrate.reamde.dev/core/credential",
  "substrate.reamde.dev/core/recoverykey",
])

/** Whether a grant entry is a glob rather than one kind. */
export function isGrantGlob(entry: string): boolean {
  return entry === ALL_KINDS || entry.endsWith("/*")
}

/** Whether one grant entry reaches a kind: `patternCovers` less the auth
 * kinds, which no glob reaches (`vocabulary.GrantMatches`). */
export function grantCovers(pattern: string, kind: string): boolean {
  if (isGrantGlob(pattern) && AUTH_KINDS.has(kind)) return false
  return patternCovers(pattern, kind)
}

/** Whether every kind `inner` grants, `outer` grants too; `inner` may be a
 * glob itself (`vocabulary.GrantSubsumes`). */
function grantSubsumes(outer: string, inner: string): boolean {
  if (outer === inner) return true
  if (!isGrantGlob(inner)) return grantCovers(outer, inner)
  if (outer === ALL_KINDS) return true
  return outer.endsWith("/*") && inner.startsWith(outer.slice(0, -1))
}

/** The entries a grant ends with when the picker hands back `picked` over
 * `held`, in the order held (a pick appends, so the stored list does not
 * reshuffle). Picking "All your data" drops what it already covers (an auth
 * kind named on its own stays). Picking one collection while "All your
 * data" is held narrows the grant to that collection: one the glob covers
 * adds nothing, so the pick can only mean "just this". */
export function nextGrant(held: string[], chosen: string[]): string[] {
  const added = chosen.filter((k) => !held.includes(k))
  const picked = [...held.filter((k) => chosen.includes(k)), ...added]
  if (added.includes(ALL_KINDS)) {
    return [
      ALL_KINDS,
      ...picked.filter((k) => k !== ALL_KINDS && !grantSubsumes(ALL_KINDS, k)),
    ]
  }
  if (
    held.includes(ALL_KINDS) &&
    picked.includes(ALL_KINDS) &&
    added.some((k) => grantSubsumes(ALL_KINDS, k))
  ) {
    return picked.filter((k) => k !== ALL_KINDS)
  }
  return picked
}

/** The entries of `held` that `next` no longer covers: what an edit takes
 * away. Empty for an edit that only adds, or that drops an entry another
 * one still covers. */
export function grantNarrowing(held: string[], next: string[]): string[] {
  return held.filter((k) => !next.some((n) => grantSubsumes(n, k)))
}

/** Whether a kind belongs among one grant's choices. A write grant never
 * offers the owner-only kinds (the loader refuses them) or a tool's own kind
 * (which `permissionsWith` carries through). */
export function grantOffers(side: GrantSide, kind: string): boolean {
  if (side === "reads") return true
  return !OWNER_ONLY_KINDS.has(kind) && !isToolOwnedKind(kind)
}

// ── who can set up a collection ─────────────────────────────────────────────

/** Whether one grant pattern covers a kind: the kind itself, a
 * `<authority>/<package>/*` or `<authority>/*` over it, or `*`. The auth
 * carve-out is `grantCovers`'. */
export function patternCovers(pattern: string, kind: string): boolean {
  if (pattern === "*" || pattern === kind) return true
  return pattern.endsWith("/*") && kind.startsWith(pattern.slice(0, -1))
}

/** Whether an agent can declare a kind, which is what setting up a
 * collection is: it holds the `write` tool and its write grant covers the
 * kind kind. `propose` does not count: accepting a proposal writes through
 * the record path, which refuses a kind record (a system kind), so a
 * proposed kind never lands. */
export function canDeclareKinds(properties: Record<string, unknown>): boolean {
  if (!hostToolsOf(properties).includes(HOST_FUNCTION_WRITE)) return false
  return identitiesOf(permissionsOf(properties).writes).some((p) =>
    patternCovers(p, KIND_KIND)
  )
}

/** The agent Add a collection hands a request to: one a person can chat with
 * that can declare kinds. The one they last talked to (`lastTalkedTo`, an
 * agent id) when it can, else the first that can. Undefined when none can:
 * an agent that cannot declare a kind is never chosen, however recently it
 * was talked to. */
export function collectionMaker<
  T extends { id: string; properties: Record<string, unknown> },
>(agents: T[], lastTalkedTo?: string): T | undefined {
  const able = agents.filter(
    (a) => a.properties.hiddenFromChat !== true && canDeclareKinds(a.properties)
  )
  return able.find((a) => a.id === lastTalkedTo) ?? able[0]
}
