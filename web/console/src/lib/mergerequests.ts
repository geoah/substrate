/** Merge-request domain logic, pure and tested: the matcher's evidence read
 * honestly off the real wire fields, the field-by-field diff of the two
 * records with its §7.1 posture per row (what recompute settles vs what the
 * owner holds), and the verdict patch body — the console's one mutation
 * besides login.
 *
 * Wire facts (read live, 2026-08-06): `recordmergerequest.core.substrate.reamde.dev`
 * carries `decision` (state: proposed → accepted|rejected, accepting runs
 * `applyMerge` in the same transaction), `rationale` (string), `evidence`
 * (json: `{signals: [{signal, value?, jaccard?}], winner, loser}`) and the
 * `winner`/`loser` REFERENCE properties. The verdict is the ordinary transition patch —
 * `PATCH {properties: {decision}}` — with the optional note riding the same
 * atomic write as the `owner/note` annotation. */

import {
  readReference,
  type SubstrateRecord,
  type KindInfo,
} from "@/lib/api/types"
import { declaredProperties } from "@/lib/definition"
import { splitRecordPath, type RecordPathParts } from "@/lib/record-path"

// ── decisions ───────────────────────────────────────────────────────────────

export const DECISIONS = ["proposed", "accepted", "rejected"] as const
export type Decision = (typeof DECISIONS)[number]
export const DECISION_INITIAL: Decision = "proposed"

/** The two verdicts a person can hand down. */
export type MergeVerdict = "accepted" | "rejected"

export function decisionOf(mr: SubstrateRecord): Decision {
  const d = mr.properties.decision
  return d === "accepted" || d === "rejected" ? d : "proposed"
}

// ── the pair ────────────────────────────────────────────────────────────────

/** The two records a request names, each read off its `winner`/`loser`
 * REFERENCE property: the stored value is the referent's whole record path, so
 * the kind grammar splits it and the registry is never consulted. Either side
 * is undefined when the request carries no readable pointer there. */
export function mergePair(mr: SubstrateRecord): {
  winner?: RecordPathParts
  loser?: RecordPathParts
} {
  return {
    winner: pairSide(mr.properties.winner),
    loser: pairSide(mr.properties.loser),
  }
}

function pairSide(value: unknown): RecordPathParts | undefined {
  const held = readReference(value)
  return held ? splitRecordPath(held.path) : undefined
}

// ── evidence ────────────────────────────────────────────────────────────────

/** One matched signal, normalized: the dedupe function writes
 * `{signal: "email", value}` and `{signal: "name", jaccard}`; a future matcher
 * may write `score`. Anything unreadable yields no signals — the caller shows
 * the raw JSON instead of guessing. */
export interface EvidenceSignal {
  kind: string
  value?: string
  score?: number
}

export function evidenceSignals(evidence: unknown): EvidenceSignal[] {
  if (typeof evidence !== "object" || evidence === null) return []
  const raw = (evidence as { signals?: unknown }).signals
  if (!Array.isArray(raw)) return []
  const out: EvidenceSignal[] = []
  for (const item of raw) {
    if (typeof item !== "object" || item === null) return []
    const s = item as Record<string, unknown>
    const kind = s.signal ?? s.name
    if (typeof kind !== "string" || !kind) return []
    const score =
      typeof s.jaccard === "number"
        ? s.jaccard
        : typeof s.score === "number"
          ? s.score
          : undefined
    out.push({
      kind,
      value: typeof s.value === "string" ? s.value : undefined,
      score,
    })
  }
  return out
}

/** The matcher's confidence, one number for a table cell: the strongest
 * score any signal carries; undefined when no signal is scored (an exact
 * email match needs no number). */
export function evidenceScore(evidence: unknown): number | undefined {
  const scores = evidenceSignals(evidence)
    .map((s) => s.score)
    .filter((n): n is number => n !== undefined)
  return scores.length ? Math.max(...scores) : undefined
}

/** A signal said plainly: `both carry alex@acme.com`, `names match, 0.86`.
 * A signal the console has no words for keeps its wire fields. */
export function signalText(s: EvidenceSignal): string {
  if (s.kind === "email" && s.value) return `both carry ${s.value}`
  if (s.kind === "name" && s.score !== undefined)
    return `names match, ${s.score.toFixed(2)}`
  const parts = [s.kind]
  if (s.value) parts.push(s.value)
  if (s.score !== undefined) parts.push(s.score.toFixed(2))
  return parts.join(", ")
}

// ── the side-by-side diff ───────────────────────────────────────────────────

/** What the merge does to one row (the recompute story, per §7.1 and
 * engine/merge.go):
 * - `choice`: differing and held by the owner on either side — recompute
 *   yields to the owner, so the winner's value simply stands; the owner must
 *   choose (edit after the merge if the other value was the right one).
 * - `recompute`: differing and machine-held — values never migrate; the
 *   winner recomputes from the union of live sources after the merge.
 * - `equal`: both sides already agree; the merge changes nothing here.
 *
 * There is no row posture for a pointer moving: nothing re-points at a merge.
 * A reference at the loser resolves to the winner through the former-id trail,
 * so the value stays exactly as written. */
export type DiffPosture = "choice" | "recompute" | "equal"

export interface DiffRow {
  key: string
  /** False for a property present on a record but absent from the schema. */
  declared: boolean
  /** The record-56 one-liner, when the schema carries one. */
  description?: string
  winner: unknown
  loser: unknown
  winnerManager?: string
  loserManager?: string
  posture: DiffPosture
}

/** Value equality for the diff: repeated properties compare as multisets
 * (recompute unions sources — order is not a difference), everything else
 * compares structurally with sorted keys. */
export function sameValue(a: unknown, b: unknown): boolean {
  return canonical(a) === canonical(b)
}

function canonical(v: unknown): string {
  if (Array.isArray(v)) {
    const items = v.map(canonical)
    items.sort()
    return `[${items.join(",")}]`
  }
  if (typeof v === "object" && v !== null) {
    const rec = v as Record<string, unknown>
    const keys = Object.keys(rec).sort()
    return `{${keys.map((k) => `${JSON.stringify(k)}:${canonical(rec[k])}`).join(",")}}`
  }
  return JSON.stringify(v) ?? "undefined"
}

const POSTURE_ORDER: Record<DiffPosture, number> = {
  choice: 0,
  recompute: 1,
  equal: 2,
}

/** The field-by-field comparison the detail page renders: every declared
 * property of the pair's kind, plus anything either record actually carries
 * that the declaration does not name (honesty over tidiness). Rows order by
 * posture — what needs the owner first — then by name. */
export function deriveDiff(
  winner: SubstrateRecord,
  loser: SubstrateRecord,
  kind?: KindInfo
): DiffRow[] {
  const declaredProps = kind ? declaredProperties(kind) : []
  const declaredPropNames = new Set(declaredProps.map((p) => p.name))
  const propKeys = new Set<string>(declaredPropNames)
  for (const k of Object.keys(winner.properties)) propKeys.add(k)
  for (const k of Object.keys(loser.properties)) propKeys.add(k)

  const rows: DiffRow[] = []

  for (const key of propKeys) {
    const w = winner.properties[key]
    const l = loser.properties[key]
    if (w === undefined && l === undefined) continue
    const winnerManager = winner.propertyMeta?.[key]?.manager
    const loserManager = loser.propertyMeta?.[key]?.manager
    const equal = sameValue(w, l)
    // §7.1: recompute yields to the owner above all — a differing value held
    // by the owner on either side is the owner's to settle.
    const ownerHeld = winnerManager === "owner" || loserManager === "owner"
    rows.push({
      key,
      declared: declaredPropNames.has(key),
      description: declaredProps.find((p) => p.name === key)?.description,
      winner: w,
      loser: l,
      winnerManager,
      loserManager,
      posture: equal ? "equal" : ownerHeld ? "choice" : "recompute",
    })
  }

  rows.sort((a, b) => {
    const order = POSTURE_ORDER[a.posture] - POSTURE_ORDER[b.posture]
    if (order !== 0) return order
    if (a.declared !== b.declared) return a.declared ? -1 : 1
    return a.key.localeCompare(b.key)
  })
  return rows
}

// ── the verdict patch ───────────────────────────────────────────────────────

export interface VerdictPatch {
  properties: { decision: MergeVerdict }
  annotations?: Record<string, string>
}

/** The single atomic submit: the ordinary state-transition patch, with the
 * optional note riding the same write as the `owner/note` annotation (the
 * owner may write any namespaced key; the changelog row is this patch). */
export function verdictPatch(
  verdict: MergeVerdict,
  note?: string
): VerdictPatch {
  const trimmed = note?.trim()
  const patch: VerdictPatch = { properties: { decision: verdict } }
  if (trimmed) patch.annotations = { "owner/note": trimmed }
  return patch
}

/** The note a decided request carries, wherever the namespace put it —
 * `owner/note` is what the console writes; any `<actor>/note` reads back. */
export function verdictNote(mr: SubstrateRecord): string | undefined {
  for (const [key, value] of Object.entries(mr.annotations ?? {})) {
    if (!/(^|\/)note$/.test(key)) continue
    if (typeof value === "string" && value) return value
  }
  return undefined
}
