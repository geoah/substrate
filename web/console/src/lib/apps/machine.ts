/** A kind's state machines, read off the declaration: which property is the
 * badge, which moves a transition admits, and whether a move has a declared
 * way back (the Undo arm). `PropSpec` carries the states and the initial
 * state but not the arms, so the arms are read here from the raw declaration
 * block, the one place the console reads `transitions`. */

import type { KindInfo, RecordFilter } from "@/lib/api/types"
import { propSpecs, type PropSpec } from "@/lib/record-schema"

export interface Arm {
  from: string
  to: string
}

export function stateSpecs(kind: KindInfo): PropSpec[] {
  return propSpecs(kind).filter((s) => s.kind === "state")
}

/** The named state property, else the kind's sole one; undefined when the
 * kind declares none or more than one and the action does not say which. */
export function stateSpecOf(
  kind: KindInfo | undefined,
  property?: string
): PropSpec | undefined {
  if (!kind) return undefined
  const states = stateSpecs(kind)
  if (property) return states.find((s) => s.name === property)
  return states.length === 1 ? states[0] : undefined
}

/** The declared arms of one state property. An empty list means the machine
 * declares none, which the engine reads as every move admitted. */
export function transitionsOf(kind: KindInfo, property: string): Arm[] {
  const def = (kind.definition ?? {}) as Record<string, unknown>
  const props = (def.properties ?? {}) as Record<
    string,
    Record<string, unknown>
  >
  const raw = props[property]?.transitions
  if (!Array.isArray(raw)) return []
  const out: Arm[] = []
  for (const arm of raw) {
    if (!arm || typeof arm !== "object") continue
    const { from, to } = arm as Record<string, unknown>
    if (typeof from === "string" && typeof to === "string") {
      out.push({ from, to })
    }
  }
  return out
}

/** Whether the machine admits `from → to`. A machine with no declared arms
 * admits every move between two distinct states. */
export function admits(
  kind: KindInfo,
  property: string,
  from: string | undefined,
  to: string
): boolean {
  if (from === to) return false
  const arms = transitionsOf(kind, property)
  if (!arms.length) return from !== undefined
  return arms.some((a) => a.from === from && a.to === to)
}

/** Whether `to → from` is itself declared: the arm an Undo can take. */
export function returnArm(
  kind: KindInfo,
  property: string,
  from: string | undefined,
  to: string
): boolean {
  return from !== undefined && admits(kind, property, to, from)
}

/** The states a view's filter admits for its state property: the `in` list,
 * the one `eq` value, else every declared state. A badge is worth drawing
 * only when more than one is admitted. */
export function admittedStates(
  filter: RecordFilter,
  state: PropSpec
): string[] {
  const cond = filter.properties?.[state.name]
  if (cond?.in?.length) return cond.in.map(String)
  if (cond?.eq !== undefined) return [String(cond.eq)]
  return state.states ?? []
}
