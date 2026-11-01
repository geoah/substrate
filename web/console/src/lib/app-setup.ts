/** What an app's approval sets up, in the owner's words
 * (docs/plans/app-setup.md): the lists the approval page renders, one line
 * per setup row, the code a package and each of its requirements run, the
 * account's values, why a row cannot run and what the owner can do about
 * it, how a re-authorization heads itself, and what moved when the server's
 * plan changed under the page. Pure, so the words are tested here. */

import type {
  AuthorizationAccount,
  AuthorizationBlock,
  AuthorizationPackage,
  AuthorizationPrevious,
  AuthorizationRequest,
  AuthorizationScope,
  AuthorizationSetup,
  AuthorizationWebhookPlan,
  SetupAction,
} from "@/lib/api/types"
import { webhookSentence } from "@/lib/app-scopes"
import { displayName, lowerFirst, packageDisplayName } from "@/lib/kind-names"
import { sentence } from "@/lib/tools"

/** Whether approving runs something for this action; `none` is in place. A
 * blocked row keeps the action it would run. */
export function needsAction(action: SetupAction): boolean {
  return (
    action === "install" ||
    action === "upgrade" ||
    action === "create" ||
    action === "enable"
  )
}

/** A package by the console's display name for the last segment of its id:
 * `providers.substrate.reamde.dev/applehealth` reads "Apple Health". The
 * catalog carries no other name. */
export function packageName(id: string): string {
  return packageDisplayName(id.slice(id.lastIndexOf("/") + 1))
}

function kindPackage(kind: string): string {
  return kind.split("/").slice(0, 2).join("/")
}

/** The account's own name: its display property where it has one, else its
 * id. */
export function accountName(account: AuthorizationAccount): string {
  for (const key of ["displayName", "name", "title"]) {
    const value = account.properties?.[key]
    if (typeof value === "string" && value) return value
  }
  return account.id
}

/** "Apple Health account": the package's name and the kind's label. */
function accountWords(account: AuthorizationAccount): string {
  const label = account.label?.singular ?? displayName(account.kind)
  return `${packageName(kindPackage(account.kind))} ${lowerFirst(label)}`
}

/** "Apple Health webhook", by the package its callable belongs to where the
 * server names it. */
function webhookWords(webhook: AuthorizationWebhookPlan): string {
  return webhook.package
    ? `${packageName(webhook.package)} webhook`
    : `${webhook.trigger} webhook`
}

/** One setup row as the line the approval page shows for it: what approving
 * does, or that it is in place already. A blocked row still says what it
 * would do; its problem is shown beside it. */
export function setupTitle(row: AuthorizationSetup): string {
  if (row.package) {
    const name = packageName(row.package.id)
    if (row.action === "none") return `${name} is installed`
    if (row.action === "upgrade") return `Update ${name}`
    return `Install ${name}`
  }
  if (row.account) {
    // An account in place is never written, so the name the app asked for
    // may not be the one it holds.
    if (row.action === "none")
      return `The ${accountWords(row.account)} this app uses is in place`
    return `Create the ${accountWords(row.account)} “${accountName(row.account)}”`
  }
  if (row.webhook) {
    const what = webhookWords(row.webhook)
    return row.action === "none" ? `The ${what} is on` : `Turn on the ${what}`
  }
  return row.type
}

/** The triggers a request leaves on: every webhook it names, enabled by
 * approving or on already. */
export function webhookTriggers(rows: readonly AuthorizationSetup[]): string[] {
  return rows.flatMap((r) => (r.webhook ? [r.webhook.trigger] : []))
}

/** What a package, or one of its requirements, ships of its own. */
type PackageFacts = Pick<
  AuthorizationPackage,
  "kinds" | "functions" | "triggers"
>

/** Whether a package ships code, and whether any of it starts on its own: an
 * enabled trigger that is not a webhook runs once the package is installed,
 * and a webhook trigger the request leaves on runs on every delivery the app
 * sends. `webhooks` are the triggers the request names. */
function codeClause(
  facts: PackageFacts,
  webhooks: readonly string[]
): string | undefined {
  if (!facts.functions?.length) return undefined
  const triggers = facts.triggers ?? []
  if (triggers.some((t) => t.webhook && webhooks.includes(t.id)))
    return "runs code on your server, automatically, each time data arrives"
  return triggers.some((t) => t.enabled && !t.webhook)
    ? "runs code on your server, automatically"
    : "runs code on your server"
}

/** The code a package runs of its own, as a sentence. */
export function codeWords(
  facts: PackageFacts,
  webhooks: readonly string[] = []
): string | undefined {
  const clause = codeClause(facts, webhooks)
  return clause && `${sentence(clause)}.`
}

/** Each package an install brings first, with the code it runs of its own:
 * "Also installs Labs, which runs code on your server, automatically." What
 * is in place already is left out. */
export function chainLines(
  pkg: AuthorizationPackage,
  webhooks: readonly string[] = []
): string[] {
  return (pkg.requires ?? []).flatMap((step) => {
    if (step.action !== "install" && step.action !== "upgrade") return []
    const verb = step.action === "install" ? "installs" : "updates"
    const clause = codeClause(step, webhooks)
    return [
      `Also ${verb} ${packageName(step.id)}${clause ? `, which ${clause}` : ""}.`,
    ]
  })
}

function count(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`
}

/** What a requirement brings of its own, counted: "3 kinds, 1 function, 1
 * trigger"; undefined when it ships none of the three. */
export function factCounts(facts: PackageFacts): string | undefined {
  const parts = [
    facts.kinds?.length ? count(facts.kinds.length, "kind", "kinds") : "",
    facts.functions?.length
      ? count(facts.functions.length, "function", "functions")
      : "",
    facts.triggers?.length
      ? count(facts.triggers.length, "trigger", "triggers")
      : "",
  ].filter(Boolean)
  return parts.length > 0 ? parts.join(", ") : undefined
}

/** The versions an everyday package row names: where an update goes from
 * and to, or what an install brings. A blocked row's problem says its
 * versions itself. */
export function versionWords(row: AuthorizationSetup): string | undefined {
  const pkg = row.package
  if (!pkg || row.blocked || !pkg.shipped) return undefined
  if (row.action === "upgrade" && pkg.held)
    return `From version ${pkg.held} to ${pkg.shipped}`
  if (row.action === "install" || row.action === "upgrade")
    return `Version ${pkg.shipped}`
  return undefined
}

/** What the webhook a row turns on lets the app do. The access comes with
 * the set up, not with a scope it asked for, so it has no checkbox and
 * refusing it is denying the request. `held` is true when the token the app
 * holds carries it already. */
export function grantWords(row: AuthorizationSetup, held: boolean): string {
  const webhook = row.webhook
  if (!webhook) return ""
  const power = lowerFirst(
    webhookSentence(
      webhook.trigger,
      webhook.package ? packageName(webhook.package) : undefined
    )
  )
  if (held) return `The app can already ${power}.`
  const when =
    row.action === "none" ? "will be able to" : "will then be able to"
  return `The app ${when} ${power}. To refuse this, deny the request.`
}

/** Text the server wrote, as a sentence: its first letter up and its end
 * stopped. */
export function asSentence(text: string): string {
  const t = sentence(text)
  return !t || /[.!?…]$/.test(t) ? t : `${t}.`
}

/** What can clear a block: the owner, in the console; a newer server; time;
 * or nothing the owner does, since only the app or the server's catalog can
 * change what is asked or offered. */
export type BlockRemedy = "console" | "server" | "wait" | "elsewhere"

export function blockRemedy(block: AuthorizationBlock): BlockRemedy {
  switch (block.reason) {
    case "sample":
    case "quarantined":
    case "declared-by-owner":
    case "upgrade-needs-review":
      return "console"
    case "server-update":
      return "server"
    case "account-deleted":
      return "wait"
    case "not-shipped":
    case "not-requested":
    case "unsupported":
      return "elsewhere"
    default:
      // A reason newer than this console: the server's link is the one
      // thing that says the owner can act on it.
      return consolePath(block.fix) ? "console" : "elsewhere"
  }
}

const BLOCK_STATUS: Record<BlockRemedy, string> = {
  console: "Needs a fix first",
  server: "Needs a server update",
  wait: "Can’t be done yet",
  elsewhere: "Can’t be done",
}

/** The word beside a blocked row's title. */
export function blockStatus(block: AuthorizationBlock): string {
  return BLOCK_STATUS[blockRemedy(block)]
}

/** What the owner can do while something cannot be set up, said once below
 * the buttons: fix it in the console, have the server updated, wait, or
 * deny, by the rows' reasons. */
export function blockedWords(blocks: readonly AuthorizationBlock[]): string {
  const remedies = blocks.map(blockRemedy)
  const fixes = remedies.filter((r) => r === "console").length
  const link =
    fixes > 1 ? "the links beside the rows" : "the link beside the row"
  if (remedies.includes("elsewhere"))
    return "This can’t be fixed from here: it takes a change to the app or to your server. Deny the request, or check again once one has changed."
  if (remedies.includes("server"))
    return fixes > 0
      ? `Your server needs an update, and the rest can be fixed in the console (${link}), before this can be approved. Check again afterwards, or deny the request.`
      : "Your server needs an update before this can be approved. Whoever runs it can update it; check again afterwards, or deny the request."
  if (fixes > 0)
    return `Fix it in the console (${link}), then check again, or deny the request.`
  return "This can be approved once what is in the way has cleared. Check again later, or deny the request."
}

/** A property name as a label: `displayName` → "Display name". */
export function propertyLabel(key: string): string {
  const spaced = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[-_]+/g, " ")
    .toLowerCase()
  return spaced ? spaced[0].toUpperCase() + spaced.slice(1) : key
}

/** A property value as text: every value an account carries is shown, so
 * nothing is left out for being a list or an object. */
export function propertyValue(value: unknown): string {
  if (typeof value === "string") return value
  if (typeof value === "boolean") return value ? "Yes" : "No"
  if (typeof value === "number") return String(value)
  if (value === null || value === undefined) return "None"
  if (Array.isArray(value) && value.every((v) => typeof v !== "object"))
    return value.map((v) => propertyValue(v)).join(", ")
  return JSON.stringify(value)
}

/** Whether the app's newest token still opens the repository: one that
 * ended holds nothing, whatever scopes it carried. */
export function stillHeld(
  previous: AuthorizationPrevious,
  now: number = Date.now()
): boolean {
  return !previous.expiresAt || Date.parse(previous.expiresAt) > now
}

/** The scopes the app asks for that the token it holds does not cover, and
 * the ones it does, as the server's coverage says. */
export function splitByPrevious(scopes: readonly AuthorizationScope[]): {
  fresh: AuthorizationScope[]
  allowed: AuthorizationScope[]
} {
  return {
    fresh: scopes.filter((s) => !s.allowed),
    allowed: scopes.filter((s) => s.allowed),
  }
}

/** A re-authorization's heading, by what changed since the token the app
 * holds: nothing (a renewal), more access, setup to run, or both. */
export function reauthorizationHeading(
  newAccess: number,
  setupToDo: number
): string {
  if (newAccess > 0 && setupToDo > 0)
    return "Allow more access and update server setup"
  if (newAccess > 0) return "Allow more access"
  if (setupToDo > 0) return "Update server setup"
  return "Renew access"
}

/** One setup row where the page lists it. `grantHeld` is set on a webhook
 * row: whether the token the app holds carries the access it brings, as the
 * server's coverage says. */
export interface SetupLine {
  row: AuthorizationSetup
  index: number
  grantHeld?: boolean
}

/** A request as the approval page lays it out. */
export interface RequestFold {
  /** The app asked before, and the newest token it holds still opens the
   * repository: what that token covers folds away as already allowed. */
  reauth: boolean
  live: boolean
  /** The scopes listed with their checkbox, and the ones folded away. */
  fresh: AuthorizationScope[]
  allowed: AuthorizationScope[]
  /** The setup rows listed, and the ones folded away: in place, with any
   * access they bring held already. */
  setup: SetupLine[]
  folded: SetupLine[]
  /** Scopes and webhook access the token the app holds does not carry. */
  newAccess: number
  /** Rows approving runs, a blocked one included. */
  toDo: number
  /** Why rows cannot run; approving waits until there are none. */
  blocks: AuthorizationBlock[]
  /** Whether what the app does, or what approving sets up, writes under the
   * app's name and so sets off the owner's automations. */
  writes: boolean
  /** The row a stopped set up stopped at, where it stopped at one. */
  failedRow?: AuthorizationSetup
}

/** The lists the approval page renders from one read of the request. Only
 * access the app still holds folds away; a token that ended holds nothing,
 * so everything is asked for in full. */
export function foldRequest(
  request: AuthorizationRequest,
  now: number = Date.now()
): RequestFold {
  const previous = request.previous
  const live = previous !== undefined && stillHeld(previous, now)
  const split = splitByPrevious(request.scopes)
  const lines: SetupLine[] = request.setup.map((row, index) =>
    row.webhook
      ? { row, index, grantHeld: row.webhook.allowed === true }
      : { row, index }
  )
  const inPlace = (line: SetupLine) =>
    line.row.action === "none" && line.grantHeld !== false
  const toDo = request.setup.filter((r) => needsAction(r.action)).length
  const failure = request.state === "pending" ? request.failure : undefined
  return {
    reauth: previous !== undefined,
    live,
    fresh: live ? split.fresh : [...request.scopes],
    allowed: live ? split.allowed : [],
    setup: live ? lines.filter((l) => !inPlace(l)) : lines,
    folded: live ? lines.filter(inPlace) : [],
    newAccess:
      split.fresh.length + lines.filter((l) => l.grantHeld === false).length,
    toDo,
    blocks: request.setup.flatMap((r) => (r.blocked ? [r.blocked] : [])),
    // Talking to an agent writes the app's messages, and deciding writes
    // the decision, both under the app's name; what a setup writes, and
    // what a delivery's function writes, set off the same automations.
    writes:
      request.scopes.some((s) => s.verb !== "read") ||
      toDo > 0 ||
      lines.some((l) => l.row.webhook),
    failedRow:
      failure?.row !== undefined ? request.setup[failure.row] : undefined,
  }
}

/** What moved between the plan the page showed and the server's own: each
 * row whose action or block changed, as it reads now. Empty when only the
 * facts behind a row moved (a version, a description). */
export function planChanges(
  before: readonly AuthorizationSetup[],
  after: readonly AuthorizationSetup[]
): string[] {
  return after.flatMap((row, i) => {
    const was = before[i]
    if (
      was &&
      was.action === row.action &&
      was.blocked?.reason === row.blocked?.reason
    )
      return []
    const title = setupTitle(row)
    if (!row.blocked) return [title]
    const problem = asSentence(row.blocked.problem)
    return [`${title}: can’t be done now${problem ? `. ${problem}` : "."}`]
  })
}

/** How far a set up got, for the page's live region: the row it is on, or
 * how many of the rows it runs are done. */
export function progressWords(
  rows: readonly AuthorizationSetup[]
): string | undefined {
  const runs = rows.filter((r) => needsAction(r.action) && r.progress)
  if (runs.length === 0) return undefined
  const done = runs.filter((r) => r.progress === "done").length
  const at = runs.find((r) => r.progress === "running")
  if (at) return `${setupTitle(at)}: step ${done + 1} of ${runs.length}`
  return `${done} of ${runs.length} done`
}

/** The one line under the buttons once a set up stopped: where, and what
 * Try again does. A stop at no row (waiting for the repository's turn,
 * issuing the code) has no row to carry its message, so the line does. */
export function failureWords(
  failedRow: AuthorizationSetup | undefined
): string {
  if (!failedRow) return "Setting up stopped."
  return `Setting up stopped at ${setupTitle(failedRow)}. What landed stays; Try again runs the rest.`
}

const MONTHS = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
]

/** A day in the owner's words, local time: "1 December", the year added
 * once it is not this one. */
export function dayWords(at: Date, now: Date = new Date()): string {
  const day = `${at.getDate()} ${MONTHS[at.getMonth()]}`
  return at.getFullYear() === now.getFullYear()
    ? day
    : `${day} ${at.getFullYear()}`
}

/** The day access picked today for `days` ends; undefined for 0, until
 * revoked. */
export function lifetimeEnd(
  days: number,
  now: Date = new Date()
): string | undefined {
  if (days === 0) return undefined
  return dayWords(new Date(now.getTime() + days * 86_400_000), now)
}

/** A console path a blocked row links to; anything else is not followed. */
export function consolePath(fix: string | undefined): string | undefined {
  return fix && /^\/(?![/\\])/.test(fix) ? fix : undefined
}
