/** The RECORD PATH: `<authority>/<kind>/<id>`, one flat string, the whole
 * stored value of a `reference`-typed property
 * (`core.substrate.reamde.dev/llmprovider/claude`). Every kind carries an
 * authority (decision 0042), so a stored path is always three-plus segments.
 *
 * This is the console's copy of `internal/vocabulary`'s `SplitRecordPath` /
 * `RecordPath`, and of the write path's `coerceReferencePath`
 * (`internal/engine/validate.go`). It must stay a mirror of both: the two sides
 * read the same authored value, so a split that disagreed would linkify, graph
 * and submit something the substrate does not mean, and a coercion that
 * disagreed would either bar a value the server admits or wave through one it
 * refuses. */

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
 * kind NAME never does. Every kind carries an authority (decision 0042), so the
 * FIRST segment is the authority, the kind is segments one and two, and a
 * dotless first segment is no path at all.
 *
 * The id is EVERYTHING after the kind, slashes included: a DECLARATION
 * record's id is itself a kind reference, so
 * `core.substrate.reamde.dev/kind/tasks.substrate.reamde.dev/task` is one
 * four-segment path naming one record.
 *
 * A string that is not a full path answers `undefined`, which is how an
 * AUTHORED bare id is told from a stored path: a declaration id like
 * `tasks.substrate.reamde.dev/task` has a dotted first segment and nothing
 * left after its kind, so it fails here and the reader completes it from the
 * declaration's pin. */
export function splitRecordPath(path: string): RecordPathParts | undefined {
  const slash = path.indexOf("/")
  if (slash <= 0) return undefined
  const first = path.slice(0, slash)
  const rest = path.slice(slash + 1)
  if (!rest) return undefined
  if (!first.includes(".")) return undefined
  const next = rest.indexOf("/")
  if (next <= 0) return undefined
  const remainder = rest.slice(next + 1)
  if (!remainder) return undefined
  return { kind: `${first}/${rest.slice(0, next)}`, id: remainder }
}

/** Whether a record id has a slash with nothing on one side of it. `target/`,
 * `/x` and `a//b` all name no record, and completing one from a pin would
 * store a path that can never be split back (`engine.hasEmptySegment`). */
function hasEmptySegment(id: string): boolean {
  return (
    id === "" || id.startsWith("/") || id.endsWith("/") || id.includes("//")
  )
}

/** What one authored reference value coerces to, or why it cannot. */
export interface CoercedReference {
  /** The canonical record path the write would carry. */
  value?: string
  error?: string
}

/** Hold one authored string to the reference value model, exactly as the write
 * path holds it (`engine.coerceReferencePath`). ONE decision, so the form, the
 * document validator and the server cannot answer it differently.
 *
 * A full path is left alone. A bare record id is the AUTHORED SHORT FORM, and
 * only a concrete `kind:` pin can supply what it omits.
 *
 * AN AMBIGUOUS VALUE IS REFUSED, NEVER GUESSED. Under a pin, a value that
 * parses as a path whose kind is NOT the pin reads two ways (that pointer, or
 * a bare id the pin would complete) and they name different records, so both
 * readings are named and neither is chosen.
 *
 * A value that does NOT parse as a path is unambiguous even carrying slashes,
 * which is what keeps a kind or function IDENTITY spellable as the short form
 * (`core.substrate.reamde.dev/graphql` under a pin at core's `function`):
 * nothing is left over for an id, so it cannot be read as a path. Only
 * empty-segment shapes are refused there.
 *
 * The refusals are the server's own words, so a value refused here and the
 * same value refused there read as one answer rather than two. */
export function coerceReferencePath(
  pin: string,
  value: string
): CoercedReference {
  if (value === "") return { error: "a reference needs an id" }
  const parts = splitRecordPath(value)
  if (!pin) {
    // Unpinned there is no kind to borrow, so only a full path says what this
    // names.
    if (!parts) {
      return {
        error: `a reference to any kind needs a full "<kind>/<id>" path, and ${JSON.stringify(value)} is not one`,
      }
    }
    if (hasEmptySegment(parts.id)) {
      return {
        error: `reference ${JSON.stringify(value)} has an empty id segment`,
      }
    }
    return { value }
  }
  if (parts) {
    if (parts.kind === pin) {
      if (hasEmptySegment(parts.id)) {
        return {
          error: `reference ${JSON.stringify(value)} has an empty id segment`,
        }
      }
      return { value }
    }
    // Both readings, named, because either end may be the one to change.
    return {
      error: `reference ${JSON.stringify(value)} is ambiguous: it reads as a pointer at ${parts.kind}, or as a bare id the pin would complete to ${JSON.stringify(recordPath(pin, value))}, and the declaration pins ${pin}, so write that path in full`,
    }
  }
  if (hasEmptySegment(value)) {
    return {
      error: `reference ${JSON.stringify(value)} has an empty segment, so it names no record`,
    }
  }
  return { value: recordPath(pin, value) }
}
