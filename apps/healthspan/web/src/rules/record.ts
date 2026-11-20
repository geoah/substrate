// The record rules: an appointment booked in the next 14 days, a repeat
// medicine due within 14 days or overdue, a referral past its typical
// wait, and documents or messages that arrived since the owner last
// marked the record read (the profile's `recordSeenAt`).
//
// "New" is about arrival, not the clinical date: a letter dated in March
// that reached the repository yesterday is new. Anything that arrived more
// than 30 days ago is no longer new, read or not.

import { formatDate } from "../lib/format"
import {
  durationDays,
  type ClinicalRecord,
  type ClinicalRow,
} from "../record/read"
import type { Finding } from "./types"

export const rowKey = (r: Pick<ClinicalRow, "kind" | "id">) =>
  `${r.kind}:${r.id}`

const DAY = 86_400_000

const time = (iso: string | undefined) => (iso ? Date.parse(iso) : Number.NaN)

/** How long an unread record stays new. */
export const NEW_FOR_DAYS = 30

/** The Record screen's sections a finding opens at. */
export type RecordSection =
  | "conditions"
  | "medicines"
  | "allergies"
  | "vaccinations"
  | "appointments"
  | "referrals"
  | "documents"
  | "consultations"
  | "messages"

/** The Record screen at a section, with a row to open. */
export function recordHref(section: RecordSection, open?: string): string {
  const q = new URLSearchParams({ section })
  if (open) q.set("open", open)
  return `#/record?${q.toString()}`
}

/** Whether a document or message is new: it arrived after `seenAt`, in
 * the last 30 days, and not after today. */
export function isUnread(
  r: ClinicalRow,
  seenAt: string | undefined,
  today: Date
): boolean {
  const arrived = time(r.arrivedAt)
  if (!Number.isFinite(arrived)) return false
  if (
    arrived > today.getTime() ||
    today.getTime() - arrived > NEW_FOR_DAYS * DAY
  )
    return false
  return !(seenAt && arrived <= time(seenAt))
}

/** The documents and messages not read yet, newest arrival first. */
export function unreadRows(
  rec: ClinicalRecord,
  seenAt: string | undefined,
  today: Date
): ClinicalRow[] {
  return [...rec.rows.document, ...rec.rows.communication]
    .filter((r) => isUnread(r, seenAt, today))
    .sort((a, b) => time(b.arrivedAt) - time(a.arrivedAt))
}

/** The `recordSeenAt` that marks everything arrived read: the newest
 * arrival, a time the server stamped, so a clock off on this device marks
 * nothing that has not arrived. Undefined where it would move nothing. */
export function markAllRead(
  rec: ClinicalRecord,
  seenAt: string | undefined
): string | undefined {
  const arrivals = [...rec.rows.document, ...rec.rows.communication]
    .map((r) => time(r.arrivedAt))
    .filter(Number.isFinite)
  if (!arrivals.length) return undefined
  const newest = Math.max(...arrivals)
  if (seenAt && newest <= time(seenAt)) return undefined
  return new Date(newest).toISOString()
}

/** One finding per unread document or message, or one per kind when more
 * than three of that kind are unread. */
function unreadFindings(
  rec: ClinicalRecord,
  seenAt: string | undefined,
  today: Date
): Finding[] {
  const out: Finding[] = []
  const unread = unreadRows(rec, seenAt, today)
  for (const kind of ["document", "communication"] as const) {
    const rows = unread.filter((r) => r.kind === kind)
    const noun = kind === "document" ? "document" : "message"
    const section = kind === "document" ? "documents" : "messages"
    if (rows.length > 3) {
      const first = formatDate(rows[rows.length - 1].arrivedAt)
      const last = formatDate(rows[0].arrivedAt)
      out.push({
        section: "record",
        subject: `${kind}:unread`,
        title: `${rows.length} new ${noun}s`,
        detail:
          first === last
            ? `Arrived on ${last}, not opened yet`
            : `Arrived between ${first} and ${last}, not opened yet`,
        tone: "info",
        word: "New",
        since: rows[0].arrivedAt,
        href: recordHref(section),
        rank: 3,
      })
      continue
    }
    for (const r of rows)
      out.push({
        section: "record",
        subject: rowKey(r),
        title: r.title,
        detail: `${noun === "document" ? "Document" : "Message"}${r.at ? ` dated ${formatDate(r.at)}` : ""}, arrived ${formatDate(r.arrivedAt)}`,
        tone: "info",
        word: "New",
        since: r.arrivedAt,
        href: recordHref(section, rowKey(r)),
        rank: 3,
      })
  }
  return out
}

export function evaluateRecord(
  rec: ClinicalRecord,
  today: Date,
  seenAt: string | undefined
): Finding[] {
  const now = today.getTime()
  const out: Finding[] = []
  for (const a of rec.rows.appointment) {
    const t = a.at ? Date.parse(a.at) : Number.NaN
    if (a.p.status === "booked" && t >= now && t - now <= 14 * DAY)
      out.push({
        section: "record",
        subject: `appointment:${a.id}`,
        title: a.title,
        detail: `Booked for ${formatDate(t)}${typeof a.p.location === "string" ? `, ${a.p.location}` : ""}`,
        tone: "info",
        word: "Upcoming",
        since: a.at,
        href: recordHref("appointments"),
        rank: 3,
      })
  }
  for (const m of rec.rows.medicationrequest) {
    const next =
      typeof m.p.nextIssueAt === "string"
        ? Date.parse(m.p.nextIssueAt)
        : Number.NaN
    if (
      m.p.intent === "plan" &&
      m.p.status === "active" &&
      next - now <= 14 * DAY
    )
      out.push({
        section: "record",
        subject: `medication:${m.id}`,
        title: m.title,
        detail:
          next < now
            ? `The next issue was due on ${formatDate(next)}`
            : `The next issue is due on ${formatDate(next)}`,
        tone: "warning",
        word: next < now ? "Overdue" : "Due soon",
        since: m.p.nextIssueAt as string,
        href: recordHref("medicines"),
        rank: 2,
      })
  }
  for (const r of rec.rows.referral) {
    const wait = durationDays(r.p.typicalWait)
    const at = r.at ? Date.parse(r.at) : Number.NaN
    if (r.p.status === "active" && wait !== undefined && now - at > wait * DAY)
      out.push({
        section: "record",
        subject: `referral:${r.id}`,
        title: r.title,
        detail: `Referred on ${formatDate(at)}; the typical wait of ${Math.round(wait / 7)} weeks has passed`,
        tone: "info",
        word: "Past typical wait",
        since: r.at,
        href: recordHref("referrals"),
        rank: 3,
      })
  }
  out.push(...unreadFindings(rec, seenAt, today))
  return out
}
