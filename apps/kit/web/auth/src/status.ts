import {
  SubstrateClient,
  SubstrateError,
  describe,
  inspectServer,
  judgeServer,
} from "@substrate/client"
import { packageName, requirementOf, type AppManifest } from "./manifest"
import { hasEnded, type Session } from "./session"
import { changesFrom, type Change } from "./words"

/** Something about an app's access the owner may need to act on. Several
 * can hold at once. */
export type AppIssue =
  /** The approved access ended, or was revoked: only signing in again gets
   * it back. */
  | { type: "accessEnded" }
  /** The server cannot do what the app needs: `running` is its version,
   * `needed` the release the app names, `missing` what it cannot do yet,
   * `blocked` each package only a newer server sets up. */
  | {
      type: "serverTooOld"
      running?: string
      needed?: string
      missing: string[]
      blocked: Change[]
    }
  /** The app's request has grown past what was approved, or what was set up
   * on the server has gone or fallen behind: an approval fixes it. */
  | { type: "needsApproval"; changes: Change[] }
  /** The approved access ends at this time (epoch ms). */
  | { type: "accessEndsSoon"; at: number }
  /** The server could not be asked; the words say why. */
  | { type: "unreachable"; words: string }

/** A package the app needs and where the repository stands with it: the
 * version held, `missing`, or unknown (absent) where the token cannot see
 * it. */
export interface PackageStanding {
  id: string
  name: string
  needs: number
  held?: number | "missing"
}

/** The app's standing with its server, as one check found it. */
export interface AppStatus {
  issues: AppIssue[]
  /** What the server said it runs, where it answered. */
  serverVersion?: string
  packages: PackageStanding[]
  checkedAt: number
}

/** What the owner meets first: an ended access outranks everything, a
 * server update comes before an approval it would block. */
const rank: Record<AppIssue["type"], number> = {
  accessEnded: 0,
  serverTooOld: 1,
  needsApproval: 2,
  accessEndsSoon: 3,
  unreachable: 4,
}

export const defaultEndingWindow = 7 * 86_400_000

/** How far ahead an ending is worth telling the owner about: a quarter of
 * the approved lifetime at most, so access approved for a week is not
 * ending from the moment it was approved. */
export function endingWindow(
  session: Session,
  window = defaultEndingWindow
): number {
  if (!session.expiresAt || session.expiresAt <= session.signedInAt)
    return window
  return Math.min(window, (session.expiresAt - session.signedInAt) / 4)
}

/** The scopes the owner left out of the last approval: the ones its request
 * asked for that the token does not hold. Never a reason to ask again. */
export function leftOut(manifest: AppManifest, session: Session): string[] {
  if (!session.requestedScopes) return []
  const held = new Set(session.scopes)
  const asked = new Set(session.requestedScopes)
  return manifest.scopes.filter((s) => asked.has(s) && !held.has(s))
}

/** Whether an app is still set up with its server, from what the server
 * says now: its discovery and metadata against the app's requirement,
 * `POST /api/v1/authorizations/check` against the app's request (which
 * also records this build's `softwareVersion` on the token), and when the
 * access ends. A row the token cannot see is never a reason to ask the
 * owner anything. The kit's AppStatusCheck. */
export async function checkAppStatus(opts: {
  manifest: AppManifest
  session: Session
  client?: SubstrateClient
  now?: () => number
  window?: number
  signal?: AbortSignal
}): Promise<AppStatus> {
  const { manifest, session, signal } = opts
  const now = opts.now ?? Date.now
  const at = now()
  const packages: PackageStanding[] = manifest.details
    .filter((d) => d.type === "package" && d.package)
    .map((d) => ({
      id: d.package!,
      name: packageName(manifest, d.package!),
      needs: d.version ?? 1,
    }))
  const status = (issues: AppIssue[], serverVersion?: string): AppStatus => ({
    issues: issues.sort((a, b) => rank[a.type] - rank[b.type]),
    serverVersion,
    packages,
    checkedAt: at,
  })
  if (hasEnded(session, at)) return status([{ type: "accessEnded" }])

  const ending = (expiresAt?: number): AppIssue[] => {
    if (expiresAt === undefined) return []
    if (expiresAt <= at) return [{ type: "accessEnded" }]
    return expiresAt - at <= endingWindow(session, opts.window)
      ? [{ type: "accessEndsSoon", at: expiresAt }]
      : []
  }

  const host = new URL(session.server).host
  const { document, metadata } = await inspectServer(session.server, signal)
  if (!document) {
    return status([
      {
        type: "unreachable",
        words: `Couldn't reach ${host}. Check your connection and try again.`,
      },
      ...ending(session.expiresAt),
    ])
  }
  const version = document.server?.version
  const verdict = judgeServer(requirementOf(manifest), version, metadata)
  if (!verdict.compatible) {
    return status(
      [
        {
          type: "serverTooOld",
          running: verdict.running,
          needed: verdict.needed,
          missing: verdict.missing,
          blocked: [],
        },
        ...ending(session.expiresAt),
      ],
      version
    )
  }

  const issues: AppIssue[] = []
  let expiresAt = session.expiresAt
  if (metadata?.authorization_details_types_supported?.length) {
    const client =
      opts.client ?? new SubstrateClient(session.server, session.token)
    const skip = new Set(leftOut(manifest, session))
    try {
      const result = await client.checkAuthorization(
        {
          scope: manifest.scopes.filter((s) => !skip.has(s)).join(" "),
          details: manifest.details,
          softwareVersion: manifest.softwareVersion || undefined,
        },
        signal
      )
      const found = changesFrom(manifest, result)
      if (found.serverBlocked.length)
        issues.push({
          type: "serverTooOld",
          running: version,
          missing: [],
          blocked: found.serverBlocked,
        })
      if (found.changes.length)
        issues.push({ type: "needsApproval", changes: found.changes })
      if (result.expiresAt) expiresAt = Date.parse(result.expiresAt)
      for (const row of result.setup ?? []) {
        const facts = row.package
        if (row.type !== "package" || !facts || row.action === "unknown")
          continue
        const p = packages.find((x) => x.id === facts.id)
        if (!p) continue
        p.held =
          facts.held ?? (row.action === "install" ? "missing" : undefined)
        p.needs = facts.required || p.needs
      }
    } catch (e) {
      if (e instanceof SubstrateError && e.status === 401)
        return status([{ type: "accessEnded" }], version)
      if (signal?.aborted) throw e
      issues.push({ type: "unreachable", words: describe(e) })
    }
  }
  return status([...issues, ...ending(expiresAt)], version)
}
