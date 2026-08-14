/** The DECLARATION kinds, and the two things an editor has to know about them.
 *
 * A declaration record IS the vocabulary: its properties are the manifest, and
 * its id is the declaration's NAME. That makes two ordinary create affordances
 * wrong for these nine kinds and only these nine.
 *
 * The id is not optional. Every other create may leave `metadata.id` blank and
 * let the substrate mint one; a declaration write is refused without it,
 * because there is no id to mint: the id is what the registry resolves
 * (`engine/vocabularywrite.go`, `putSchemaRecord`).
 *
 * The authority is not asked. A declaration's `authority` property is the label
 * in front of the slash of its own id, so asking for it twice is asking
 * somebody to agree with themselves. The actor kind is the exception: its id is
 * a bare name (`console`), and the authority it belongs to is genuinely
 * authored beside it.
 *
 * The set mirrors `vocabularyRecordKinds` (engine/vocabularywrite.go). */

import { CORE_AUTHORITY } from "@/lib/api/http"

export const ACTOR_KIND = `${CORE_AUTHORITY}/actor`
export const AGENT_KIND = `${CORE_AUTHORITY}/agent`
export const AUTHORITY_KIND = `${CORE_AUTHORITY}/authority`
export const BUNDLE_KIND = `${CORE_AUTHORITY}/bundle`
export const FUNCTION_KIND = `${CORE_AUTHORITY}/function`
export const KIND_KIND = `${CORE_AUTHORITY}/kind`
export const PROPERTY_TYPE_KIND = `${CORE_AUTHORITY}/propertytype`
export const RECORD_MAPPING_KIND = `${CORE_AUTHORITY}/recordmapping`
export const TRAIT_KIND = `${CORE_AUTHORITY}/trait`

export const DECLARATION_KINDS: readonly string[] = [
  ACTOR_KIND,
  AGENT_KIND,
  AUTHORITY_KIND,
  BUNDLE_KIND,
  FUNCTION_KIND,
  KIND_KIND,
  PROPERTY_TYPE_KIND,
  RECORD_MAPPING_KIND,
  TRAIT_KIND,
]

export function isDeclarationKind(identity: string): boolean {
  return DECLARATION_KINDS.includes(identity)
}

/** How this declaration kind's id is SPELLED, as a placeholder. An actor is one
 * bare name; an authority and a bundle are named by the DNS-style label they
 * own; everything else is `<authority>/<name>`. */
export function declarationIdShape(identity: string): string {
  switch (identity) {
    case ACTOR_KIND:
      return "<name>"
    case AUTHORITY_KIND:
    case BUNDLE_KIND:
      return "<authority>"
    default:
      return "<authority>/<name>"
  }
}

/** The property every declaration carries naming who publishes it. Spelled
 * once here because two surfaces reach for it by name. */
export const AUTHORITY_PROPERTY = "authority"

/** Whether this kind's `authority` property is the id's to say. True for every
 * declaration kind but the actor, whose id is a bare name and whose authority
 * is therefore genuinely authored. */
export function authorityIsDerived(identity: string): boolean {
  return isDeclarationKind(identity) && identity !== ACTOR_KIND
}

/** The authority a declaration's id puts it under: everything in front of the
 * slash, which for a kind whose id IS a label (an authority, a bundle) is the
 * whole id. `undefined` where the id does not say: an actor names itself, and
 * a blank id says nothing at all. */
export function derivedAuthority(
  identity: string,
  id: string
): string | undefined {
  if (!authorityIsDerived(identity)) return undefined
  const label = id.trim().split("/")[0]
  return label || undefined
}
