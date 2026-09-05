/** Change-request domain logic, pure and tested: the op a request carries,
 * the target it names, the stored diff read as before/after rows against the
 * target's live values, the drift between the version the accept will CAS
 * against and the target's current one, and the decision patch.
 *
 * Wire facts (kinds/substrate.reamde.dev/core/recordpatchrequest.yaml,
 * internal/engine/write.go): `op` is create|patch|delete and ABSENT MEANS
 * PATCH (an older request stores no op, so back-compat is by omission);
 * `targetKind`/`targetId` name a create's record, which nothing can point at
 * yet; the `target` REFERENCE names a patch's or a delete's; `targetVersion` is the
 * version the diff was computed against, which the accept CAS's against unless
 * the diff carries its own `ifVersion`; `diff` is json, decoded STRICTLY as a
 * PatchInput (properties, labels, annotations, finalizers, ifVersion) or, for a
 * create, a PutInput (the same set, minus the finalizers). Accepting is what applies the
 * change: the `decision` transition runs `applyDiff` in the same transaction,
 * and a decision on the human path must carry the REQUEST's `ifVersion`
 * (write.go refuses it otherwise), which is also what enforces the envelope the
 * reviewer read.
 *
 * NOTHING here is shared with the merge request's logic, deliberately. The two
 * kinds are two independent declarations that happen to spell `decision` the
 * same way today; neither guarantees the other's states, and their value
 * equality is genuinely different (see `sameApplied`). */

import {
  readReference,
  type KindInfo,
  type SubstrateRecord,
} from "@/lib/api/types"
import { declaredProperties } from "@/lib/definition"
import { splitRecordPath } from "@/lib/record-path"

// ── the decision ────────────────────────────────────────────────────────────

export const DECISIONS = ["proposed", "accepted", "rejected"] as const
export type Decision = (typeof DECISIONS)[number]
export const DECISION_INITIAL: Decision = "proposed"

/** The two verdicts a person hands down. */
export type Verdict = "accepted" | "rejected"

export function decisionOf(r: SubstrateRecord): Decision {
  const d = r.properties.decision
  return d === "accepted" || d === "rejected" ? d : "proposed"
}

/** The note a decided request carries, wherever the namespace put it:
 * `owner/note` is what the console writes; any `<actor>/note` reads back. */
export function decisionNote(r: SubstrateRecord): string | undefined {
  for (const [key, value] of Object.entries(r.annotations ?? {})) {
    if (!/(^|\/)note$/.test(key)) continue
    if (typeof value === "string" && value) return value
  }
  return undefined
}

// ── value equality, as the APPLY decides it ─────────────────────────────────

/** Whether the accept would consider a proposed value equal to the stored one.
 * The write path compares with `jsonEqual` (write.go's `take`): byte equality
 * over Go's JSON encoding, so ARRAYS ARE ORDER-SENSITIVE (`["b","a"]` over
 * `["a","b"]` is a real change and the accept applies it) while object keys are
 * not (Go marshals maps with sorted keys). A recompute-flavoured compare that
 * sorted arrays would report "no change" for a reorder the substrate performs,
 * which is a preview that lies. */
export function sameApplied(a: unknown, b: unknown): boolean {
  return canonicalJSON(a) === canonicalJSON(b)
}

function canonicalJSON(v: unknown): string {
  if (Array.isArray(v)) return `[${v.map(canonicalJSON).join(",")}]`
  if (typeof v === "object" && v !== null) {
    const rec = v as Record<string, unknown>
    // An undefined-valued key does not survive JSON on either side, exactly as
    // it does not survive Go's encoder.
    const keys = Object.keys(rec)
      .filter((k) => rec[k] !== undefined)
      .sort()
    return `{${keys.map((k) => `${JSON.stringify(k)}:${canonicalJSON(rec[k])}`).join(",")}}`
  }
  return JSON.stringify(v) ?? "undefined"
}

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
  /** How the request names it: `reference` is the `target` property (patch,
   * delete), `declared` is a create's own targetKind/targetId, which no
   * reference can point at because the record does not exist yet. */
  via: "reference" | "declared"
}

export function changeTarget(r: SubstrateRecord): ChangeTargetRef | undefined {
  const pointed = referenceRef(r.properties.target)
  const declared = declaredRef(r)
  return changeOp(r) === "create"
    ? (declared ?? pointed)
    : (pointed ?? declared)
}

/** The `target` reference read as its two halves. A reference stores the
 * referent's whole record PATH, so the kind grammar splits it and the registry
 * is never consulted. */
function referenceRef(value: unknown): ChangeTargetRef | undefined {
  const held = readReference(value)
  if (!held) return undefined
  const target = splitRecordPath(held.path)
  return target
    ? { kind: target.kind, id: target.id, via: "reference" }
    : undefined
}

function declaredRef(r: SubstrateRecord): ChangeTargetRef | undefined {
  const kind = str(r.properties.targetKind)
  const id = str(r.properties.targetId)
  return kind && id ? { kind, id, via: "declared" } : undefined
}

// ── the stored diff ─────────────────────────────────────────────────────────

/** A value the console cannot read where the decode expects a shape: kept
 * verbatim, because "the diff names nothing" and "the diff names something the
 * substrate will refuse" are opposite facts and the reviewer needs the second
 * one said out loud. */
export interface UnreadableField {
  /** The diff key. */
  key: string
  /** What was actually there. */
  raw: unknown
}

export interface ProposedDiff {
  properties: Record<string, unknown>
  labels?: Record<string, unknown>
  annotations?: Record<string, unknown>
  /** Finalizers the patch adds or removes. Named by the diff, applied by the
   * accept, and invisible in a property table, so they are carried here rather
   * than dropped: a finalizer-only diff is NOT an empty one. */
  addFinalizers: string[]
  removeFinalizers: string[]
  /** The diff's OWN CAS precondition. When present it OVERRIDES the stamped
   * `targetVersion` at accept (write.go: the stamp is only a fallback), so it
   * is what a drift warning must compare against. */
  ifVersion?: number
  /** Top-level keys the substrate's strict decode would refuse, so the surface
   * can say why an accept will fail before anybody presses the button. */
  refused: string[]
  /** Keys whose value is the wrong SHAPE for the decode (`properties: []`).
   * The accept fails on these too. */
  malformed: UnreadableField[]
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
  "ifVersion",
]

export function proposedDiff(r: SubstrateRecord): ProposedDiff {
  const raw = r.properties.diff
  const empty: ProposedDiff = {
    properties: {},
    addFinalizers: [],
    removeFinalizers: [],
    refused: [],
    malformed: [],
    unreadable: false,
  }
  if (raw === undefined || raw === null) return empty
  if (typeof raw !== "object" || Array.isArray(raw)) {
    return { ...empty, unreadable: true }
  }
  const diff = raw as Record<string, unknown>
  const admitted = changeOp(r) === "create" ? CREATE_DIFF_KEYS : PATCH_DIFF_KEYS
  const malformed: UnreadableField[] = []

  const map = (key: string): Record<string, unknown> | undefined => {
    if (!(key in diff) || diff[key] === undefined || diff[key] === null) {
      return undefined
    }
    const value = mapOf(diff[key])
    if (!value) malformed.push({ key, raw: diff[key] })
    return value
  }
  const strings = (key: string): string[] => {
    if (!(key in diff) || diff[key] === undefined || diff[key] === null) {
      return []
    }
    const value = diff[key]
    if (!Array.isArray(value) || value.some((v) => typeof v !== "string")) {
      malformed.push({ key, raw: value })
      return []
    }
    return value as string[]
  }

  let ifVersion: number | undefined
  if (diff.ifVersion !== undefined && diff.ifVersion !== null) {
    if (typeof diff.ifVersion === "number") ifVersion = diff.ifVersion
    else malformed.push({ key: "ifVersion", raw: diff.ifVersion })
  }

  return {
    properties: map("properties") ?? {},
    labels: map("labels"),
    annotations: map("annotations"),
    addFinalizers: strings("addFinalizers"),
    removeFinalizers: strings("removeFinalizers"),
    ifVersion,
    refused: Object.keys(diff)
      .filter((k) => !admitted.includes(k))
      .sort(),
    // Sorted for the same reason `refused` is: the order values happen to be
    // parsed in is not information, and a stable list is what a surface can
    // render twice the same way.
    malformed: malformed.sort((a, b) => a.key.localeCompare(b.key)),
    unreadable: false,
  }
}

function mapOf(value: unknown): Record<string, unknown> | undefined {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return undefined
  }
  return value as Record<string, unknown>
}

/** True when the substrate's strict decode will refuse this diff whole: an
 * unreadable value, a key PatchInput/PutInput does not carry, or a key whose
 * value is the wrong shape. Nothing is applied in that case, and no accept can
 * succeed until the proposal is rewritten. */
export function diffCannotApply(diff: ProposedDiff): boolean {
  return diff.unreadable || diff.refused.length > 0 || diff.malformed.length > 0
}

/** True when the diff names NOTHING the write path would act on. The accept is
 * refused outright (write.go's `diffEmpty`), so this is a warning, not a quiet
 * detail. Finalizers count: a finalizer-only diff names something. */
export function diffNamesNothing(diff: ProposedDiff): boolean {
  return (
    Object.keys(diff.properties).length === 0 &&
    Object.keys(diff.labels ?? {}).length === 0 &&
    Object.keys(diff.annotations ?? {}).length === 0 &&
    diff.addFinalizers.length === 0 &&
    diff.removeFinalizers.length === 0
  )
}

// ── before / after ──────────────────────────────────────────────────────────

/** What the accept does to one property:
 * - `clear`: the diff names `null`, which DELETES the key (a patch merges
 *   key-wise). Never `clear` for a key the target does not carry: the write
 *   path skips a delete of an absent key, so nothing changes.
 * - `set`: a value the target already carries, replaced.
 * - `add`: a value the target has no key for at all, and every property of a
 *   create (nothing exists yet to compare against).
 * - `unchanged`: the target already satisfies the proposed value, as the APPLY
 *   compares it (`sameApplied`). A patch whose whole diff is unchanged FAILS
 *   the accept (write.go refuses the silent no-op). */
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
      effect = sameApplied(before, after) ? "unchanged" : "set"
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

/** True when accepting would apply NOTHING even though the diff names things:
 * every named property already matches, and no label, annotation or finalizer
 * rides along. The write path refuses that accept rather than recording a
 * decision that changed nothing, so the page says so before the button. Only
 * meaningful where the live target was read; a create has nothing to match. */
export function appliesNothing(diff: ProposedDiff, rows: ChangeRow[]): boolean {
  if (rows.length === 0) return false
  if (!rows.every((r) => r.effect === "unchanged")) return false
  return (
    Object.keys(diff.labels ?? {}).length === 0 &&
    Object.keys(diff.annotations ?? {}).length === 0 &&
    diff.addFinalizers.length === 0 &&
    diff.removeFinalizers.length === 0
  )
}

// ── the stale target ────────────────────────────────────────────────────────

/** Where the version the accept CAS's against comes from: the diff's own
 * `ifVersion` overrides, and the engine falls back to the stamped
 * `targetVersion` only when the diff carries none. */
export type CASSource = "diff.ifVersion" | "targetVersion"

export interface EffectiveCAS {
  version: number
  via: CASSource
}

export function effectiveCAS(
  r: SubstrateRecord,
  diff: ProposedDiff
): EffectiveCAS | undefined {
  if (diff.ifVersion !== undefined) {
    return { version: diff.ifVersion, via: "diff.ifVersion" }
  }
  const stamped = r.properties.targetVersion
  return typeof stamped === "number"
    ? { version: stamped, via: "targetVersion" }
    : undefined
}

export interface TargetDrift extends EffectiveCAS {
  current: number
}

/** The target moved past the version the accept will check it against. It is a
 * WARNING, not a refusal the console invents: the accept CAS's the patch and
 * fails the whole transition, so a stale request must be re-proposed rather
 * than force-accepted. Undefined when the versions agree or either is unknown. */
export function targetDrift(
  r: SubstrateRecord,
  diff: ProposedDiff,
  target?: SubstrateRecord
): TargetDrift | undefined {
  const cas = effectiveCAS(r, diff)
  if (!cas || !target || cas.version === target.version) return undefined
  return { ...cas, current: target.version }
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
 * and states carry no manager row. Reads off `propertyMeta`, which ONLY the
 * single-record read carries. */
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
