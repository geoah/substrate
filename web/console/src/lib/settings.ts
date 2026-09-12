/** The settings surfaces' pure fold: `setting` and `secret` records grouped by
 * the bundle that owns them, each projected into the one control it earns
 * (decision record 0076).
 *
 * Ownership is the ID PREFIX and nothing else: a record's id is
 * `<authority>/<package>/<name>`, exactly three segments, so the first two are
 * the bundle and the third is the setting's own name. Nothing here asks the
 * server which bundle a record belongs to, because the id already says.
 *
 * A setting's `type` is a HINT on the record, not a declared property type, so
 * it is projected into the same `FormField` a declared property earns and
 * rendered by the same `PropertyField`. That keeps one control per datatype
 * across the console, and keeps the hint's small vocabulary in one place. */

import { SECRET_KIND } from "@/lib/api/settings"
import type { SubstrateRecord } from "@/lib/api/types"
import type { FormField } from "@/lib/record-form"
import type { PropSpec } from "@/lib/record-schema"
import { controlFor, humanizeName, inputTypeFor } from "@/lib/record-schema"

/** The `type` hints a `setting` record may carry, and the datatype each one
 * borrows for its control. An unknown hint reads as a string, because the
 * value is stored as one either way. */
const TYPE_DATATYPE: Record<string, string> = {
  string: "string",
  url: "url",
  int: "int",
  bool: "bool",
  enum: "string",
}

/** One setting or secret, as a form renders it. */
export interface SettingField {
  /** The record behind it: what a save patches. */
  record: SubstrateRecord
  /** Kind and id together: what identifies the field to a form. */
  key: string
  /** The bundle that owns it: the record id's first two segments. */
  bundle: string
  /** The setting's own name: the id's third segment. */
  name: string
  /** True for a `secret` record, whose value is write-only. */
  secret: boolean
  /** The bundle declared it required, so an empty value is a setup item. */
  required: boolean
  /** The stored value; always empty for a secret, which never reads back. */
  value: string
  /** A value is stored. A sealed secret reads back as the redacted marker and
   * an empty one reads back empty, so set / not set is the only question the
   * console can answer about a secret. */
  set: boolean
  /** The control, label and description the field renders with. */
  field: FormField
}

/** One bundle's settings, in one card. */
export interface SettingGroup {
  bundle: string
  fields: SettingField[]
}

/** The setting's own name: the one segment past `<authority>/<package>/`, and
 * empty when the id is not a bundle setting's. EXACTLY three segments, all of
 * them filled: that is the id the server admits (engine `settingName`), so an
 * id with a fourth segment is some other record that happens to live in this
 * collection and is not offered as a bundle's setting. */
export function settingNameOf(id: string): string {
  const parts = id.split("/")
  return parts.length === 3 && parts.every(Boolean) ? parts[2] : ""
}

/** The bundle a setting record belongs to: the first two segments of its id
 * (`<authority>/<package>`). Empty for an id no bundle owns. */
export function bundleIdOf(id: string): string {
  return settingNameOf(id) ? id.split("/").slice(0, 2).join("/") : ""
}

/** Whether this record is a bundle's setting at all: its id names a bundle
 * and one setting under it. */
export function isBundleSetting(record: SubstrateRecord): boolean {
  return settingNameOf(record.id) !== ""
}

/** What identifies a setting to a form: a record is unique by KIND and id, not
 * by id, so a bundle's `setting` and its `secret` may both be `apiKey`. A key
 * that dropped the kind would let one field's draft land in the other's
 * record, and a secret typed into a plain setting is readable afterwards. */
export function settingKey(
  record: Pick<SubstrateRecord, "kind" | "id">
): string {
  return `${record.kind}\n${record.id}`
}

function text(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function values(record: SubstrateRecord): string[] {
  const raw = record.properties.values
  if (!Array.isArray(raw)) return []
  return raw.filter((v): v is string => typeof v === "string")
}

function specOf(record: SubstrateRecord, name: string): PropSpec {
  const secret = record.kind === SECRET_KIND
  const hint = text(record.properties.type)
  const admitted = values(record)
  return {
    name: "value",
    label: text(record.properties.displayName) || humanizeName(name),
    kind: secret ? "secret" : (TYPE_DATATYPE[hint] ?? "string"),
    required: record.properties.required === true,
    repeated: false,
    keyed: false,
    managed: false,
    // An `enum` setting names its admitted values on the record, which is what
    // turns the control into a select.
    values:
      !secret && hint === "enum" && admitted.length
        ? admitted.map((value) => ({ value, label: "" }))
        : undefined,
    description: text(record.properties.description) || undefined,
  }
}

/** Project one `setting` or `secret` record into the field a form renders. */
export function settingField(record: SubstrateRecord): SettingField {
  const name = settingNameOf(record.id)
  const secret = record.kind === SECRET_KIND
  const spec = specOf(record, name)
  const stored = text(record.properties.value)
  return {
    record,
    key: settingKey(record),
    bundle: bundleIdOf(record.id),
    name,
    secret,
    required: spec.required,
    // A secret's stored value never reaches the browser, so the form starts
    // blank whatever the read served.
    value: secret ? "" : stored,
    set: stored !== "",
    field: {
      // The control's DOM id, so it must separate two records of one id the
      // same way the draft key does, without a newline in an attribute.
      name: `${record.kind}:${record.id}`,
      label: spec.label,
      control: controlFor(spec),
      inputType: inputTypeFor(spec),
      options: spec.values,
      required: spec.required,
      description: spec.description,
      spec,
    },
  }
}

/** Every setting grouped by its owning bundle, bundles and fields both sorted
 * by name, so the page reads the same on every load. A record whose id names
 * no bundle setting is left out rather than grouped under a prefix nobody
 * owns. */
export function groupSettings(records: SubstrateRecord[]): SettingGroup[] {
  const byBundle = new Map<string, SettingField[]>()
  for (const record of records.filter(isBundleSetting)) {
    const field = settingField(record)
    const group = byBundle.get(field.bundle)
    if (group) group.push(field)
    else byBundle.set(field.bundle, [field])
  }
  return [...byBundle.entries()]
    .map(([bundle, fields]) => ({
      bundle,
      // The kind breaks the tie, because one bundle may own a `setting` and a
      // `secret` of the same name.
      fields: fields.sort(
        (a, b) =>
          a.name.localeCompare(b.name) ||
          a.record.kind.localeCompare(b.record.kind)
      ),
    }))
    .sort((a, b) => a.bundle.localeCompare(b.bundle))
}

/** A required setting nobody has filled in: what the server counts as a
 * `setting` setup item, marked on the field here. */
export function unset(field: SettingField): boolean {
  return field.required && !field.set
}
