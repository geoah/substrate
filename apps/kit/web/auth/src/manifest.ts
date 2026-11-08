/** One entry of an app's `authorization_details` (RFC 9396): a package the
 * app needs at a version or later, installed or upgraded on approval. */
export interface AuthorizationDetail {
  type: "package" | "account" | "webhook"
  package?: string
  version?: number
  kind?: string
  id?: string
  properties?: Record<string, unknown>
  trigger?: string
  url?: string
}

export function packageDetail(
  id: string,
  atLeast: number
): AuthorizationDetail {
  return { type: "package", package: id, version: atLeast }
}

/** A web app, named once: what its app.json says, plus where it is served.
 * Its client id is the URL of its client metadata document, and its
 * redirect is on the same origin (decision 0194). */
export interface AppManifest {
  /** The app's id in app.json, such as `tasks`. */
  id: string
  name: string
  clientID: string
  redirectURI: string
  scopes: string[]
  details: AuthorizationDetail[]
  softwareVersion: string
}

/** The manifest of an app served from `base` on this page's origin: its
 * client document at `<base>client.json`, its callback the base itself. A
 * loopback origin makes it a local app, whose document is never fetched. */
export function webManifest(
  input: Omit<AppManifest, "clientID" | "redirectURI"> & { base?: string }
): AppManifest {
  const base = new URL(input.base ?? "./", window.location.href)
  return {
    ...input,
    clientID: new URL("client.json", base).toString(),
    redirectURI: base.toString(),
  }
}
