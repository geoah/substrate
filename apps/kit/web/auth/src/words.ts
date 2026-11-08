import {
  blockServerUpdate,
  setupActionsNeedingApproval,
  type AuthorizationBlock,
  type AuthorizationCheckResult,
  type AuthorizationDetail,
  type AuthorizationSetup,
} from "@substrate/client"
import { packageName, type AppManifest } from "./manifest"

/** One scope in the console's everyday words. The scope itself is shown
 * beside it wherever identity matters. */
export function scopeWords(scope: string): string {
  const colon = scope.indexOf(":")
  const verb = colon < 0 ? scope : scope.slice(0, colon)
  const pattern = colon < 0 ? "" : scope.slice(colon + 1)
  const glob = pattern.endsWith("*")
  switch (verb) {
    case "read":
    case "write": {
      const action = verb === "read" ? "See" : "See, add, change and delete"
      const later = glob ? ", including kinds added later" : ""
      if (pattern === "*") return `${action} all your records${later}`
      const parts = pattern.split("/")
      if (parts.length === 3 && parts[2] === "*")
        return `${action} your records of the ${parts[1]} package${later}`
      if (parts.length === 3 && !glob)
        return `${action} your ${parts[2]} records`
      return `${action} your records matching ${pattern}${later}`
    }
    case "agents":
      return pattern === "*"
        ? "Talk to all your agents, including agents added later"
        : `Talk to ${pattern}`
    case "decide":
      return "Apply or dismiss the changes your agents suggest, as you"
    case "call":
      return glob
        ? "Run your functions, including functions added later"
        : `Run ${pattern}`
    case "webhook":
      return `Send data to your substrate through the ${pattern} webhook`
    case "full":
      return "Everything in your repository, as you"
    default:
      return scope
  }
}

/** One thing the owner's approval would change, in the console's words. */
export interface Change {
  kind: "scope" | "install" | "upgrade" | "account" | "webhook"
  /** What approving does: "Install Tasks". */
  words: string
  /** A fact beside it: "Version 2 to 3", or a blocked row's problem. */
  detail?: string
  /** Why the approval page cannot do it as things stand. */
  block?: AuthorizationBlock
}

/** The changes a check found against what the app holds, and the package
 * rows only a newer server can set up. `none` is in place, `unknown` is out
 * of the token's sight, and an action the kit does not know asks nothing. */
export function changesFrom(
  manifest: AppManifest,
  result: AuthorizationCheckResult
): { changes: Change[]; serverBlocked: Change[] } {
  const changes: Change[] = (result.scopes?.missing ?? []).map((s) => ({
    kind: "scope",
    words: scopeWords(s),
    detail: s,
  }))
  const serverBlocked: Change[] = []
  result.setup?.forEach((row, index) => {
    const detail = manifest.details[index] ?? { type: row.type }
    if (row.blocked) {
      if (row.blocked.reason === blockServerUpdate && row.package) {
        const name = packageName(manifest, row.package.id)
        const upgrade = row.action === "upgrade"
        serverBlocked.push({
          kind: upgrade ? "upgrade" : "install",
          words: upgrade
            ? `Update ${name} to version ${row.package.required} or later`
            : `Install ${name} version ${row.package.required} or later`,
          detail: row.blocked.problem || undefined,
          block: row.blocked,
        })
        return
      }
      const change = setupChange(manifest, detail, row)
      if (change)
        changes.push({
          ...change,
          detail: row.blocked.problem || change.detail,
          block: row.blocked,
        })
      return
    }
    if (
      (setupActionsNeedingApproval as readonly string[]).includes(row.action)
    ) {
      const change = setupChange(manifest, detail, row)
      if (change) changes.push(change)
    }
  })
  return { changes, serverBlocked }
}

function setupChange(
  manifest: AppManifest,
  detail: AuthorizationDetail | { type: string },
  row: AuthorizationSetup
): Change | undefined {
  const d = detail as AuthorizationDetail
  switch (row.type) {
    case "package": {
      const id = row.package?.id ?? d.package
      if (!id) return undefined
      const name = packageName(manifest, id)
      const p = row.package
      if (row.action === "upgrade" && p) {
        const to = p.shipped || p.required
        const from = `Version ${p.held ?? 0} to ${to}`
        return {
          kind: "upgrade",
          words: `Update ${name}`,
          detail:
            p.shipped && p.shipped !== p.required
              ? `${from} (needs ${p.required} or later)`
              : from,
        }
      }
      if (row.action === "enable")
        return { kind: "install", words: `Set up ${name}` }
      return {
        kind: "install",
        words: `Install ${name}`,
        detail: p?.shipped ? `Version ${p.shipped}` : undefined,
      }
    }
    case "account": {
      const kind = row.account?.kind ?? d.kind
      if (!kind) return undefined
      return {
        kind: "account",
        words: `Create the ${kind} account`,
        detail: kind,
      }
    }
    case "webhook": {
      const trigger = row.webhook?.trigger ?? d.trigger
      if (!trigger) return undefined
      return { kind: "webhook", words: `Turn on the ${trigger} webhook` }
    }
    default:
      return undefined
  }
}
