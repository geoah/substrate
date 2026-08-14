/** Change-request domain logic, pure and tested: the op a request carries,
 * the target it names, the stored diff read as before/after rows against the
 * target's live values, the drift between the version the diff was computed
 * against and the target's current one, and the decision patch.
 *
 * Wire facts (kinds/core.substrate.reamde.dev/recordpatchrequest.yaml,
 * internal/engine/write.go): `op` is create|patch|delete and ABSENT MEANS
 * PATCH (an older request stores no op, so back-compat is by omission);
 * `targetKind`/`targetId` name a create's record, which nothing can point at
 * yet; the `target` edge names a patch's or a delete's; `targetVersion` is the
 * version the diff was computed against and the accept CAS's against it;
 * `diff` is json, decoded STRICTLY as a PatchInput (`properties`) or, for a
 * create, a PutInput (`properties` plus `edges`). Accepting is what applies
 * the change: the `decision` transition runs `applyDiff` in the same
 * transaction, and a decision on the human path must carry the REQUEST's
 * `ifVersion` (write.go refuses it otherwise), which is also what enforces the
 * envelope the reviewer read. */

import type { EdgeTarget, KindInfo, SubstrateRecord } from "@/lib/api/types"
import { declaredEdges, declaredProperties } from "@/lib/definition"
import {
  DECISION_INITIAL,
  decisionOf,
  sameValue,
  verdictNote,
  type Decision,
  type MergeVerdict,
} from "@/lib/mergerequests"

// The decision is ONE machine and both request kinds declare it identically
// (proposed → accepted|rejected, accepting is what applies), and the note
// convention is one too, so they are read from one place rather than declared
// twice and left free to drift.
export { DECISION_INITIAL, decisionOf, verdictNote as decisionNote }
export type { Decision }

/** The two verdicts a person hands down. */
export type Verdict = MergeVerdict

// ── the op ──────────────────────────────────────────────────────────────────

export const CHANGE_OPS = ["create", "patch", "delete"] as const
export type ChangeOp = (typeof CHANGE_OPS)[number]

/** The change verb. An absent (or empty) `op` IS patch, per the declaration.
 * An op the console does not know yields undefined rather than being read as
 * patch: the three ops do opposite things, and the accept would refuse it
 * anyway, so the surface says so instead of guessing. */
export function changeOp(r: SubstrateRecord): ChangeOp | undefined {
  const op = r.properties.op
  if (op === undefined || op === null || op === "") return "patch"
  return CHANGE_OPS.includes(op as ChangeOp) ? (op as ChangeOp) : undefined
}

// ── the target ──────────────────────────────────────────────────────────────

export interface ChangeTargetRef {
  /** The target's kind reference. */
  kind: string
  id: string
  /** Display sugar the `target` edge carries on the wire, when it does. */
  title?: string
  /** How the request names it: `edge` is the `target` edge (patch, delete),
   * `declared` is a create's own targetKind/targetId, which no edge can point
   * at because the record does not exist yet. */
  via: "edge" | "declared"
}

export function changeTarget(r: SubstrateRecord): ChangeTargetRef | undefined {
  const edge = edgeRef(r.edges?.target?.[0])
  const declared = declaredRef(r)
  return changeOp(r) === "create" ? (declared ?? edge) : (edge ?? declared)
}

function edgeRef(target?: EdgeTarget): ChangeTargetRef | undefined {
  if (!target?.id) return undefined
  return {
    kind: target.kind,
    id: target.id,
    title: target.title,
    via: "edge",
  }
}

function declaredRef(r: SubstrateRecord): ChangeTargetRef | undefined {
  const kind = str(r.properties.targetKind)
  const id = str(r.properties.targetId)
  return kind && id ? { kind, id, via: "declared" } : undefined
}

// ── the stored diff ─────────────────────────────────────────────────────────

/** One edge a create request would write, normalized off `diff.edges`
 * (`substrate.EdgeInput`: `{rel, to: {kind?, id}, properties?}`). `kind` is
 * absent where the declaration pins one target kind and the writer left it
 * implicit. */
export interface ProposedEdge {
  rel: string
  kind?: string
  id: string
  properties?: Record<string, unknown>
}

export interface ProposedDiff {
  properties: Record<string, unknown>
  labels?: Record<string, unknown>
  annotations?: Record<string, unknown>
  /** Create only: the edges the minted record is born with. */
  edges: ProposedEdge[]
  /** Top-level keys the substrate's strict decode would refuse, so the surface
   * can say why an accept will fail before anybody presses the button. */
  refused: string[]
  /** True when `diff` is present but is not an object at all. */
  unreadable: boolean
}

/** The keys `decodeDiff`/`decodeCreate` admit: a patch's stored diff decodes
 * as a PatchInput, a create's as a PutInput, both with
 * DisallowUnknownFields, so a key outside the set FAILS the accept rather than
 * being quietly ignored. `kind`/`id` ride the create set because PutInput
 * carries them, though the accept overwrites both from the request. */
const PATCH_DIFF_KEYS = [
  "properties",
  "labels",
  "annotations",
  "addFinalizers",
  "removeFinalizers",
  "ifVersion",
]
const CREATE_DIFF_KEYS = [
  "kind",
  "id",
  "properties",
  "labels",
  "annotations",
  "edges",
  "ifVersion",
]

export function proposedDiff(r: SubstrateRecord): ProposedDiff {
  const raw = r.properties.diff
  const empty: ProposedDiff = {
    properties: {},
    edges: [],
    refused: [],
    unreadable: false,
  }
  if (raw === undefined || raw === null) return empty
  if (typeof raw !== "object" || Array.isArray(raw)) {
    return { ...empty, unreadable: true }
  }
  const diff = raw as Record<string, unknown>
  const admitted = changeOp(r) === "create" ? CREATE_DIFF_KEYS : PATCH_DIFF_KEYS
  return {
    properties: mapOf(diff.properties) ?? {},
    labels: mapOf(diff.labels),
    annotations: mapOf(diff.annotations),
    edges: proposedEdges(diff.edges),
    refused: Object.keys(diff)
      .filter((k) => !admitted.includes(k))
      .sort(),
    unreadable: false,
  }
}

function mapOf(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined
  }
  return value as Record<string, unknown>
}

function proposedEdges(value: unknown): ProposedEdge[] {
  if (!Array.isArray(value)) return []
  const out: ProposedEdge[] = []
  for (const item of value) {
    const edge = mapOf(item)
    if (!edge) continue
    const rel = str(edge.rel)
    const to = mapOf(edge.to)
    const id = str(to?.id)
    if (!rel || !id) continue
    out.push({
      rel,
      kind: str(to?.kind),
      id,
      properties: mapOf(edge.properties),
    })
  }
  return out
}

// ── before / after ──────────────────────────────────────────────────────────

/** What the accept does to one property:
 * - `clear`: the diff names `null`, which DELETES the key (a patch merges
 *   key-wise). Never `clear` for a key the target does not carry: the write
 *   path skips a delete of an absent key, so nothing changes.
 * - `set`: a value the target already carries, replaced.
 * - `add`: a value the target has no key for at all, and every property of a
 *   create (nothing exists yet to compare against).
 * - `unchanged`: the target already satisfies the proposed value. A patch
 *   whose whole diff is unchanged FAILS the accept (write.go refuses the
 *   silent no-op), so this is a warning, not a detail. */
export type ChangeEffect = "clear" | "set" | "add" | "unchanged"

export interface ChangeRow {
  key: string
  /** False for a property the target's kind does not declare. */
  declared: boolean
  /** The declaration's one-liner, where the kind wrote one. */
  description?: string
  /** The target's live value; always undefined for a create. */
  before: unknown
  after: unknown
  /** Who last had a change to this property accepted on the TARGET, so a
   * reviewer can see whose value the accept would overwrite. */
  manager?: string
  effect: ChangeEffect
}

// What the reviewer must look at first: values the accept removes or
// overwrites, then what it adds, then the rows that change nothing.
const EFFECT_ORDER: Record<ChangeEffect, number> = {
  clear: 0,
  set: 1,
  add: 2,
  unchanged: 3,
}

/** The before/after rows the detail page renders: ONE row per property the
 * diff names, because a patch touches only what it names (unlike a merge,
 * which compares two whole records). `target` is the live record for a patch
 * or a delete and absent for a create. */
export function deriveChangeRows(
  proposed: Record<string, unknown>,
  target?: SubstrateRecord,
  kind?: KindInfo
): ChangeRow[] {
  const declared = kind ? declaredProperties(kind) : []
  const rows: ChangeRow[] = []

  for (const key of Object.keys(proposed)) {
    const after = proposed[key]
    const before = target?.properties[key]
    const had = target ? key in target.properties : false
    let effect: ChangeEffect
    if (after === null) {
      effect = had ? "clear" : "unchanged"
    } else if (!target || !had) {
      effect = "add"
    } else {
      effect = sameValue(before, after) ? "unchanged" : "set"
    }
    rows.push({
      key,
      declared: declared.some((p) => p.name === key),
      description: declared.find((p) => p.name === key)?.description,
      before,
      after,
      manager: target?.propertyMeta?.[key]?.manager,
      effect,
    })
  }

  rows.sort((a, b) => {
    const order = EFFECT_ORDER[a.effect] - EFFECT_ORDER[b.effect]
    if (order !== 0) return order
    if (a.declared !== b.declared) return a.declared ? -1 : 1
    return a.key.localeCompare(b.key)
  })
  return rows
}

/** An edge a create would write, matched to its declaration so the preview can
 * say what the rel IS and whether the kind declares it at all. */
export function describeProposedEdge(
  edge: ProposedEdge,
  kind?: KindInfo
): { declared: boolean; description?: string } {
  const declaration = kind
    ? declaredEdges(kind).find((e) => e.rel === edge.rel)
    : undefined
  return {
    declared: Boolean(declaration),
    description: declaration?.description,
  }
}

// ── the stale target ────────────────────────────────────────────────────────

export interface TargetDrift {
  /** `targetVersion`: what the target was when the diff was computed. */
  proposedAgainst: number
  current: number
}

/** The target moved after the proposal was written. It is a WARNING, not a
 * refusal: the accept CAS's the patch against `targetVersion` and fails the
 * whole transition, so a stale request must be re-proposed rather than
 * force-accepted. Undefined when the versions agree or either is unknown. */
export function targetDrift(
  r: SubstrateRecord,
  target?: SubstrateRecord
): TargetDrift | undefined {
  const proposedAgainst = r.properties.targetVersion
  if (typeof proposedAgainst !== "number" || !target) return undefined
  if (proposedAgainst === target.version) return undefined
  return { proposedAgainst, current: target.version }
}

// ── the decision patch ─────────────────────────────────────────────────────

export interface DecisionPatch {
  properties: { decision: Verdict }
  annotations?: Record<string, string>
  /** NOT optional: deciding a change request must carry the version of the
   * request the reviewer read, so a concurrent write cannot swap the envelope
   * under the decision. The write path refuses a decision without it. */
  ifVersion: number
}

/** The single atomic submit: the ordinary state transition, CAS'd on the
 * request's own version, with the optional note riding the same write as the
 * `owner/note` annotation. */
export function decisionPatch(
  verdict: Verdict,
  version: number,
  note?: string
): DecisionPatch {
  const trimmed = note?.trim()
  const patch: DecisionPatch = {
    properties: { decision: verdict },
    ifVersion: version,
  }
  if (trimmed) patch.annotations = { "owner/note": trimmed }
  return patch
}

// ── what the server left behind ─────────────────────────────────────────────

export interface ApplyConflict {
  reason: string
  /** When the refused apply happened, where the annotation carries it. */
  at?: string
}

/** The server's own account of a refused apply: the transition rolls back with
 * the transaction and `substrate/conflict` is the record the owner sees
 * (`{reason, at}`). Any `<namespace>/conflict` key reads back, and a plain
 * string is accepted too, so an older or a differently namespaced writer is
 * still surfaced rather than swallowed. */
export function applyConflict(r: SubstrateRecord): ApplyConflict | undefined {
  for (const [key, value] of Object.entries(r.annotations ?? {})) {
    if (!/(^|\/)conflict$/i.test(key)) continue
    if (typeof value === "string" && value) return { reason: value }
    const note = mapOf(value)
    const reason = str(note?.reason)
    if (reason) return { reason, at: str(note?.at) }
    if (value !== undefined && value !== null) {
      return { reason: JSON.stringify(value) }
    }
  }
  return undefined
}

// ── who and why ─────────────────────────────────────────────────────────────

/** Properties only a PROPOSAL writes, so the manager of the first one present
 * is whoever proposed the change. `decision` is not among them: it is a state,
 * and states carry no manager row. */
const PROPOSAL_PROPERTIES = [
  "diff",
  "op",
  "targetKind",
  "targetVersion",
  "rationale",
]

export function proposerOf(r: SubstrateRecord): string | undefined {
  for (const name of PROPOSAL_PROPERTIES) {
    const manager = r.propertyMeta?.[name]?.manager
    if (manager) return manager
  }
  return undefined
}

/** `decidedAt` is STAMPED by the decision transition, so its manager is the
 * actor whose decision it was. */
export function deciderOf(r: SubstrateRecord): string | undefined {
  return r.propertyMeta?.decidedAt?.manager
}

export function rationaleOf(r: SubstrateRecord): string | undefined {
  return str(r.properties.rationale)
}

export function decidedAtOf(r: SubstrateRecord): string | undefined {
  return str(r.properties.decidedAt)
}

function str(value: unknown): string | undefined {
  return typeof value === "string" && value ? value : undefined
}
