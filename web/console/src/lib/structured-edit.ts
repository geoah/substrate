/** Edits inside a structured property, as new values of the whole property.
 *
 * A write names a top-level property and carries its whole value: there is
 * no write at a nested path. So every edit below a property is a copy of the
 * value the editor opened on with one thing changed, and everything it did
 * not touch (a sibling item, a field nobody declared, an empty list written
 * on purpose, the exact spelling of a stored number) goes back as it was
 * read. Nothing here re-serializes a value through a form. */

/** Where a value sits below its property: field names and list indices. */
export type Path = readonly (string | number)[]

/** A stable string for a path, for keys and focus. */
export function pathKey(property: string, path: Path): string {
  return [property, ...path].join("/")
}

function isBag(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

/** The value at `path`, or undefined where nothing is. */
export function getIn(root: unknown, path: Path): unknown {
  let at: unknown = root
  for (const step of path) {
    if (typeof step === "number") {
      if (!Array.isArray(at)) return undefined
      at = at[step]
    } else {
      if (!isBag(at)) return undefined
      at = at[step]
    }
  }
  return at
}

/** `root` with `value` at `path`. Each container on the way is copied, never
 * changed in place; an object missing on the way is made. An index past a
 * list's end is refused, because it would name an item nobody holds. */
export function setIn(root: unknown, path: Path, value: unknown): unknown {
  if (!path.length) return value
  const [step, ...rest] = path
  if (typeof step === "number") {
    if (!Array.isArray(root) || step < 0 || step >= root.length) {
      throw new RangeError(`no item ${step + 1} to write`)
    }
    const out = [...(root as unknown[])]
    out[step] = setIn(out[step], rest, value)
    return out
  }
  const bag = isBag(root) ? root : {}
  return { ...bag, [step]: setIn(bag[step], rest, value) }
}

/** `root` without the field at `path`: the field is taken out of its object,
 * never written as null or zero. */
export function unsetIn(root: unknown, path: Path): unknown {
  if (!path.length) return undefined
  const [step, ...rest] = path
  if (typeof step === "number") {
    if (!Array.isArray(root) || step < 0 || step >= root.length) return root
    if (!rest.length) return root.filter((_, i) => i !== step)
    const list = root as unknown[]
    const inner = unsetIn(list[step], rest)
    if (inner === list[step]) return root
    const out = [...list]
    out[step] = inner
    return out
  }
  if (!isBag(root) || !(step in root)) return root
  if (!rest.length) {
    const out = { ...root }
    delete out[step]
    return out
  }
  const inner = unsetIn(root[step], rest)
  return inner === root[step] ? root : { ...root, [step]: inner }
}

/** The list at `path` as stored, or an empty one where none is. */
function listAt(root: unknown, path: Path): unknown[] {
  const list = getIn(root, path)
  return Array.isArray(list) ? list : []
}

/** `root` with `item` inserted into the list at `path` before `index`. A
 * list nobody holds yet is made. */
export function insertAt(
  root: unknown,
  path: Path,
  index: number,
  item: unknown
): unknown {
  const list = listAt(root, path)
  const at = Math.max(0, Math.min(index, list.length))
  return setIn(root, path, [...list.slice(0, at), item, ...list.slice(at)])
}

/** `root` without the item at `index` of the list at `path`. An emptied
 * list stays an empty list: written that way it says "none", where taking
 * the property out would let its sources fill it again. */
export function removeAt(root: unknown, path: Path, index: number): unknown {
  const list = listAt(root, path)
  if (index < 0 || index >= list.length) return root
  return setIn(
    root,
    path,
    list.filter((_, i) => i !== index)
  )
}

/** `root` with the item at `from` moved to `to` in the list at `path`. */
export function moveAt(
  root: unknown,
  path: Path,
  from: number,
  to: number
): unknown {
  const list = listAt(root, path)
  if (from < 0 || from >= list.length || to < 0 || to >= list.length) {
    return root
  }
  if (from === to) return root
  const out = [...list]
  const [moved] = out.splice(from, 1)
  out.splice(to, 0, moved)
  return setIn(root, path, out)
}

/** A deep copy of a stored value, for an item duplicated before it is
 * written. */
export function copyOf<T>(value: T): T {
  return structuredClone(value)
}
