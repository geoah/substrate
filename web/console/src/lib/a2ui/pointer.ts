/** JSON Pointers (RFC 6901) over an A2UI data model, and the paths a
 * component binds with. A write never mutates: it copies the containers on
 * its way down, so a reader holding the old model (a React snapshot) keeps
 * seeing what it rendered. */

/** The tokens of a pointer: `/a/b~1c` is `["a", "b/c"]`. `""` and `"/"` are
 * both the whole document, as A2UI's `updateDataModel` reads them. */
export function parsePointer(pointer: string): string[] {
  if (pointer === "" || pointer === "/") return []
  const body = pointer.startsWith("/") ? pointer.slice(1) : pointer
  return body
    .split("/")
    .map((token) => token.replace(/~1/g, "/").replace(/~0/g, "~"))
}

/** One token, escaped for a pointer. */
export function escapeToken(token: string): string {
  return token.replace(/~/g, "~0").replace(/\//g, "~1")
}

/** The absolute pointer a binding names. An absolute path stands alone; a
 * relative one is read inside `scope`, the item a template is drawing
 * (`/data/books/3`), and against the root outside one. */
export function resolvePath(path: string, scope?: string): string {
  if (path.startsWith("/")) return path
  const base = scope && scope !== "/" ? scope.replace(/\/+$/, "") : ""
  if (path === "" || path === ".") return base || "/"
  return `${base}/${path}`
}

function isIndex(token: string): boolean {
  return /^(0|[1-9][0-9]*)$/.test(token)
}

/** The value at `pointer`, or undefined when any step is missing. */
export function getAt(doc: unknown, pointer: string): unknown {
  let at: unknown = doc
  for (const token of parsePointer(pointer)) {
    if (Array.isArray(at)) {
      if (!isIndex(token)) return undefined
      at = at[Number(token)]
    } else if (at !== null && typeof at === "object") {
      if (!Object.hasOwn(at, token)) return undefined
      at = (at as Record<string, unknown>)[token]
    } else {
      return undefined
    }
  }
  return at
}

type Container = Record<string, unknown> | unknown[]

function copyOf(value: unknown, nextToken: string): Container {
  if (Array.isArray(value)) return [...(value as unknown[])]
  if (value !== null && typeof value === "object")
    return { ...(value as Record<string, unknown>) }
  // A missing step becomes the container the next token addresses.
  return isIndex(nextToken) || nextToken === "-" ? [] : {}
}

function put(container: Container, token: string, value: unknown) {
  if (Array.isArray(container)) {
    const index = token === "-" ? container.length : Number(token)
    if (!Number.isInteger(index) || index < 0) return
    container[index] = value
  } else {
    container[token] = value
  }
}

/** A copy of `doc` with `value` at `pointer`, creating each missing step.
 * The empty pointer replaces the document. */
export function setAt(doc: unknown, pointer: string, value: unknown): unknown {
  const tokens = parsePointer(pointer)
  if (!tokens.length) return value
  const root = copyOf(doc, tokens[0])
  let parent: Container = root
  let source: unknown = doc
  for (let i = 0; i < tokens.length - 1; i++) {
    const token = tokens[i]
    source = getAt(source, `/${escapeToken(token)}`)
    const next = copyOf(source, tokens[i + 1])
    put(parent, token, next)
    parent = next
  }
  put(parent, tokens[tokens.length - 1], value)
  return root
}

/** A copy of `doc` without the key at `pointer`; an array item is spliced
 * out. A pointer that names nothing leaves the document as it was. The empty
 * pointer removes the document, which reads back as an empty object. */
export function removeAt(doc: unknown, pointer: string): unknown {
  const tokens = parsePointer(pointer)
  if (!tokens.length) return {}
  const parentPointer = tokens
    .slice(0, -1)
    .map((t) => `/${escapeToken(t)}`)
    .join("")
  const parent = getAt(doc, parentPointer)
  const last = tokens[tokens.length - 1]
  if (Array.isArray(parent)) {
    if (!isIndex(last) || Number(last) >= parent.length) return doc
    const next = parent.filter((_, i) => i !== Number(last))
    return setAt(doc, parentPointer, next)
  }
  if (parent === null || typeof parent !== "object") return doc
  if (!Object.hasOwn(parent, last)) return doc
  const next = { ...(parent as Record<string, unknown>) }
  delete next[last]
  return setAt(doc, parentPointer, next)
}

/** Whether a write at `written` can change what `watched` reads: one is the
 * other or contains it. */
export function pathsOverlap(written: string, watched: string): boolean {
  const a = parsePointer(written)
  const b = parsePointer(watched)
  const n = Math.min(a.length, b.length)
  for (let i = 0; i < n; i++) if (a[i] !== b[i]) return false
  return true
}
