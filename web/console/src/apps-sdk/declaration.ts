/** A kind's declaration as the guest reads it: the properties with their
 * datatypes, the state machines with their arms, the enum labels and the
 * temporal point, all derived from the `KindInfo` rows the host handed over
 * at mount, so a transition or a form checks the machine and labels a value
 * without a call. The derivations are copies of the console's
 * (`lib/record-schema.ts`, `lib/apps/machine.ts`, `lib/definition.ts`) kept
 * pure here, because the SDK chunk must carry no console module that pulls
 * the router or the query client. */

import type { KindInfo } from "@/lib/api/types"

export interface EnumValue {
  value: string
  label: string
}

export interface PropSpec {
  name: string
  /** The declaration's `displayName`, else the humanized name. */
  label: string
  /** The declared datatype (`string`, `datetime`, `state`, `reference`, …). */
  kind: string
  required: boolean
  repeated: boolean
  /** An enum's admitted set, declaration order. */
  values?: EnumValue[]
  /** A state property's states and the state a record is born into. */
  states?: string[]
  initial?: string
  /** A reference's pinned kind. */
  to?: string
  default?: unknown
  description?: string
}

export interface Arm {
  from: string
  to: string
}

export interface Declaration {
  identity: string
  name: string
  authority: string
  package: string
  version: number
  description: string
  displayTemplate?: string
  properties: PropSpec[]
  property(name: string): PropSpec | undefined
  /** The named state property, else the kind's sole one; undefined when the
   * kind declares none, or more than one and none is named. */
  stateProperty(name?: string): PropSpec | undefined
  /** The declared arms of one state property; empty means every move
   * between two distinct states is admitted. */
  transitions(property: string): Arm[]
  admits(property: string, from: string | undefined, to: string): boolean
  /** The enum's authored label for a value, else the value itself. */
  labelOf(property: string, value: unknown): string
  /** The property the kind's temporal trait binds the point to (`dueAt` for
   * `temporal(point: dueAt)`, `at` for a plain point and for a range);
   * undefined for a kind that binds none. */
  temporalPoint?: string
}

/** `backfillDepth` becomes "Backfill depth"; an ALL-CAPS run keeps its case. */
export function humanizeName(name: string): string {
  const spaced = name
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/([A-Z]+)([A-Z][a-z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim()
  if (!spaced) return name
  const words = spaced
    .split(/\s+/)
    .map((word) => (/^[A-Z0-9]{2,}$/.test(word) ? word : word.toLowerCase()))
  const first = words[0]
  words[0] = first.charAt(0).toUpperCase() + first.slice(1)
  return words.join(" ")
}

function enumValues(raw: unknown): EnumValue[] | undefined {
  if (!Array.isArray(raw)) return undefined
  const out: EnumValue[] = []
  for (const item of raw) {
    if (typeof item === "string") {
      out.push({ value: item, label: "" })
    } else if (item && typeof item === "object") {
      const rec = item as Record<string, unknown>
      if (typeof rec.value === "string") {
        out.push({
          value: rec.value,
          label: typeof rec.label === "string" ? rec.label : "",
        })
      }
    }
  }
  return out.length ? out : undefined
}

function stringList(v: unknown): string[] | undefined {
  if (!Array.isArray(v)) return undefined
  const out = v.filter((x): x is string => typeof x === "string")
  return out.length ? out : undefined
}

type Raw = Record<string, unknown>

function rawProperties(kind: KindInfo): Record<string, Raw> {
  const def = (kind.definition ?? {}) as Raw
  return (def.properties ?? {}) as Record<string, Raw>
}

function specOf(name: string, def: Raw): PropSpec {
  const displayName =
    typeof def.displayName === "string" && def.displayName.trim()
      ? def.displayName.trim()
      : undefined
  return {
    name,
    label: displayName ?? humanizeName(name),
    kind: typeof def.type === "string" ? def.type : "string",
    required: def.required === true,
    repeated: def.repeated === true,
    values: enumValues(def.values),
    states: stringList(def.states),
    initial: typeof def.initial === "string" ? def.initial : undefined,
    to: typeof def.kind === "string" ? def.kind : undefined,
    default: def.default,
    description:
      typeof def.description === "string" ? def.description : undefined,
  }
}

/** `temporal(point)` → `at`, `temporal(range)` → `at`, and a remap like
 * `temporal(point: dueAt)` → `dueAt`; the first binding wins. */
export function temporalPointOf(kind: KindInfo): string | undefined {
  const traits = ((kind.definition ?? {}) as Raw).traits
  if (!Array.isArray(traits)) return undefined
  for (const trait of traits) {
    if (typeof trait !== "string") continue
    const m = trait.match(/^temporal\(\s*(point|range)(?:\s*:\s*(\w+))?\s*\)$/)
    if (!m) continue
    return m[1] === "range" ? "at" : (m[2] ?? "at")
  }
  return undefined
}

export function declarationOf(kind: KindInfo): Declaration {
  const raw = rawProperties(kind)
  const properties = Object.entries(raw)
    .map(([name, def]) => specOf(name, def ?? {}))
    .sort(
      (a, b) =>
        Number(b.required) - Number(a.required) || a.name.localeCompare(b.name)
    )
  const byName = new Map(properties.map((p) => [p.name, p]))
  const states = properties.filter((p) => p.kind === "state")
  const def = (kind.definition ?? {}) as Raw

  const transitions = (property: string): Arm[] => {
    const arms = raw[property]?.transitions
    if (!Array.isArray(arms)) return []
    const out: Arm[] = []
    for (const arm of arms) {
      if (!arm || typeof arm !== "object") continue
      const { from, to } = arm as Raw
      if (typeof from === "string" && typeof to === "string") {
        out.push({ from, to })
      }
    }
    return out
  }

  return {
    identity: kind.identity,
    name: kind.name,
    authority: kind.authority,
    package: kind.package,
    version: kind.version,
    description: kind.description,
    displayTemplate:
      typeof def.displayTemplate === "string" ? def.displayTemplate : undefined,
    properties,
    property: (name) => byName.get(name),
    stateProperty(name) {
      if (name) return states.find((s) => s.name === name)
      return states.length === 1 ? states[0] : undefined
    },
    transitions,
    admits(property, from, to) {
      if (from === to) return false
      const arms = transitions(property)
      if (!arms.length) return from !== undefined
      return arms.some((a) => a.from === from && a.to === to)
    },
    labelOf(property, value) {
      const text = value === undefined || value === null ? "" : String(value)
      const found = byName.get(property)?.values?.find((v) => v.value === text)
      return found?.label || text
    },
    temporalPoint: temporalPointOf(kind),
  }
}
