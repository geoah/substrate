import { describe, expect, it } from "vitest"
import { KIND, Profile, type Profile as ProfileValue } from "../kinds"
import {
  birthDateFromContact,
  birthDateOf,
  birthDateProblem,
  contactOf,
  ownerContactId,
  patientAboutOwner,
  resolveOwner,
  sexOf,
  type PatientRegistration,
} from "./owner"

const record = (kind: string, id: string, p: Record<string, unknown>) => ({
  id,
  kind,
  properties: p,
  version: 1,
  createdAt: "2026-10-01T00:00:00Z",
  updatedAt: "2026-10-01T00:00:00Z",
})

const contact = (dates?: Record<string, unknown>) =>
  contactOf(
    record(KIND.contact, "c1", {
      name: "Sam Example",
      ...(dates ? { dates } : {}),
    })
  )

const profile = (p: Record<string, unknown>): ProfileValue =>
  Profile.read(
    record(KIND.profile, "owner", { name: "Healthspan profile", ...p })
  )

const registration = (
  id: string,
  p: Partial<PatientRegistration> = {}
): PatientRegistration => ({
  id,
  birthDate: "1980-04-05",
  gender: "female",
  ...p,
})

describe("the owner's contact", () => {
  it("reads the setting's value, trimmed, and none where it is empty", () => {
    expect(ownerContactId(record(KIND.setting, "s", { value: " c1 " }))).toBe(
      "c1"
    )
    expect(ownerContactId(record(KIND.setting, "s", { value: "" }))).toBe(
      undefined
    )
    expect(ownerContactId(null)).toBeUndefined()
  })
  it("reads the name and the birthday, with or without its year", () => {
    expect(contact({ birthday: { year: 1984, month: 5, day: 7 } })).toEqual({
      id: "c1",
      name: "Sam Example",
      birthday: { year: 1984, month: 5, day: 7 },
    })
    expect(contact({ birthday: { month: 5, day: 7 } }).birthday).toEqual({
      month: 5,
      day: 7,
    })
    expect(contact().birthday).toBeUndefined()
  })
})

describe("birth date", () => {
  it("takes the contact's birthday first", () => {
    const o = resolveOwner({
      contact: contact({ birthday: { year: 1984, month: 5, day: 7 } }),
      profile: profile({ birthDate: "1990-01-01" }),
      patients: [],
    })
    expect(o.birthDate).toEqual({ value: "1984-05-07", origin: "contact" })
  })
  it("then the profile's, where the birthday has no year", () => {
    const o = resolveOwner({
      contact: contact({ birthday: { month: 5, day: 7 } }),
      profile: profile({ birthDate: "1990-01-01" }),
      patients: [],
    })
    expect(o.birthDate).toEqual({ value: "1990-01-01", origin: "profile" })
    expect(o.contact?.birthday).toEqual({ month: 5, day: 7 })
  })
  it("gives no birth date for a yearless birthday and no profile", () => {
    expect(
      resolveOwner({
        contact: contact({ birthday: { month: 5, day: 7 } }),
        patients: [],
      }).birthDate
    ).toBeUndefined()
  })
  it("never takes a day the calendar lacks", () => {
    expect(
      birthDateFromContact({ year: 1990, month: 2, day: 30 })
    ).toBeUndefined()
    expect(birthDateOf("1991-02-03T00:00:00Z")).toBe("1991-02-03")
    expect(birthDateOf("1991-02")).toBeUndefined()
  })
  it("only offers a patient registration's, never uses it", () => {
    const o = resolveOwner({
      contact: contact(),
      patients: [registration("gp/1", { service: "A Health Service" })],
    })
    expect(o.birthDate).toBeUndefined()
    expect(o.suggestions).toEqual({ birthDate: "1980-04-05", sex: "female" })
    expect(o.suggestedBy).toEqual({ id: "gp/1", service: "A Health Service" })
  })
  it("checks a typed one: a calendar day, not after today, ages 18 to 110", () => {
    const today = new Date(2026, 10, 9)
    expect(birthDateProblem("1984-05-17", today)).toBeUndefined()
    expect(birthDateProblem("17/05/1984", today)).toMatch(/YYYY-MM-DD/)
    expect(birthDateProblem("2027-01-01", today)).toMatch(/future/)
    expect(birthDateProblem("2015-01-01", today)).toMatch(/18 to 110/)
  })
})

describe("sex", () => {
  it("is the profile's alone", () => {
    const o = resolveOwner({
      contact: contact(),
      profile: profile({ sex: "male" }),
      patients: [registration("gp/1")],
    })
    expect(o.sex).toEqual({ value: "male", origin: "profile" })
    expect(o.suggestions.sex).toBe("female")
  })
  it("offers only male and female from a registration's gender", () => {
    expect(sexOf("male")).toBe("male")
    expect(sexOf("Female")).toBe("female")
    expect(sexOf("other")).toBeUndefined()
    expect(sexOf("unknown")).toBeUndefined()
    const o = resolveOwner({
      patients: [registration("gp/1", { gender: "unknown" })],
    })
    expect(o.suggestions).toEqual({ birthDate: "1980-04-05" })
    expect(o.sex).toBeUndefined()
  })
})

describe("the patient registration about the owner", () => {
  const mine = `${KIND.contact}/c1`
  it("takes one that names no one, or names the owner's contact", () => {
    expect(patientAboutOwner([registration("a")], "c1")?.id).toBe("a")
    expect(
      patientAboutOwner([registration("a", { about: mine })], "c1")?.id
    ).toBe("a")
  })
  it("leaves out one about someone else", () => {
    const other = registration("b", { about: `${KIND.contact}/c2` })
    expect(patientAboutOwner([other], "c1")).toBeUndefined()
    expect(patientAboutOwner([other, registration("a")], "c1")?.id).toBe("a")
  })
  it("offers nothing from two registrations", () => {
    const o = resolveOwner({
      contact: contact(),
      patients: [registration("a"), registration("b", { about: mine })],
    })
    expect(o.suggestions).toEqual({})
    expect(o.suggestedBy).toBeUndefined()
  })
  it("without an owner's contact, counts only the ones naming no one", () => {
    expect(
      patientAboutOwner([registration("a", { about: mine })], undefined)
    ).toBeUndefined()
    expect(patientAboutOwner([registration("a")], undefined)?.id).toBe("a")
  })
})

describe("no owner's contact", () => {
  it("knows the owner from the profile alone", () => {
    const o = resolveOwner({
      profile: profile({ birthDate: "1990-01-01", sex: "female" }),
      patients: [],
    })
    expect(o.contact).toBeUndefined()
    expect(o.birthDate).toEqual({ value: "1990-01-01", origin: "profile" })
    expect(o.sex).toEqual({ value: "female", origin: "profile" })
  })
  it("knows nothing with no profile either", () => {
    expect(resolveOwner({ patients: [] })).toEqual({
      contact: undefined,
      birthDate: undefined,
      sex: undefined,
      suggestions: {},
      suggestedBy: undefined,
      profile: undefined,
    })
  })
})
