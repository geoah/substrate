/** The DECLARATION kinds, and the two things an editor has to know about them.
 *
 * A declaration record IS the vocabulary: its properties are the manifest, and
 * its id is the declaration's NAME. That makes two ordinary create affordances
 * wrong for these ten kinds and only these ten.
 *
 * The id is not optional. Every other create may leave `metadata.id` blank and
 * let the substrate mint one; a declaration write is refused without it,
 * because there is no id to mint: the id is what the registry resolves
 * (`engine/vocabularywrite.go`, `putSchemaRecord`).
 *
 * The authority and the package are not asked. A declaration's `authority` and
 * `package` properties are the first two segments of its own id, so asking for
 * them twice is asking somebody to agree with themselves. The actor kind is the
 * exception: its id is a bare name (`console`), and the authority it belongs to
 * is genuinely authored beside it.
 *
 * The set mirrors `vocabularyRecordKinds` (engine/vocabularywrite.go). */

import { CORE_PACKAGE } from "@/lib/api/http"

export const ACTOR_KIND = `${CORE_PACKAGE}/actor`
export const AGENT_KIND = `${CORE_PACKAGE}/agent`
export const AUTHORITY_KIND = `${CORE_PACKAGE}/authority`
export const BUNDLE_KIND = `${CORE_PACKAGE}/bundle`
export const FUNCTION_KIND = `${CORE_PACKAGE}/function`
export const KIND_KIND = `${CORE_PACKAGE}/kind`
export const PACKAGE_KIND = `${CORE_PACKAGE}/package`
export const PROPERTY_TYPE_KIND = `${CORE_PACKAGE}/propertytype`
export const RECORD_MAPPING_KIND = `${CORE_PACKAGE}/recordmapping`
export const TRAIT_KIND = `${CORE_PACKAGE}/trait`

export const DECLARATION_KINDS: readonly string[] = [
  ACTOR_KIND,
  AGENT_KIND,
  AUTHORITY_KIND,
  BUNDLE_KIND,
  FUNCTION_KIND,
  KIND_KIND,
  PACKAGE_KIND,
  PROPERTY_TYPE_KIND,
  RECORD_MAPPING_KIND,
  TRAIT_KIND,
]

export function isDeclarationKind(identity: string): boolean {
  return DECLARATION_KINDS.includes(identity)
}

/** How this declaration kind's id is SPELLED, as a placeholder. An actor is one
 * bare name; an authority is the DNS-style label it owns; a bundle and a
 * package are named by the package identity they own; everything else is a kind
 * reference. */
export function declarationIdShape(identity: string): string {
  switch (identity) {
    case ACTOR_KIND:
      return "<name>"
    case AUTHORITY_KIND:
      return "<authority>"
    case BUNDLE_KIND:
    case PACKAGE_KIND:
      return "<authority>/<package>"
    default:
      return "<authority>/<package>/<name>"
  }
}

/** The property every declaration carries naming who publishes it. Spelled
 * once here because two surfaces reach for it by name. */
export const AUTHORITY_PROPERTY = "authority"

/** The property every declaration but the authority's own carries naming the
 * package it is owned and versioned in. */
export const PACKAGE_PROPERTY = "package"

/** Whether this kind's `authority` property is the id's to say. True for every
 * declaration kind but the actor, whose id is a bare name and whose authority
 * is therefore genuinely authored. */
export function authorityIsDerived(identity: string): boolean {
  return isDeclarationKind(identity) && identity !== ACTOR_KIND
}

/** Whether this kind's `package` property is the id's to say. The authority
 * declaration is the exception the other way: its id is one label and it
 * belongs to no package. */
export function packageIsDerived(identity: string): boolean {
  return authorityIsDerived(identity) && identity !== AUTHORITY_KIND
}

/** The authority a declaration's id puts it under: everything in front of the
 * first slash, which for a kind whose id IS a label (an authority) is the whole
 * id. `undefined` where the id does not say: an actor names itself, and a blank
 * id says nothing at all. */
export function derivedAuthority(
  identity: string,
  id: string
): string | undefined {
  if (!authorityIsDerived(identity)) return undefined
  const label = id.trim().split("/")[0]
  return label || undefined
}

/** The package a declaration's id puts it in: the SECOND segment of the id.
 * `undefined` where the id does not say it yet — an authority declaration has
 * no package, and a half-typed id has no second segment. */
export function derivedPackage(
  identity: string,
  id: string
): string | undefined {
  if (!packageIsDerived(identity)) return undefined
  const word = id.trim().split("/")[1]
  return word || undefined
}
