/** A kind's status as core's `lifecycle` trait reads it (decisions 0226 and
 * 0247): the property it binds, renamed or not, its values in declared
 * order, which of them close a record (`done`, `dropped`), which are a
 * suggestion waiting for its owner (`proposed`), and, for a state machine,
 * the moves it allows. Every value in none of the lists is open, and a
 * record with no status is open. The Swift kit's `KindStatus`. */
export interface KindStatus {
  /** The kind's property the trait binds: `status`, or what the binding
   * renames it to (`lifecycle(status: state)`). */
  property: string
  values: string[]
  done: string[]
  dropped: string[]
  proposed: string[]
  /** A machine's moves as `from` to the states it may go to, in declared
   * order; undefined for an enum, which takes any of its values. */
  moves?: Record<string, string[]>
  /** A machine's initial state, which a create that names none lands in. */
  initial?: string
}

export type Standing = "proposed" | "open" | "done" | "dropped"

export const lifecycleTrait = "substrate.reamde.dev/core/lifecycle"

/** The trait one `traits:` entry binds, by its full identity: the entry
 * before any `(`. Undefined for an entry that is not three segments. */
export function boundTrait(entry: string): string | undefined {
  const identity = entry.split("(")[0].trim()
  return identity.split("/").length === 3 ? identity : undefined
}

/** What a `traits:` entry's remap renames, contract name to the kind's own:
 * `lifecycle(status: state)` is `{status: "state"}`. */
export function renames(entry: string): Record<string, string> {
  const open = entry.indexOf("(")
  const close = entry.lastIndexOf(")")
  if (open < 0 || close <= open) return {}
  const out: Record<string, string> = {}
  for (const part of entry.slice(open + 1, close).split(",")) {
    const [from, to] = part.split(":").map((s) => s.trim())
    if (from && to) out[from] = to
  }
  return out
}

type Json = unknown
const strings = (v: Json): string[] =>
  Array.isArray(v)
    ? v.flatMap((x) =>
        typeof x === "string"
          ? [x]
          : x &&
              typeof x === "object" &&
              typeof (x as { value?: unknown }).value === "string"
            ? [(x as { value: string }).value]
            : []
      )
    : []

/** The status of a kind that binds `lifecycle`, read off its declaration's
 * data map (a `core/kind` record's properties, or the `definition` a trait's
 * implementors route serves); undefined where it binds no such trait or its
 * status is no enum or machine. */
export function kindStatus(
  definition: Record<string, Json>
): KindStatus | undefined {
  const entries = Array.isArray(definition.traits)
    ? definition.traits.filter((e): e is string => typeof e === "string")
    : []
  const entry = entries.find((e) => boundTrait(e) === lifecycleTrait)
  if (entry === undefined) return undefined
  const property = renames(entry).status ?? "status"
  const props = definition.properties as
    Record<string, Record<string, Json>> | undefined
  const spec = props?.[property]
  if (!spec || typeof spec !== "object") return undefined
  const lists = {
    done: strings(spec.done),
    dropped: strings(spec.dropped),
    proposed: strings(spec.proposed),
  }
  if (spec.type === "state") {
    const moves: Record<string, string[]> = {}
    for (const t of Array.isArray(spec.transitions) ? spec.transitions : []) {
      const { from, to } = (t ?? {}) as { from?: unknown; to?: unknown }
      if (typeof from === "string" && typeof to === "string")
        (moves[from] ??= []).push(to)
    }
    return {
      property,
      values: strings(spec.states),
      ...lists,
      moves,
      initial: typeof spec.initial === "string" ? spec.initial : undefined,
    }
  }
  if (spec.type === "enum")
    return { property, values: strings(spec.values), ...lists }
  return undefined
}

export function standing(
  status: KindStatus,
  value: string | undefined | null
): Standing {
  if (value == null) return "open"
  if (status.done.includes(value)) return "done"
  if (status.dropped.includes(value)) return "dropped"
  if (status.proposed.includes(value)) return "proposed"
  return "open"
}

/** The values a record standing at `value` may move to: a machine's declared
 * moves, or every other value of an enum. */
export function movesFrom(
  status: KindStatus,
  value: string | undefined | null
): string[] {
  if (!status.moves) return status.values.filter((v) => v !== value)
  if (value == null) return []
  return status.moves[value] ?? []
}

/** The first move from `value` to a value standing `to`: accepting a
 * suggestion is its first move to an open value, dismissing it the first to
 * a dropped one, completing a task the first to a done one. */
export function moveTo(
  status: KindStatus,
  value: string | undefined | null,
  to: Standing
): string | undefined {
  return movesFrom(status, value).find((v) => standing(status, v) === to)
}
