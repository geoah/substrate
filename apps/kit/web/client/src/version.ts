import type { AuthorizationServerMetadata } from "./discovery"

/** A substrate build's version as discovery serves it: a release
 * (`v0.113.0`), a build past one as `git describe` names it
 * (`v0.113.0-4-gabc1234`, `-dirty`), a prerelease or a Go pseudo-version
 * (`v0.114.0-pre.1`, `v0.113.1-0.20261031120000-abc`), which come before
 * the release they name. Anything else (`dev`) is not a version: only what
 * such a build can do decides whether an app works with it. The kit's
 * ServerVersion. */
export interface ServerVersion {
  major: number
  minor: number
  patch: number
  /** What follows a release it comes before: `pre.1`, `rc.2`. */
  prerelease?: string
  /** How many commits past the tag the build is, from `git describe`. */
  commitsAfter?: number
}

export function parseServerVersion(
  text: string | undefined
): ServerVersion | undefined {
  if (!text) return undefined
  let rest = text.trim().replace(/^[vV]/, "")
  // Build metadata (`+dirty`, `+incompatible`) never orders.
  rest = rest.split("+")[0]
  const dash = rest.indexOf("-")
  const core = (dash >= 0 ? rest.slice(0, dash) : rest).split(".")
  if (core.length < 2 || core.length > 3) return undefined
  if (!core.every((p) => /^\d+$/.test(p))) return undefined
  const [major, minor, patch = 0] = core.map(Number)
  const v: ServerVersion = { major, minor, patch }
  if (dash < 0) return v
  const suffix = rest.slice(dash + 1)
  if (!suffix) return undefined
  const commits = describedCommits(suffix)
  if (commits !== undefined) v.commitsAfter = commits
  else v.prerelease = suffix
  return v
}

/** `4-gabc1234`, `4-gabc1234-dirty` or `dirty`: commits past the tag. */
function describedCommits(suffix: string): number | undefined {
  if (suffix === "dirty") return 0
  const parts = suffix.split("-")
  if (parts.length !== 2 && !(parts.length === 3 && parts[2] === "dirty"))
    return undefined
  if (!/^\d+$/.test(parts[0]) || !/^g[0-9a-f]+$/i.test(parts[1]))
    return undefined
  return Number(parts[0])
}

export function isRelease(v: ServerVersion): boolean {
  return v.prerelease === undefined && v.commitsAfter === undefined
}

export function formatServerVersion(v: ServerVersion): string {
  let text = `${v.major}.${v.minor}.${v.patch}`
  if (v.prerelease !== undefined) text += `-${v.prerelease}`
  if (v.commitsAfter !== undefined)
    text += v.commitsAfter === 0 ? "-dirty" : `-${v.commitsAfter}`
  return text
}

/** Negative, zero or positive as `a` orders before, with or after `b`:
 * before the release, the release, then past it. */
export function compareServerVersions(
  a: ServerVersion,
  b: ServerVersion
): number {
  const core = a.major - b.major || a.minor - b.minor || a.patch - b.patch
  if (core) return core
  const rank = (v: ServerVersion) =>
    v.prerelease !== undefined ? 0 : v.commitsAfter === undefined ? 1 : 2
  if (rank(a) !== rank(b)) return rank(a) - rank(b)
  if (a.prerelease !== undefined && b.prerelease !== undefined)
    return comparePrereleases(a.prerelease, b.prerelease)
  return (a.commitsAfter ?? 0) - (b.commitsAfter ?? 0)
}

/** Semantic versioning's order: numbers by value and below words, a
 * shorter list first when one prefixes the other. */
function comparePrereleases(a: string, b: string): number {
  const x = a.split(".")
  const y = b.split(".")
  for (let i = 0; i < Math.min(x.length, y.length); i++) {
    if (x[i] === y[i]) continue
    const m = /^\d+$/.test(x[i]) ? Number(x[i]) : undefined
    const n = /^\d+$/.test(y[i]) ? Number(y[i]) : undefined
    if (m !== undefined && n !== undefined) return m - n
    if (m !== undefined) return -1
    if (n !== undefined) return 1
    return x[i] < y[i] ? -1 : 1
  }
  return x.length - y.length
}

/** A version as every screen says it: a release as `0.114.0`, a build
 * before or past one as a build of it, anything else as a development
 * build. */
export function displayServerVersion(text: string | undefined): string {
  const v = parseServerVersion(text)
  if (!v) return "a development build"
  if (isRelease(v)) return formatServerVersion(v)
  return `a build of ${v.major}.${v.minor}.${v.patch}`
}

/** The server an app needs: a release to name to the owner, and what the
 * server must be able to do. What it can do decides: a `dev` build, or one
 * between releases, works when its metadata lists the verbs and types, and
 * a release below the minimum is too old even when it lists them. The
 * kit's ServerRequirement. */
export interface ServerRequirement {
  /** The first release that serves what the app needs, such as `0.113.0`. */
  minimumVersion?: string
  /** The scope verbs the app asks for (`read`, `write`, `agents`), each of
   * which the server's `scopes_supported` must list. */
  scopeVerbs?: string[]
  /** The `authorization_details` types the app sends, each of which the
   * server must list. */
  detailTypes?: string[]
}

export type ServerVerdict =
  | { compatible: true }
  | {
      compatible: false
      /** The version the server reports. */
      running?: string
      /** The minimum the app names. */
      needed?: string
      /** What the server cannot do yet, in words that finish "It can't
       * yet …". */
      missing: string[]
    }

/** The verdict on a server from its version and its OAuth metadata, which
 * is absent when the server has no app authorization at all. */
export function judgeServer(
  requirement: ServerRequirement,
  version: string | undefined,
  metadata: AuthorizationServerMetadata | undefined
): ServerVerdict {
  const missing: string[] = []
  if (metadata) {
    const verbs = new Set(
      (metadata.scopes_supported ?? []).map((s) => s.split(":")[0])
    )
    for (const verb of requirement.scopeVerbs ?? [])
      if (!verbs.has(verb)) missing.push(verbWords(verb))
    const types = metadata.authorization_details_types_supported ?? []
    for (const type of requirement.detailTypes ?? [])
      if (!types.includes(type)) missing.push(detailWords(type))
  } else {
    missing.push("give apps access of their own")
  }
  const minimum = parseServerVersion(requirement.minimumVersion)
  const running = parseServerVersion(version)
  const below =
    !!minimum &&
    !!running &&
    isRelease(running) &&
    compareServerVersions(running, minimum) < 0
  if (!below && !missing.length) return { compatible: true }
  return {
    compatible: false,
    running: version,
    needed: minimum ? formatServerVersion(minimum) : undefined,
    missing,
  }
}

/** What a scope verb lets an app do, as what a server without it cannot. */
function verbWords(verb: string): string {
  switch (verb) {
    case "read":
      return "let apps see your records"
    case "write":
      return "let apps change your records"
    case "agents":
      return "let apps talk to your agents"
    case "decide":
      return "let apps apply what your agents suggest"
    case "call":
      return "let apps run your functions"
    case "webhook":
      return "let apps send data to a webhook"
    case "full":
      return "let apps sign in with full access"
    default:
      return `give apps ${verb} access`
  }
}

/** What an `authorization_details` type sets up on approval. */
function detailWords(type: string): string {
  switch (type) {
    case "package":
      return "install what an app needs when you approve it"
    case "account":
      return "create an app's account when you approve it"
    case "webhook":
      return "turn on an app's webhook when you approve it"
    default:
      return `set up ${type} for an app`
  }
}
