/** The RECORD PATH: `<kind>/<id>`, one flat string, the whole stored value of a
 * `reference`-typed property (`core.substrate.reamde.dev/llmprovider/claude`,
 * or `task/abc123` for a repository-local kind).
 *
 * This is the console's copy of `internal/vocabulary`'s `SplitRecordPath` /
 * `RecordPath`, and it must stay a mirror of it: the two sides read the same
 * authored value, so a split that disagreed would linkify, graph and submit
 * something the substrate does not mean. */

/** A record path's two halves: the referent's kind REFERENCE and its id. */
export interface RecordPathParts {
  kind: string
  id: string
}

/** Render a record path from its parts. */
export function recordPath(kind: string, id: string): string {
  return `${kind}/${id}`
}

/** Split a record path into its kind reference and its id, or `undefined` when
 * the string is not a path at all.
 *
 * The split rests on the KIND GRAMMAR and on nothing else, so it is
 * deterministic WITHOUT the registry: an authority always carries a dot and a
 * kind NAME never does. So the FIRST segment decides — with a dot it is an
 * authority and the kind is segments one and two, without one it is a
 * repository-local kind and the kind is segment one.
 *
 * The id is EVERYTHING after the kind, slashes included: a DECLARATION
 * record's id is itself a kind reference, so
 * `core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task` is one
 * four-segment path naming one record.
 *
 * A string that is not a path answers `undefined`, which is how an AUTHORED
 * bare id is told from a full path: a declaration id like
 * `tasks.substrate.reamde.dev/task` has a dotted first segment and nothing
 * left after its kind, so it fails here and the reader completes it from the
 * declaration's pin. */
export function splitRecordPath(path: string): RecordPathParts | undefined {
  const slash = path.indexOf("/")
  if (slash <= 0) return undefined
  const first = path.slice(0, slash)
  const rest = path.slice(slash + 1)
  if (!rest) return undefined
  if (!first.includes(".")) return { kind: first, id: rest }
  const next = rest.indexOf("/")
  if (next <= 0) return undefined
  const remainder = rest.slice(next + 1)
  if (!remainder) return undefined
  return { kind: `${first}/${rest.slice(0, next)}`, id: remainder }
}
