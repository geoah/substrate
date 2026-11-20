// The synthetic demo member (`?demo`): one invented person every domain's
// demo data is about, the invented sources they come from, a seeded random
// generator, and the record shape the live reads parse, so the demo runs
// through the same transforms as the owner's data. Nothing here is
// anyone's data.

import type { SubstrateRecord } from "@substrate/react"
import { KIND, type Sex } from "../kinds"

/** The invented member. */
export const DEMO_PERSON = {
  contactId: "demo-contact",
  name: "Alex Demo",
  birthDate: "1984-05-17",
  sex: "male" as Sex,
}

/** The invented sources, by id after `observations/source/`, with the
 * class and name a record of theirs carries. A domain's demo draws from
 * these and adds none of its own, so Sources lists one set. */
export const DEMO_SOURCES = {
  band: { id: "band/demo", class: "service", name: "Demo Band" },
  watch: { id: "watch/demo", class: "watch", name: "Demo Watch" },
  phone: { id: "phone/demo", class: "app", name: "Demo Phone" },
  /** The band's nights again, re-exported through the phone: one device
   * twice, which Sources links. */
  bandViaPhone: {
    id: "phone/demo/band",
    class: "app",
    name: "Demo Band (via Demo Phone)",
  },
  lab: { id: "lab/demo", class: "lab", name: "Demo Lab" },
  clinic: { id: "lab/demo-clinic", class: "lab", name: "Demo Clinic" },
  dxa: { id: "lab/demo-dxa", class: "lab", name: "Demo DXA" },
  scale: { id: "scale/demo", class: "scale", name: "Demo Scale" },
} as const

export function rng(seed: number): () => number {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

export function rec(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    version: 1,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
  }
}

/** A quantity as the server answers one. */
export const q = (value: number, unit: string, precision = 3) => ({
  value: value.toFixed(precision),
  unit,
})

/** A reference as the server answers one. */
export const ref = (kind: string, id: string) => ({ ref: `${kind}/${id}` })

/** The demo's source records. */
export function demoSourceRecords(): SubstrateRecord[] {
  return Object.values(DEMO_SOURCES).map((s) =>
    rec(KIND.source, s.id, { name: s.name, class: s.class })
  )
}
