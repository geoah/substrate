/** What a create or patch sheet asks for. The fields are exactly the action's
 * `prompt` names, in order, and nothing the author did not prompt for: a
 * declared owner-writable property, or a hot column the kind's temporal
 * trait binds (`dueAt`), which `buildFormFields` leaves out because it is
 * not declared. Required is what the declaration marks required plus ONE
 * heading alternative: the first name of the displayTemplate's first
 * placeholder that is in the prompt (`{displayName|name}` with
 * `prompt: [name, ...]` requires `name`). */

import type { KindInfo } from "@/lib/api/types"
import { resolveReferenceTarget } from "@/lib/definition"
import type { FormField } from "@/lib/record-form"
import {
  controlFor,
  exampleFor,
  inputTypeFor,
  ownerWritable,
  type PropSpec,
} from "@/lib/record-schema"
import { allSpecs } from "./cond"

/** The alternatives of the displayTemplate's first placeholder, in order:
 * `{name|title}` is `["name", "title"]`. */
export function headingAlternatives(kind: KindInfo | undefined): string[] {
  const template = (kind?.definition as Record<string, unknown> | undefined)
    ?.displayTemplate
  if (typeof template !== "string") return []
  const m = /\{\s*([^}]+?)\s*\}/.exec(template)
  if (!m) return []
  return m[1]
    .split("|")
    .map((s) => s.trim())
    .filter((s) => /^[A-Za-z_][A-Za-z0-9_]*$/.test(s))
}

/** The one prompted name the heading needs: the first alternative that the
 * prompt asks for. */
export function requiredHeading(
  kind: KindInfo | undefined,
  prompt: string[]
): string | undefined {
  return headingAlternatives(kind).find((name) => prompt.includes(name))
}

/** A reference pinned by a BARE name (`kind: project`) resolves inside the
 * declaring kind's package; the control looks the pin up by identity, so the
 * field carries the resolved identity or the control cannot offer a record. */
function withResolvedPin(
  spec: PropSpec,
  kind: KindInfo,
  kinds: KindInfo[]
): PropSpec {
  if (spec.kind !== "reference" || !spec.to || spec.to.includes("/")) {
    return spec
  }
  const target = resolveReferenceTarget(kinds, kind, spec.to)
  return target ? { ...spec, to: target.identity } : spec
}

function fieldOf(spec: PropSpec, required: boolean): FormField {
  return {
    name: spec.name,
    label: spec.label,
    control: controlFor(spec),
    inputType: inputTypeFor(spec),
    options: spec.values,
    defaultValue:
      typeof spec.default === "string" && spec.default.length
        ? spec.default
        : undefined,
    required,
    description: spec.description,
    example: exampleFor(spec),
    spec,
  }
}

/** The sheet's fields for one prompt list. A name the kind does not carry,
 * or one the owner may not write, is skipped (view-spec already warned). */
export function promptFields(
  kind: KindInfo | undefined,
  prompt: string[],
  kinds: KindInfo[] = []
): FormField[] {
  if (!kind) return []
  const specs = allSpecs(kind)
  const heading = requiredHeading(kind, prompt)
  const out: FormField[] = []
  for (const name of prompt) {
    const spec = specs.find((s) => s.name === name)
    if (!spec || spec.managed || !ownerWritable(spec)) continue
    out.push(
      fieldOf(
        withResolvedPin(spec, kind, kinds),
        spec.required || name === heading
      )
    )
  }
  return out
}

/** A field for a seeded value shown read-only beside the prompt. */
export function seedField(
  kind: KindInfo | undefined,
  name: string,
  kinds: KindInfo[] = []
): FormField | undefined {
  const spec = kind ? allSpecs(kind).find((s) => s.name === name) : undefined
  return spec && kind
    ? fieldOf(withResolvedPin(spec, kind, kinds), false)
    : undefined
}

/** Minted once per pending create and reused on every retry of it. */
export function newIdempotencyKey(): string {
  return crypto.randomUUID()
}
