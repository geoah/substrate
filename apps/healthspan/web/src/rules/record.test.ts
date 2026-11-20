import { describe, expect, it } from "vitest"
import { demoRecordRows } from "../demo/record"
import {
  durationDays,
  filesOf,
  providersOf,
  toRecord,
  toRow,
} from "../record/read"
import { patientOf } from "../profile/owner"
import {
  evaluateRecord,
  isUnread,
  markAllRead,
  recordHref,
  unreadRows,
} from "./record"

const today = new Date("2026-10-25T12:00:00Z")
const record = toRecord(demoRecordRows(today))
const words = (seenAt?: string) =>
  Object.fromEntries(
    evaluateRecord(record, today, seenAt).map((f) => [f.subject, f.word])
  )

const doc = (
  id: string,
  p: Record<string, unknown>,
  createdAt: string,
  updatedAt = createdAt,
  version = 1
) =>
  toRow("document", {
    id,
    kind: "substrate.reamde.dev/clinical/document",
    properties: p,
    createdAt,
    updatedAt,
    version,
  })

describe("record rules", () => {
  it("finds the booked appointment, the repeat due soon and the late referral", () => {
    const w = words()
    expect(w["appointment:demohosp/demo/ap1"]).toBe("Upcoming")
    expect(w["medication:demogp/demo/m1"]).toBe("Due soon")
    expect(w["referral:demogp/demo/r1"]).toBe("Past typical wait")
  })
  it("ignores a completed order and a fulfilled appointment", () => {
    const w = words()
    expect(w["medication:demogp/demo/m2"]).toBeUndefined()
    expect(w["appointment:demohosp/demo/ap2"]).toBeUndefined()
  })
  it("reads ISO durations in weeks, months and days", () => {
    expect(durationDays("P18W")).toBe(126)
    expect(durationDays("P2M")).toBe(60)
    expect(durationDays("P10D")).toBe(10)
    expect(durationDays("soon")).toBeUndefined()
  })
  it("links each finding to its section of the Record screen", () => {
    const fs = evaluateRecord(record, today, undefined)
    expect(fs.find((f) => f.subject.startsWith("medication:"))?.href).toBe(
      "#/record?section=medicines"
    )
    expect(fs.every((f) => f.section === "record")).toBe(true)
  })
})

describe("new documents and messages", () => {
  it("judges new by arrival, not by the clinical date", () => {
    // The reminder is dated 20 days ago and arrived yesterday: new.
    expect(words()["communication:demohosp/demo/msg1"]).toBe("New")
    // A letter dated March that arrived today is new; one dated today that
    // arrived 40 days ago is not.
    const late = doc(
      "x/1",
      { at: "2026-03-01T00:00:00Z" },
      "2026-10-25T08:00:00Z"
    )
    const early = doc(
      "x/2",
      { at: "2026-10-25T00:00:00Z" },
      "2026-09-15T08:00:00Z"
    )
    expect(isUnread(late, undefined, today)).toBe(true)
    expect(isUnread(early, undefined, today)).toBe(false)
  })
  it("takes a revision's updatedAt as a new arrival", () => {
    const first = doc(
      "x/1",
      {},
      "2026-10-01T00:00:00Z",
      "2026-10-24T00:00:00Z",
      1
    )
    const revised = doc(
      "x/1",
      {},
      "2026-10-01T00:00:00Z",
      "2026-10-24T00:00:00Z",
      2
    )
    expect(first.arrivedAt).toBe("2026-10-01T00:00:00Z")
    expect(revised.arrivedAt).toBe("2026-10-24T00:00:00Z")
    // Read up to its first arrival, then revised: new again.
    expect(isUnread(first, "2026-10-02T00:00:00Z", today)).toBe(false)
    expect(isUnread(revised, "2026-10-02T00:00:00Z", today)).toBe(true)
  })
  it("reads what arrived up to the mark; a later arrival stays new", () => {
    const before = unreadRows(record, undefined, today).map((r) => r.id)
    expect(before).toEqual(
      expect.arrayContaining(["demohosp/demo/d1", "demohosp/demo/msg1"])
    )
    // Marked read between the letter (3 days ago) and the reminder (1).
    const between = new Date(today.getTime() - 2 * 86_400_000).toISOString()
    const after = words(between)
    expect(after["document:demohosp/demo/d1"]).toBeUndefined()
    expect(after["communication:demohosp/demo/msg1"]).toBe("New")
  })
  it("marks everything read at the newest arrival, and a later arrival is new again", () => {
    const all = markAllRead(record, undefined)
    expect(all).toBe(record.rows.communication[0].arrivedAt)
    expect(unreadRows(record, all, today)).toEqual([])
    expect(markAllRead(record, all)).toBeUndefined()
    const later = doc("x/9", {}, "2026-10-25T09:00:00Z")
    expect(isUnread(later, all, today)).toBe(true)
  })
  it("sums up a kind with more than three unread", () => {
    const rows = demoRecordRows(today)
    for (let i = 0; i < 5; i++)
      rows.document.push({
        id: `demogp/demo/bulk${i}`,
        kind: "substrate.reamde.dev/clinical/document",
        properties: { at: "2020-01-01T00:00:00Z", name: `Letter ${i}` },
        createdAt: "2026-10-24T10:00:00Z",
        updatedAt: "2026-10-24T10:00:00Z",
        version: 1,
      })
    const fs = evaluateRecord(toRecord(rows), today, undefined).filter(
      (f) => f.word === "New"
    )
    const docs = fs.find((f) => f.subject === "document:unread")
    // Five bulk letters and the clinic letter; the request from 200 days ago
    // arrived too long ago to be new.
    expect(docs?.title).toBe("6 new documents")
    expect(docs?.href).toBe("#/record?section=documents")
    expect(fs.filter((f) => f.subject.startsWith("document:")).length).toBe(1)
    expect(
      fs.find((f) => f.subject === "communication:demohosp/demo/msg1")?.href
    ).toBe(
      "#/record?section=messages&open=communication%3Ademohosp%2Fdemo%2Fmsg1"
    )
  })
  it("reads one stored file or a list of them", () => {
    const f = { digest: "blob-sha256-1", mediaType: "image/png", size: 10 }
    expect(filesOf(f)).toEqual([{ ...f, name: undefined }])
    expect(filesOf([f, { nope: 1 }])).toHaveLength(1)
    expect(filesOf(undefined)).toEqual([])
  })
  it("builds a link with the row to open", () => {
    expect(recordHref("documents", "document:a/b")).toBe(
      "#/record?section=documents&open=document%3Aa%2Fb"
    )
  })
})

describe("patient rows", () => {
  const patient = (id: string, p: Record<string, unknown>) => ({
    id,
    kind: "substrate.reamde.dev/clinical/patient",
    properties: p,
    version: 1,
    createdAt: "2026-10-01T00:00:00Z",
    updatedAt: "2026-10-01T00:00:00Z",
  })
  it("reads birth date and gender into patients, never into the listed rows", () => {
    const rec = toRecord({
      patient: [
        patient("b/owner", { birthDate: "1991-02-03", gender: "male" }),
        patient("a/owner", {
          birthDate: "1991-02-03",
          gender: "male",
          name: "X",
        }),
      ],
    })
    expect(rec.patients).toMatchObject([
      { id: "a/owner", birthDate: "1991-02-03", gender: "male" },
      { id: "b/owner", birthDate: "1991-02-03", gender: "male" },
    ])
    expect(Object.keys(rec.rows)).not.toContain("patient")
    expect(patientOf(patient("x/1", { birthDate: 19910203 }))).toEqual({
      id: "x/1",
      service: undefined,
      about: undefined,
      birthDate: undefined,
      gender: undefined,
    })
  })
  it("reads an empty list where the repository has no patient kind", () => {
    expect(toRecord({}).patients).toEqual([])
  })
  it("gives the demo member a GP registration unless asked for none", () => {
    expect(record.patients.map((p) => p.id)).toEqual(["demogp/demo/owner"])
    expect(
      toRecord(demoRecordRows(today, { profile: "none" })).patients
    ).toEqual([])
  })
})

describe("providers", () => {
  it("names each provider from its records, never from the code", () => {
    expect(providersOf(record)).toEqual([
      { id: "demohosp", name: "Demo General Hospital" },
      { id: "demogp", name: "Demo Health Service" },
    ])
  })
  it("falls back to the care provider named most, then the id", () => {
    const rows = demoRecordRows(today, { profile: "none" })
    rows.condition.push({
      id: "other/x/1",
      kind: "substrate.reamde.dev/clinical/condition",
      properties: { name: "Something" },
      version: 1,
      createdAt: "2026-10-01T00:00:00Z",
      updatedAt: "2026-10-01T00:00:00Z",
    })
    expect(providersOf(toRecord(rows))).toEqual([
      { id: "demohosp", name: "Demo General Hospital" },
      { id: "demogp", name: "Demo Surgery" },
      { id: "other", name: "other" },
    ])
  })
})
