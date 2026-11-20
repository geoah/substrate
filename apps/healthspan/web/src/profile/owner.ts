// The owner as every screen reads them (docs/plans/healthspan.md, The
// owner's details): the contact the core setting names, the profile this
// app keeps, and a patient registration about them, offered and never used
// until the owner saves it.

import { refProp, stringProp, type SubstrateRecord } from "@substrate/react"
import type { Owner } from "../data/profile"
import { KIND, type Profile, type Sex } from "../kinds"

/** A day of a contact's `dates`; the year is absent for one saved without. */
export interface ContactDay {
  year?: number
  month: number
  day: number
}

/** The owner's contact, as this app reads it. */
export interface OwnerContact {
  /** The contact's id as served: a merged contact answers as its winner. */
  id: string
  name?: string
  birthday?: ContactDay
}

/** A `clinical/patient` registration, as this app reads it. */
export interface PatientRegistration {
  id: string
  /** The health service it is with, which titles it. */
  service?: string
  /** Whose registration it is, as `<kind>/<id>`; absent means the owner. */
  about?: string
  /** As written: FHIR `date`, normally `YYYY-MM-DD`. */
  birthDate?: string
  /** FHIR's administrative gender: `male`, `female`, `other`, `unknown`. */
  gender?: string
}

/** The setting's value: the id of the owner's contact, or none. */
export function ownerContactId(
  setting: SubstrateRecord | null
): string | undefined {
  const v = setting?.properties.value
  return typeof v === "string" && v.trim() ? v.trim() : undefined
}

function whole(v: unknown): number | undefined {
  return typeof v === "number" && Number.isInteger(v) ? v : undefined
}

function dayOf(v: unknown): ContactDay | undefined {
  if (!v || typeof v !== "object") return undefined
  const o = v as Record<string, unknown>
  const month = whole(o.month)
  const day = whole(o.day)
  if (!month || !day) return undefined
  const year = whole(o.year)
  return year ? { year, month, day } : { month, day }
}

export function contactOf(r: SubstrateRecord): OwnerContact {
  const dates = r.properties.dates
  const birthday =
    dates && typeof dates === "object"
      ? dayOf((dates as Record<string, unknown>).birthday)
      : undefined
  return {
    id: r.id,
    name: stringProp(r, "name")?.trim() || undefined,
    birthday,
  }
}

export function patientOf(r: SubstrateRecord): PatientRegistration {
  const about = refProp(r, "about")
  return {
    id: r.id,
    service: stringProp(r, "service"),
    about: about && `${about.kind}/${about.id}`,
    birthDate: stringProp(r, "birthDate"),
    gender: stringProp(r, "gender"),
  }
}

const pad = (n: number, width = 2) => String(n).padStart(width, "0")

/** `YYYY-MM-DD` when it names a day of the calendar. */
export function isCalendarDate(iso: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(iso)) return false
  const t = Date.parse(`${iso}T00:00:00Z`)
  return Number.isFinite(t) && new Date(t).toISOString().slice(0, 10) === iso
}

/** A contact's birthday as a birth date: only with its year, and only a day
 * the calendar has. */
export function birthDateFromContact(
  day: ContactDay | undefined
): string | undefined {
  if (!day?.year) return undefined
  const iso = `${pad(day.year, 4)}-${pad(day.month)}-${pad(day.day)}`
  return isCalendarDate(iso) ? iso : undefined
}

/** A FHIR date or dateTime to `YYYY-MM-DD`; a partial date (`1990-03`) is
 * no value. */
export function birthDateOf(v: unknown): string | undefined {
  if (typeof v !== "string") return undefined
  const m = /^(\d{4}-\d{2}-\d{2})/.exec(v)
  return m && isCalendarDate(m[1]) ? m[1] : undefined
}

/** FHIR `gender` as a reference basis to offer: `male` and `female`;
 * anything else (`other`, `unknown`) offers nothing. */
export function sexOf(gender: unknown): Sex | undefined {
  const g = typeof gender === "string" ? gender.trim().toLowerCase() : ""
  return g === "male" || g === "female" ? g : undefined
}

/** The one patient registration about the owner: it names no `about`, or
 * names the owner's contact. None where no registration or more than one
 * is theirs, so a value is never offered from a guess between two. */
export function patientAboutOwner(
  patients: readonly PatientRegistration[],
  contactId: string | undefined
): PatientRegistration | undefined {
  const own = contactId ? `${KIND.contact}/${contactId}` : undefined
  const theirs = patients.filter((p) => !p.about || p.about === own)
  return theirs.length === 1 ? theirs[0] : undefined
}

/** Who the owner is and what is known of them. Birth date: the contact's
 * birthday with its year, then the profile's. Sex: the profile's alone. A
 * patient registration's values are suggestions. */
export function resolveOwner(input: {
  contact?: OwnerContact
  profile?: Profile
  patients: readonly PatientRegistration[]
}): Owner {
  const { contact, profile } = input
  const fromContact = birthDateFromContact(contact?.birthday)
  const fromProfile = profile?.birthDate
    ? birthDateOf(profile.birthDate)
    : undefined
  const patient = patientAboutOwner(input.patients, contact?.id)
  const suggestions: Owner["suggestions"] = {}
  const suggestedBirthDate = birthDateOf(patient?.birthDate)
  const suggestedSex = sexOf(patient?.gender)
  if (suggestedBirthDate) suggestions.birthDate = suggestedBirthDate
  if (suggestedSex) suggestions.sex = suggestedSex
  return {
    contact: contact && {
      id: contact.id,
      name: contact.name,
      birthday: contact.birthday,
    },
    birthDate: fromContact
      ? { value: fromContact, origin: "contact" }
      : fromProfile
        ? { value: fromProfile, origin: "profile" }
        : undefined,
    sex: profile?.sex ? { value: profile.sex, origin: "profile" } : undefined,
    suggestions,
    suggestedBy:
      patient && (suggestedBirthDate || suggestedSex)
        ? { id: patient.id, service: patient.service }
        : undefined,
    profile,
  }
}

/** The age in whole years on `today` of someone born on `birthDate`. */
export function ageOn(birthDate: string, today: Date): number {
  const [y, m, d] = birthDate.split("-").map(Number)
  let age = today.getFullYear() - y
  if (
    today.getMonth() + 1 < m ||
    (today.getMonth() + 1 === m && today.getDate() < d)
  )
    age--
  return age
}

/** What is wrong with a birth date typed by the owner, in words, or
 * nothing: a calendar day, not after today, and an age the reference
 * curves cover. */
export function birthDateProblem(iso: string, today: Date): string | undefined {
  if (!isCalendarDate(iso)) return "Enter a birth date as YYYY-MM-DD."
  const t = new Date(`${iso}T00:00:00`)
  if (t.getTime() > today.getTime())
    return "A birth date can't be in the future."
  const age = ageOn(iso, today)
  if (age < 18 || age > 110) return "The age score works for ages 18 to 110."
  return undefined
}

/** `YYYY-MM-DD` of a local day: a date field's bound. */
export function localDay(d: Date): string {
  return `${pad(d.getFullYear(), 4)}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** A birth date as the local day it names, for `formatDate`: a bare
 * `YYYY-MM-DD` parses as UTC and would print the day before west of it. */
export function birthDay(iso: string): Date {
  return new Date(`${iso}T00:00:00`)
}
