import type { AuthorizationDetail, ServerRequirement } from "@substrate/client"

export type { AuthorizationDetail }

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
  /** The server the app needs. Absent, it is read from the scopes and the
   * details (`requirementOf`). */
  requirement?: ServerRequirement
  /** What a person calls each package the app asks for, by id: "Tasks".
   * Absent, a package is called by the last segment of its id. */
  packageNames?: Record<string, string>
}

/** The manifest of an app served from `base` on this page's origin: its
 * client document at `<base>client.json`, its callback the base itself. A
 * loopback origin makes it a local app, whose document is never fetched.
 * Absent, the base is the page's folder (`appBase`); an app that routes in
 * its path, not its hash, names it (`import.meta.env.BASE_URL`). */
export function webManifest(
  input: Omit<AppManifest, "clientID" | "redirectURI"> & { base?: string }
): AppManifest {
  const { base: at, ...rest } = input
  const base = at ? new URL(at, window.location.href) : appBase()
  return {
    ...rest,
    clientID: new URL("client.json", base).toString(),
    redirectURI: base.toString(),
  }
}

/** The folder a page is served from, ending in `/`. A last segment with no
 * dot is a folder opened without its slash (`/substrate/tasks`), never a
 * file beside the app, so the client id is `/substrate/tasks/client.json`
 * and not `/substrate/client.json`. */
export function appBase(location: Location | URL = window.location): URL {
  const path = location.pathname
  const last = path.slice(path.lastIndexOf("/") + 1)
  const folder =
    last === ""
      ? path
      : last.includes(".")
        ? path.slice(0, -last.length)
        : `${path}/`
  return new URL(folder, location.origin)
}

/** The server an app needs: what its manifest names, else every scope verb
 * it asks for and every detail type it sends. */
export function requirementOf(manifest: AppManifest): ServerRequirement {
  if (manifest.requirement) return manifest.requirement
  return {
    scopeVerbs: [...new Set(manifest.scopes.map((s) => s.split(":")[0]))],
    detailTypes: [...new Set(manifest.details.map((d) => d.type))],
  }
}

/** A package by what a person calls it: "Tasks" for
 * `substrate.reamde.dev/tasks`, unless the manifest names it. */
export function packageName(manifest: AppManifest, id: string): string {
  const named = manifest.packageNames?.[id]
  if (named) return named
  const last = id.split("/").at(-1) ?? id
  return last.charAt(0).toUpperCase() + last.slice(1)
}
