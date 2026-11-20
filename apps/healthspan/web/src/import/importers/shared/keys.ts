// The kinds a provider's plan writes, the keys its records are found by,
// references to them, and the hash a key is made of where the provider's
// data gives no id (apps/healthspan/docs/importers/00-shared.md, Keys).

import { KIND } from "../../../kinds"
import { sha256 } from "../../ids"
import type { PlannedDocRef, PlannedRef, RecordKey } from "../types"

const CLINICAL = "substrate.reamde.dev/clinical"

/** Every kind a provider's plan writes, and the metric it names. */
export const KINDS = {
  source: KIND.source,
  metric: KIND.metric,
  labreport: KIND.labreport,
  labresult: KIND.labresult,
  organization: `${CLINICAL}/organization`,
  patient: `${CLINICAL}/patient`,
  encounter: `${CLINICAL}/encounter`,
  condition: `${CLINICAL}/condition`,
  allergy: `${CLINICAL}/allergy`,
  immunization: `${CLINICAL}/immunization`,
  medicationrequest: `${CLINICAL}/medicationrequest`,
  medicationdispense: `${CLINICAL}/medicationdispense`,
  appointment: `${CLINICAL}/appointment`,
  referral: `${CLINICAL}/referral`,
  document: `${CLINICAL}/document`,
  communication: `${CLINICAL}/communication`,
} as const

/** A planned record's key. */
export function recordKey(
  kind: string,
  namespace: string,
  key: string
): RecordKey {
  return { kind, namespace, key }
}

/** The key of a provider's own source: namespace `source`, keyed by the
 * provider's namespace (`numan`, `dxa/<centre>`). */
export function sourceKey(namespace: string): RecordKey {
  return recordKey(KINDS.source, "source", namespace)
}

/** The key of an organization: namespace `ods`, keyed by its ODS code, so
 * every importer that meets it names one record. */
export function organizationKey(ods: string): RecordKey {
  return recordKey(KINDS.organization, "ods", ods)
}

/** A reference to a planned record (or one an earlier import wrote under
 * the same key). */
export function ref(key: RecordKey): PlannedRef {
  return {
    $ref: { kind: key.kind, namespace: key.namespace, key: key.key },
  }
}

/** A document's place in a property, by its digest. */
export function docRef(digest: string): PlannedDocRef {
  return { $doc: digest }
}

/** A metric's record path, which a result's `metric` holds. */
export function metricPath(id: string): string {
  return `${KINDS.metric}/${id}`
}

/** Bytes as lower-case hex. */
export function hex(bytes: Uint8Array): string {
  let out = ""
  for (const b of bytes) out += b.toString(16).padStart(2, "0")
  return out
}

/** The hash a key is made of where the provider gives no id: SHA-256, as
 * lower-case hex, over the parts each written as its JSON text (a missing
 * part as `null`) and joined by U+001F. */
export function keyHash(...parts: unknown[]): string {
  const text = parts
    .map((p) => JSON.stringify(p === undefined ? null : p) ?? "null")
    .join("\u001f")
  return hex(sha256(new TextEncoder().encode(text)))
}

/** Keys made unique in the order they come: the first as it is, the
 * second and later of one key with `-2`, `-3` (a consultation's key, whose
 * date, clinician and place two consultations can share). */
export function numberedKeys(keys: readonly string[]): string[] {
  const seen = new Map<string, number>()
  return keys.map((k) => {
    const n = (seen.get(k) ?? 0) + 1
    seen.set(k, n)
    return n === 1 ? k : `${k}-${n}`
  })
}

/** Text as a slug: lower case, `%` as ` pct `, every run of anything but
 * ASCII letters, digits and `.` one `-`, no `-` at either end
 * (`Region %Fat (total)` is `region-pct-fat-total`). */
export function slug(text: string): string {
  return text
    .toLowerCase()
    .replace(/%/g, " pct ")
    .replace(/[^a-z0-9.]+/g, "-")
    .replace(/^-+|-+$/g, "")
}
