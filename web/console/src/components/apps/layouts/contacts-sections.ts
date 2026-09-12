/** The contacts layout's pure half: which A–Z section a title falls in, the
 * initials an avatar shows, the local filter over the loaded rows, and the
 * "first + n" reading of a repeated value. Apart from the component so the
 * grouping is a unit test rather than a render. */

import type { SubstrateRecord } from "@/lib/api/types"

/** The section for every title outside A–Z: a digit, a symbol, another
 * script, or no title at all. Last in the index, like an address book. */
export const OTHER_SECTION = "#"

export const SECTIONS: readonly string[] = [
  ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ",
  OTHER_SECTION,
]

/** What names a contact: the server-rendered title, else `name`. Empty when
 * neither is set, which is what lands the row under "#"; the row still
 * displays its id (`displayTitle`). */
export function contactTitle(record: SubstrateRecord): string {
  const { title, name } = record.properties
  if (typeof title === "string" && title.trim()) return title.trim()
  if (typeof name === "string" && name.trim()) return name.trim()
  return ""
}

export function displayTitle(record: SubstrateRecord): string {
  return contactTitle(record) || record.id
}

/** The section a title files under: its first letter with any accent
 * stripped, upper-cased, when that is A–Z; otherwise "#". */
export function sectionOf(title: string): string {
  const first = title
    .trim()
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .charAt(0)
    .toUpperCase()
  return /^[A-Z]$/.test(first) ? first : OTHER_SECTION
}

/** Up to two initials: the first letter of the first two words. */
export function initialsOf(title: string): string {
  const words = title.trim().split(/\s+/).filter(Boolean)
  return words
    .slice(0, 2)
    .map((w) => w.charAt(0).toUpperCase())
    .join("")
}

export interface ContactSection {
  key: string
  records: SubstrateRecord[]
}

/** The rows sectioned in index order, empty sections left out and each
 * section keeping the order the rows arrived in (the server's `title` sort). */
export function groupContacts(records: SubstrateRecord[]): ContactSection[] {
  const byKey = new Map<string, SubstrateRecord[]>()
  for (const record of records) {
    const key = sectionOf(contactTitle(record))
    const list = byKey.get(key)
    if (list) list.push(record)
    else byKey.set(key, [record])
  }
  return SECTIONS.filter((key) => byKey.has(key)).map((key) => ({
    key,
    records: byKey.get(key) ?? [],
  }))
}

function strings(value: unknown): string[] {
  if (typeof value === "string") return [value]
  if (Array.isArray(value)) {
    return value.filter((v): v is string => typeof v === "string")
  }
  return []
}

/** The local filter: a case-insensitive substring of the title, the `name`,
 * or any value under the named properties (the kind's email-typed ones). */
export function matchesNeedle(
  record: SubstrateRecord,
  needle: string,
  fields: string[]
): boolean {
  const q = needle.trim().toLowerCase()
  if (!q) return true
  const haystack = [
    displayTitle(record),
    ...strings(record.properties.name),
    ...fields.flatMap((f) => strings(record.properties[f])),
  ]
  return haystack.some((s) => s.toLowerCase().includes(q))
}

/** A repeated value as a row shows it: the first entry and how many more. */
export function firstAndMore(value: unknown): { first: unknown; more: number } {
  if (Array.isArray(value)) {
    const present = value.filter(
      (v) => v !== null && v !== undefined && v !== ""
    )
    return { first: present[0], more: Math.max(present.length - 1, 0) }
  }
  return { first: value, more: 0 }
}
