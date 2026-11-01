/** What an app's approval sets up, in the owner's words
 * (docs/plans/app-setup.md): one line per setup row, the code a package
 * runs, the packages an install brings first, the account's values, how a
 * re-authorization heads itself, and what moved when the server's plan
 * changed under the page. Pure, so the words are tested here. */

import type {
  AuthorizationAccount,
  AuthorizationPackage,
  AuthorizationPrevious,
  AuthorizationScope,
  AuthorizationSetup,
  SetupAction,
} from "@/lib/api/types"
import { displayName, lowerFirst, packageDisplayName } from "@/lib/kind-names"
import { listWords } from "@/lib/tools"

/** Whether approving does something for this action; `none` is in place and
 * `blocked` cannot run. */
export function needsAction(action: SetupAction): boolean {
  return (
    action === "install" ||
    action === "upgrade" ||
    action === "create" ||
    action === "arm"
  )
}

function packageWord(id: string): string {
  return id.slice(id.lastIndexOf("/") + 1)
}

/** A package by its catalog name, else by its own word in words: a catalog
 * that names a package only by its word (`applehealth`) reads as the
 * console's display name for it ("Apple Health"). */
export function packageName(pkg: { id: string; name?: string }): string {
  const word = packageWord(pkg.id)
  if (pkg.name && pkg.name !== word) return pkg.name
  return packageDisplayName(word)
}

/** The catalog name of the package an account kind or a webhook belongs to:
 * the request's own package row where it names one, else the package's word. */
export function ownerName(
  rows: readonly AuthorizationSetup[],
  packageId: string
): string {
  const row = rows.find((r) => r.package?.id === packageId)
  return row?.package
    ? packageName(row.package)
    : packageDisplayName(packageWord(packageId))
}

function kindPackage(kind: string): string {
  const parts = kind.split("/")
  return parts.slice(0, 2).join("/")
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
function accountWords(
  account: AuthorizationAccount,
  rows: readonly AuthorizationSetup[]
): string {
  const label = account.label?.singular ?? displayName(account.kind)
  return `${ownerName(rows, kindPackage(account.kind))} ${lowerFirst(label)}`
}

/** One setup row as the line the approval page shows for it: what approving
 * does, or that it is in place already. A blocked row still says what it
 * would do; its problem is shown beside it. */
export function setupTitle(
  row: AuthorizationSetup,
  rows: readonly AuthorizationSetup[]
): string {
  if (row.package) {
    const name = packageName(row.package)
    if (row.action === "none") return `${name} is installed`
    if (row.action === "upgrade") return `Update ${name}`
    if (row.action === "blocked" && row.package.held > 0)
      return `Update ${name}`
    return `Install ${name}`
  }
  if (row.account) {
    const what = `${accountWords(row.account, rows)} “${accountName(row.account)}”`
    return row.action === "none"
      ? `The ${what} is in place`
      : `Create the ${what}`
  }
  if (row.webhook) {
    const what = row.webhook.package
      ? `${ownerName(rows, row.webhook.package)} webhook`
      : `${row.webhook.trigger} webhook`
    return row.action === "none" ? `The ${what} is on` : `Turn on the ${what}`
  }
  return row.type
}

/** Whether a package ships code, and whether any of it starts on its own: an
 * enabled trigger that is not a webhook runs once the package is installed. */
export function codeWords(pkg: AuthorizationPackage): string | undefined {
  if (!pkg.functions?.length) return undefined
  const automatic = pkg.triggers?.some((t) => t.enabled && !t.webhook)
  return automatic
    ? "Runs code on your server, automatically."
    : "Runs code on your server."
}

/** The packages an install brings first: "Also installs Health." */
export function chainWords(pkg: AuthorizationPackage): string | undefined {
  const steps = pkg.requires ?? []
  const installs = steps
    .filter((s) => s.action === "install")
    .map((s) => packageName(s))
  const updates = steps
    .filter((s) => s.action === "upgrade")
    .map((s) => packageName(s))
  const parts = [
    installs.length > 0 ? `installs ${listWords(installs)}` : "",
    updates.length > 0 ? `updates ${listWords(updates)}` : "",
  ].filter(Boolean)
  return parts.length > 0 ? `Also ${parts.join(" and ")}.` : undefined
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

/** The scopes the app asks for that the token it holds does not carry, and
 * the ones it does. Without a previous token every scope is new. */
export function splitByPrevious(
  scopes: readonly AuthorizationScope[],
  previous: AuthorizationPrevious | undefined
): { fresh: AuthorizationScope[]; allowed: AuthorizationScope[] } {
  if (!previous) return { fresh: [...scopes], allowed: [] }
  const held = new Set(previous.scopes)
  return {
    fresh: scopes.filter((s) => !held.has(s.scope)),
    allowed: scopes.filter((s) => held.has(s.scope)),
  }
}

/** A re-authorization's heading, by what changed since the token the app
 * holds: nothing (a renewal), more scopes, setup to run, or both. */
export function reauthorizationHeading(
  newScopes: number,
  setupToDo: number
): string {
  if (newScopes > 0 && setupToDo > 0)
    return "Allow more access and update server setup"
  if (newScopes > 0) return "Allow more access"
  if (setupToDo > 0) return "Update server setup"
  return "Renew access"
}

/** What moved between the plan the page showed and the server's own: each
 * row whose action changed, as it reads now. Empty when only the facts
 * behind a row moved (a version, a description). */
export function planChanges(
  before: readonly AuthorizationSetup[],
  after: readonly AuthorizationSetup[]
): string[] {
  return after.flatMap((row, i) => {
    if (before[i]?.action === row.action) return []
    const title = setupTitle(row, after)
    if (row.action !== "blocked") return [title]
    return [
      `${title}: can’t be done now${row.problem ? `. ${row.problem}` : ""}`,
    ]
  })
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
