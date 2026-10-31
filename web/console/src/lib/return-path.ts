/** Where `/login` and `/register` send a reader once they are signed in. The
 * value arrives in the address (`?redirect=`), so anybody can write it: the
 * door follows it only when it resolves to a path on this console's own
 * origin. A browser reads `\` as `/` and drops tabs and newlines inside a URL
 * before reading it, so a value carrying either (or any whitespace, or an
 * encoded backslash) is refused outright rather than reasoned about, and the
 * caller falls back to Home. */

function hasControlOrSpace(value: string): boolean {
  for (let i = 0; i < value.length; i++) {
    const c = value.charCodeAt(i)
    if (c <= 0x1f || c === 0x7f) return true
  }
  return /\s/.test(value)
}

/** The local path `value` resolves to (its pathname, search and hash on this
 * origin), else undefined. Only a value with exactly one leading `/` is a
 * candidate. */
export function safeReturnPath(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined
  if (!value.startsWith("/") || value.startsWith("//")) return undefined
  if (value.includes("\\") || /%5c/i.test(value)) return undefined
  if (hasControlOrSpace(value)) return undefined
  const origin = window.location.origin
  let resolved: URL
  try {
    resolved = new URL(value, origin)
  } catch {
    return undefined
  }
  if (resolved.origin !== origin) return undefined
  // Dot segments can fold a path into `//host` (`/.//host`), which, handed
  // back as a path, is a protocol-relative URL to another origin.
  if (resolved.pathname.startsWith("//")) return undefined
  return resolved.pathname + resolved.search + resolved.hash
}

/** The path a reader whose session just ended returns to after signing in
 * again: an app's approval page only, because that is a flow somebody else
 * started and cannot resume on their own. Everywhere else lands on Home. */
export function returnAfterExpiry(pathname: string): string | undefined {
  return pathname.startsWith("/authorize/")
    ? safeReturnPath(pathname)
    : undefined
}
