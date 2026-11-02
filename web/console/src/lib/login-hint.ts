/** An app's `login_hint`: the repository it wants signed in, which the server
 * passes from `/oauth/authorize` to the approval page and the approval page
 * passes to the sign-in. It only fills the repository field, so anything that
 * is not a valid repository name is dropped, never shown. The rule mirrors the
 * server's `vocabulary.ValidRepositoryAuthority`: dot-separated lowercase DNS
 * labels, each at most 63 characters, 253 in all. */

const AUTHORITY =
  /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/

export function loginHint(value: unknown): string | undefined {
  if (typeof value !== "string") return undefined
  const name = value.trim().toLowerCase()
  if (name.length > 253 || !AUTHORITY.test(name)) return undefined
  if (name.split(".").some((label) => label.length > 63)) return undefined
  return name
}
