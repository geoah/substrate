// The demo member's clinical record (`?demo`): two invented providers, a
// GP record under `demogp/` and a hospital record under `demohosp/`, in
// the shape the clinical kinds declare, so the demo runs through the same
// transforms as the owner's record. Nothing here is anyone's data.

import type { SubstrateRecord } from "@substrate/react"
import { DEMO_PERSON, rec } from "./shared"

const C = "substrate.reamde.dev/clinical"

export interface DemoRecordOptions {
  /** `none` leaves the patient registration out, so nothing is offered
   * (`?profile=none`). */
  profile?: string | null
}

/** The registration with the GP record's health service: the member's
 * birth date and administrative gender, offered on getting started. */
export function demoPatientRecords(
  opts: DemoRecordOptions = {}
): SubstrateRecord[] {
  if (opts.profile === "none") return []
  return [
    rec(`${C}/patient`, "demogp/demo/owner", {
      service: "Demo Health Service",
      name: DEMO_PERSON.name,
      birthDate: DEMO_PERSON.birthDate,
      gender: DEMO_PERSON.sex,
    }),
  ]
}

/** The record by kind (the kind's last segment): a booked appointment, a
 * repeat medicine due soon, a referral past its typical wait, a new letter
 * and a new message, and the patient registration. */
export function demoRecordRows(
  today = new Date(),
  opts: DemoRecordOptions = {}
): Record<string, SubstrateRecord[]> {
  const D = 86_400_000
  const at = (days: number) =>
    new Date(today.getTime() + days * D).toISOString()
  const org = (id: string) => ({ ref: `${C}/organization/${id}` })
  // Each record arrives when it was written, unless `arrived` says later.
  const r = (
    kind: string,
    id: string,
    p: Record<string, unknown>,
    arrived?: string
  ): SubstrateRecord => {
    const row = rec(`${C}/${kind}`, id, p)
    const when =
      arrived ?? (typeof p.at === "string" && p.at <= at(0) ? p.at : at(-400))
    return { ...row, createdAt: when, updatedAt: when, version: 1 }
  }
  const file = (n: number, mediaType: string, size: number) => ({
    digest: `blob-sha256-${String(n).repeat(64)}`,
    mediaType,
    name: `demo-${n}.${mediaType.split("/")[1]}`,
    size,
    status: "stored",
  })
  return {
    patient: demoPatientRecords(opts),
    organization: [
      r("organization", "org/demo-surgery", {
        name: "Demo Surgery",
        type: "gppractice",
      }),
      r("organization", "org/demo-hospital", {
        name: "Demo General Hospital",
        type: "hospital",
      }),
    ],
    condition: [
      r("condition", "demogp/demo/c1", {
        at: at(-900),
        name: "Seasonal allergic rhinitis",
        clinicalStatus: "active",
        organization: org("org/demo-surgery"),
      }),
      r("condition", "demogp/demo/c2", {
        at: at(-2000),
        name: "Sprained ankle",
        clinicalStatus: "resolved",
        organization: org("org/demo-surgery"),
      }),
    ],
    medicationrequest: [
      r("medicationrequest", "demogp/demo/m1", {
        at: at(-300),
        medication: "Cetirizine 10 mg tablets",
        dosage: "One a day when needed",
        intent: "plan",
        status: "active",
        lastIssuedAt: at(-50),
        nextIssueAt: at(6),
        organization: org("org/demo-surgery"),
      }),
      r("medicationrequest", "demogp/demo/m2", {
        at: at(-700),
        medication: "Ibuprofen 400 mg tablets",
        dosage: "One three times a day for five days",
        intent: "order",
        status: "completed",
        organization: org("org/demo-surgery"),
      }),
    ],
    medicationdispense: [],
    allergy: [
      r("allergy", "demogp/demo/a1", {
        at: at(-3000),
        name: "Allergy to penicillin",
        substance: "penicillin",
        type: "allergy",
        category: "medication",
        clinicalStatus: "active",
        verificationStatus: "confirmed",
        criticality: "high",
        reactions: [{ manifestation: "Urticaria", severity: "moderate" }],
        organization: org("org/demo-surgery"),
      }),
    ],
    immunization: [
      r("immunization", "demogp/demo/i1", {
        at: at(-40),
        name: "Influenza vaccine",
        organization: org("org/demo-surgery"),
      }),
      r("immunization", "demogp/demo/i2", {
        at: at(-400),
        name: "COVID-19 booster",
        organization: org("org/demo-surgery"),
      }),
    ],
    appointment: [
      r("appointment", "demohosp/demo/ap1", {
        at: `${at(5).slice(0, 10)}T09:40:00Z`,
        endsAt: `${at(5).slice(0, 10)}T10:00:00Z`,
        name: "Dermatology follow-up",
        status: "booked",
        channel: "inperson",
        specialty: "Dermatology",
        service: "Skin clinic",
        location: "Outpatients, level 2, Demo General Hospital",
        participants: [{ name: "Dr A. Example" }],
        phone: "020 7946 0000",
        organization: org("org/demo-hospital"),
      }),
      r("appointment", "demohosp/demo/ap2", {
        at: at(-60),
        name: "Dermatology new patient",
        status: "fulfilled",
        specialty: "Dermatology",
        organization: org("org/demo-hospital"),
      }),
    ],
    referral: [
      r("referral", "demogp/demo/r1", {
        at: at(-150),
        name: "Referral to physiotherapy",
        status: "active",
        typicalWait: "P12W",
        organization: org("org/demo-surgery"),
      }),
    ],
    document: [
      r("document", "demohosp/demo/d1", {
        at: at(-3),
        name: "Clinic letter",
        type: "Letter",
        file: file(1, "application/pdf", 84_000),
        text: "Dear Dr Example,\n\nI saw your patient in the skin clinic. The rash on both forearms has settled with emollients. Review in three months.",
        organization: org("org/demo-hospital"),
      }),
      r("document", "demogp/demo/d2", {
        at: at(-200),
        name: "Blood test request",
        type: "Attachment",
        organization: org("org/demo-surgery"),
      }),
    ],
    communication: [
      r(
        "communication",
        "demohosp/demo/msg1",
        {
          at: at(-20),
          topic: "Appointment reminder",
          sender: "Demo General Hospital",
          body: "Your appointment with Dermatology is booked. Please arrive ten minutes early.",
          attachments: [file(2, "image/png", 187_000)],
          organization: org("org/demo-hospital"),
        },
        at(-1)
      ),
    ],
    encounter: [
      r("encounter", "demohosp/demo/e1", {
        at: at(-60),
        summary: "Dermatology new patient",
        type: "Outpatient",
        location: "Outpatients, level 2",
        text: "**Seen for** a rash on both forearms.\n\nPlan: emollients, review in three months.",
        organization: org("org/demo-hospital"),
      }),
      r("encounter", "demogp/demo/e2", {
        at: at(-90),
        summary: "GP consultation",
        type: "Telephone",
        text: "Hay fever worse this year. Repeat cetirizine added.",
        organization: org("org/demo-surgery"),
      }),
    ],
  }
}
