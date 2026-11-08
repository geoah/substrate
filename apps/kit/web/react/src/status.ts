import type { AppIssue, PackageStanding } from "@substrate/auth"
import type { OutboxState } from "@substrate/local"

/** Where the app stands with its repository, most blocking first (the
 * kit's LinkStatus). */
export type LinkStatus =
  /** The token was refused or its access ended: sign in again. */
  | { type: "signedOut" }
  /** The token lacks a scope the app needs, or a package it needs waits
   * for the owner's approval. */
  | { type: "needsApproval" }
  /** The server cannot take what the app needs: update it. */
  | { type: "serverTooOld" }
  /** The browser could not keep a change; the reason is said apart. */
  | { type: "notKept"; reason: string }
  /** Signed in; these packages, by name, are being checked. */
  | { type: "preparing"; names: string[] }
  /** The repository holds older versions of these packages than this
   * build needs. */
  | { type: "packagesBehind"; packages: PackageStanding[] }
  /** Ready, with this many writes still waiting. */
  | { type: "ready"; waiting: number }
  /** Ready, and the approved access ends at this time. */
  | { type: "accessEndsSoon"; at: number; waiting: number }
  /** The server could not be reached; writes wait. */
  | { type: "offline"; waiting: number; reason: string }
  /** The server refused a write, which waits, parked, with its words. */
  | { type: "refused"; waiting: number; reason: string }

export function deriveLinkStatus(input: {
  /** Nil until the first check answered. */
  packages: PackageStanding[] | null
  outbox: OutboxState
  waiting: number
  issues: AppIssue[]
  notKept?: string
}): LinkStatus {
  const { outbox, waiting, issues } = input
  if (issues.some((i) => i.type === "accessEnded")) return { type: "signedOut" }
  if (outbox.type === "signedOut") return { type: "signedOut" }
  if (outbox.type === "needsApproval") return { type: "needsApproval" }
  if (issues.some((i) => i.type === "needsApproval"))
    return { type: "needsApproval" }
  if (issues.some((i) => i.type === "serverTooOld"))
    return { type: "serverTooOld" }
  if (input.notKept) return { type: "notKept", reason: input.notKept }
  if (input.packages === null) return { type: "preparing", names: [] }
  const behind = input.packages.filter(
    (p) => typeof p.held === "number" && p.held < p.needs
  )
  if (behind.length) return { type: "packagesBehind", packages: behind }
  const missing = input.packages.filter((p) => p.held === "missing")
  if (missing.length)
    return { type: "preparing", names: missing.map((p) => p.name) }
  if (outbox.type === "offline")
    return { type: "offline", waiting, reason: outbox.words }
  if (outbox.type === "refused")
    return { type: "refused", waiting, reason: outbox.words }
  for (const i of issues)
    if (i.type === "accessEndsSoon")
      return { type: "accessEndsSoon", at: i.at, waiting }
  return { type: "ready", waiting }
}

/** The app's words the status sentences use. */
export interface StatusWords {
  /** The app's name: "Substrate Tasks". */
  app: string
  /** Where unsent writes wait: "this browser". */
  device: string
  /** What the app calls its records, plural: "tasks". */
  kept: string
}

function joinWords(words: string[]): string {
  if (words.length < 2) return words.join("")
  return `${words.slice(0, -1).join(", ")} and ${words.at(-1)}`
}

function capitalized(text: string): string {
  return text.charAt(0).toUpperCase() + text.slice(1)
}

export function formatDay(ms: number): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(ms)
}

export function formatMoment(ms: number): string {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(ms)
}

/** The status in one sentence, as every app's Settings says it. */
export function statusSentence(status: LinkStatus, words: StatusWords): string {
  const { app, device } = words
  const kept = capitalized(words.kept)
  const changes = (n: number) => (n === 1 ? "change" : "changes")
  const wait = (n: number) =>
    `${n} ${n === 1 ? "change waits" : "changes wait"}`
  switch (status.type) {
    case "signedOut":
      return "Your access ended. Sign in again to send what waits."
    case "needsApproval":
      return `${app} needs your approval for more access. Review it to send what waits.`
    case "serverTooOld":
      return `Your substrate needs an update before ${app} can use it. ${kept} wait in ${device} until then.`
    case "notKept":
      return `A change could not be kept in ${device}. ${kept} already kept stay.`
    case "preparing":
      return status.names.length
        ? `Checking the ${joinWords(status.names)} package${status.names.length === 1 ? "" : "s"} on your substrate.`
        : "Checking your substrate."
    case "packagesBehind": {
      const [one] = status.packages
      if (status.packages.length === 1)
        return `Your substrate has version ${String(one.held)} of the ${one.name} package and this app needs version ${one.needs} or later. The update waits for your approval on your substrate; ${words.kept} wait in ${device} until then.`
      return `Your substrate has older versions of the ${joinWords(status.packages.map((p) => p.name))} packages than this app needs. The updates wait for your approval on your substrate; ${words.kept} wait in ${device} until then.`
    }
    case "accessEndsSoon":
      return (
        `Access ends ${formatDay(status.at)}. Renew it to keep sending.` +
        (status.waiting
          ? ` Sending ${status.waiting} ${changes(status.waiting)}.`
          : "")
      )
    case "ready":
      return status.waiting
        ? `Sending ${status.waiting} ${changes(status.waiting)}.`
        : "Up to date."
    case "offline":
      return `Your substrate cannot be reached. ${wait(status.waiting)} in ${device}.`
    case "refused":
      return `Your substrate refused a change. ${wait(status.waiting)}; retry or discard it below.`
  }
}

/** A status's tone, which picks its colour. */
export function statusTone(
  status: LinkStatus
): "positive" | "informative" | "caution" {
  if (status.type === "ready")
    return status.waiting ? "informative" : "positive"
  if (status.type === "preparing") return "informative"
  return "caution"
}
