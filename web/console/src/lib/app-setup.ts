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
  AuthorizationPackagePublisher,
  AuthorizationPackageStep,
  AuthorizationPrevious,
  AuthorizationProviderInput,
  AuthorizationReach,
  AuthorizationRequest,
  AuthorizationScope,
  AuthorizationSetup,
  AuthorizationWebhookPlan,
  SetupAction,
} from "@/lib/api/types"
import { capWords } from "@/lib/agent-runs"
import { webhookSentence } from "@/lib/app-scopes"
import {
  displayName,
  displayPlural,
  lowerFirst,
  packageDisplayName,
} from "@/lib/kind-names"
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
    // An add-on whose provider is not here yet (decision 0216) names what
    // it waits for.
    if (row.action === "wait")
      return `${name} waits for ${
        row.package.addonFor
          ? packageName(row.package.addonFor)
          : "its provider"
      }`
    if (row.action === "none") return `${name} is installed`
    if (row.action === "upgrade") return `Update ${name}`
    // An installed package the approval still binds or turns on (its
    // agents' AI provider, decision 0217) is set up, not installed again.
    if (row.action === "enable") return `Set up ${name}`
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

/** Who publishes a document-published package, as a person reads it: the
 * document's name for the publisher with its authority, or the authority
 * alone where the document names none. */
export function publisherWords(
  publisher: AuthorizationPackagePublisher
): string {
  return publisher.name
    ? `${publisher.name} (${publisher.authority})`
    : publisher.authority
}

/** The everyday line of a package row an authority document publishes
 * (decision 0214): what approving installs or updates to, and who publishes
 * it. "Installs Training 3, published by Substrate (substrate.reamde.dev)".
 * Undefined for a package this server's catalog ships, whose versions
 * `versionWords` says. */
export function publishedWords(row: AuthorizationSetup): string | undefined {
  const pkg = row.package
  if (!pkg?.publisher) return undefined
  const who = `published by ${publisherWords(pkg.publisher)}`
  const name = packageName(pkg.id)
  if (row.blocked || row.action === "none" || !pkg.shipped)
    return `Published by ${publisherWords(pkg.publisher)}`
  if (row.action === "upgrade" && pkg.held)
    return `Updates ${name} from version ${pkg.held} to ${pkg.shipped}, ${who}`
  return `Installs ${name} ${pkg.shipped}, ${who}`
}

/** An AI provider input as the approval page names it: the package's name,
 * and its input's word only where the package has more than one, since a
 * lone input needs no second name. "Training Coach", "Training Coach
 * (judge)". */
export function providerInputName(
  input: AuthorizationProviderInput,
  inputs: readonly AuthorizationProviderInput[]
): string {
  const name = packageName(input.package)
  const siblings = inputs.filter((i) => i.package === input.package)
  return siblings.length > 1 ? `${name} (${input.input})` : name
}

/** The line under the buttons while an AI provider is still to be chosen:
 * which inputs need one, so a dead Approve says why. */
export function providerNeedWords(
  missing: readonly AuthorizationProviderInput[],
  all: readonly AuthorizationProviderInput[]
): string {
  return `Choose an AI provider for ${joinWords(
    missing.map((input) => providerInputName(input, all))
  )}.`
}

/** What an agent may spend in a day, in the console's money words: "up to
 * $1.00 a day". */
export function spendWords(cents: number): string {
  return `up to ${capWords(cents)} a day`
}

/** The console page a package is paused or removed from: what the server
 * names as a block's fix for one it offers, the provider page. */
export function packagePage(id: string): string {
  const [authority, name] = id.split("/")
  return `/providers/${encodeURIComponent(authority)}/${encodeURIComponent(name ?? "")}`
}

/** What becomes of a package approving installs or updates once the app is
 * gone (decision 0202): it stays, and the owner pauses or removes it from
 * its own page. Undefined for a row in place or blocked, which installs
 * nothing. */
export function keptWords(row: AuthorizationSetup): string | undefined {
  if (!row.package || row.blocked) return undefined
  if (row.action !== "install" && row.action !== "upgrade") return undefined
  return `${packageName(row.package.id)} stays on your server after you revoke the app. Pause or remove it from its page in the console.`
}

/** The triggers a request leaves on: every webhook it names, enabled by
 * approving or on already. */
export function webhookTriggers(rows: readonly AuthorizationSetup[]): string[] {
  return rows.flatMap((r) => (r.webhook ? [r.webhook.trigger] : []))
}

/** What a package, or one of its requirements, ships of its own. */
type PackageFacts = Pick<
  AuthorizationPackage,
  "kinds" | "functions" | "triggers" | "reach"
>

/** A list as a sentence names it: "a", "a and b", "a, b and c". */
function joinWords(items: readonly string[]): string {
  if (items.length <= 1) return items.join("")
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`
}

/** The hosts a package's code declares, every function's together, in
 * declaration order and once each. */
export function declaredHosts(facts: PackageFacts): string[] {
  const out: string[] = []
  for (const f of facts.functions ?? [])
    for (const host of f.network ?? []) if (!out.includes(host)) out.push(host)
  return out
}

/** The hosts a package's sign-in goes through (decision 0220): its own OAuth
 * endpoints' and those of each package it brings first, once each. */
export function oauthHosts(pkg: AuthorizationPackage): string[] {
  const out: string[] = []
  for (const facts of [pkg, ...(pkg.requires ?? [])])
    for (const host of facts.oauthHosts ?? [])
      if (!out.includes(host)) out.push(host)
  return out
}

/** Where a package's sign-in goes, as a sentence; undefined when it declares
 * no OAuth endpoint. An add-on's endpoints may be another provider's, so
 * the hosts are named, never assumed to be the publisher's. */
export function oauthHostWords(pkg: AuthorizationPackage): string | undefined {
  const hosts = oauthHosts(pkg)
  if (hosts.length === 0) return undefined
  return `Signing in to it goes through ${joinWords(hosts)}.`
}

/** Whether a package's code reaches the internet, as a sentence: the hosts
 * it names, or that it has no network access; undefined when the package
 * ships no code. */
export function networkWords(facts: PackageFacts): string | undefined {
  if (!facts.functions?.length) return undefined
  const hosts = declaredHosts(facts)
  if (hosts.length === 0) return "Its code has no internet access."
  return `Its code can send data to the internet; it names ${joinWords(hosts)}.`
}

/** One reach in the owner's words: a kind by its plural label, a package
 * glob as everything of that package, an authority glob as everything it
 * publishes, and `*` as everything. */
export function reachName(pattern: string): string {
  if (pattern === "*") return "everything"
  if (pattern.endsWith("/*")) {
    const scope = pattern.slice(0, -2)
    return scope.includes("/")
      ? `every ${packageName(scope)} kind`
      : `everything from ${scope}`
  }
  return lowerFirst(displayPlural(pattern))
}

/** What a package's code reads and changes, as one sentence, the reach no
 * scope left checked covers marked; undefined when it reaches nothing. */
export function reachWords(facts: PackageFacts): string | undefined {
  const reach = facts.reach ?? []
  if (reach.length === 0) return undefined
  const name = (r: AuthorizationReach) =>
    r.covered ? reachName(r.pattern) : `${reachName(r.pattern)} (not allowed)`
  const writes = reach.filter((r) => r.write)
  // A read of what it also changes says nothing more.
  const reads = reach.filter(
    (r) => !r.write && !writes.some((w) => w.pattern === r.pattern)
  )
  const parts = [
    writes.length > 0 ? `changes ${joinWords(writes.map(name))}` : "",
    reads.length > 0 ? `reads ${joinWords(reads.map(name))}` : "",
  ].filter(Boolean)
  const covered = reach.every((r) => r.covered)
  return `Its code ${parts.join(" and ")}${covered ? ", all within the access you allow above." : "."}`
}

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

/** Each package an install brings first, with who publishes it where a
 * document does and the code it runs of its own: "Also installs Labs, which
 * runs code on your server, automatically." What is in place already is
 * left out. */
export function chainLines(
  pkg: AuthorizationPackage,
  webhooks: readonly string[] = []
): string[] {
  return (pkg.requires ?? []).flatMap((step) => {
    if (step.action !== "install" && step.action !== "upgrade") return []
    const verb = step.action === "install" ? "installs" : "updates"
    const clause = codeClause(step, webhooks)
    return [
      `Also ${verb} ${stepName(step)}${clause ? `, which ${clause}` : ""}.`,
    ]
  })
}

/** A requirement by name, with its publisher where a document publishes
 * it: "Training 3, published by Substrate (substrate.reamde.dev)". */
function stepName(step: AuthorizationPackageStep): string {
  const name = packageName(step.id)
  if (!step.publisher) return name
  const version = step.shipped ? ` ${step.shipped}` : ""
  return `${name}${version}, published by ${publisherWords(step.publisher)}`
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
 * versions itself, and a document-published row's `publishedWords` names
 * them with the publisher. */
export function versionWords(row: AuthorizationSetup): string | undefined {
  const pkg = row.package
  if (!pkg || row.blocked || !pkg.shipped || pkg.publisher) return undefined
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

/** What can clear a block: the owner, in the console; the owner, on this
 * page, by leaving the access the code needs checked; a newer server; time;
 * or nothing the owner does, since only the app or the server's catalog can
 * change what is asked or offered. */
export type BlockRemedy = "console" | "scopes" | "server" | "wait" | "elsewhere"

export function blockRemedy(block: AuthorizationBlock): BlockRemedy {
  switch (block.reason) {
    case "sample":
    case "quarantined":
    case "declared-by-owner":
    case "upgrade-needs-review":
    case "no-model": // a provider's pricing, on the providers page it links to
    case "needs-setting": // an add-on's settings, on its page (decision 0216)
      return "console"
    case "reaches-past-scopes":
      return "scopes"
    case "server-update":
      return "server"
    case "account-deleted":
      return "wait"
    case "not-shipped":
    case "not-published":
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
  scopes: "Needs more access",
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
  if (remedies.includes("scopes"))
    return fixes > 0
      ? `Its code needs access that is unchecked above, or that the app did not ask for, and the rest can be fixed in the console (${link}). Allow what you can, then check again, or deny the request.`
      : "Its code needs access that is unchecked above, or that the app did not ask for. Allow it above where you can; otherwise the app has to ask for it. Or deny the request."
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

/** A setting's label from the last segment of its id: `example.com/addon/
 * apiKey` reads "Api key". */
export function settingLabel(id: string): string {
  return propertyLabel(id.slice(id.lastIndexOf("/") + 1))
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
