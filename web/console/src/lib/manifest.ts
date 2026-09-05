/** The record page's manifest: the JSON the wire serves, folded back into the
 * v1 document envelope the console and the CLI both speak (`kind`, `metadata`,
 * `data`, `status`) and serialized as YAML.
 *
 * `kind` is the record's kind REFERENCE — one key names the authority, the
 * package and the name. `metadata` carries the id and the two authored key/value blocks
 * (`labels`, `annotations`); `data` is everything authored (`properties`);
 * `status` is server-owned — version, stamps, and the §7.1 property
 * provenance (`status.properties`: manager + offers). A pointer at another
 * record is a `reference` property, so it rides inside `properties` as the
 * referent's `<kind>/<id>` path. */

import { stringify } from "yaml"

import { CORE_AUTHORITY, CORE_PACKAGE_NAME, joinKind } from "@/lib/api/http"
import {
  readReference,
  type SubstrateRecord,
  type KindInfo,
} from "@/lib/api/types"
import { declaredReferences, kindByIdentity } from "@/lib/definition"
import { splitRecordPath } from "@/lib/record-path"
import type { YamlLinkTargets } from "@/lib/yaml-annotations"

/** The meta-kind every declaration is a record of. */
const KIND_META_KIND = joinKind(CORE_AUTHORITY, CORE_PACKAGE_NAME, "kind")

/** Order-preserving object build: yaml serializes insertion order. */
export function manifestOf(record: SubstrateRecord): Record<string, unknown> {
  const metadata: Record<string, unknown> = { id: record.id }
  if (Object.keys(record.labels ?? {}).length) metadata.labels = record.labels
  if (record.annotations && Object.keys(record.annotations).length) {
    metadata.annotations = record.annotations
  }

  const data: Record<string, unknown> = {}
  if (Object.keys(record.properties ?? {}).length) {
    data.properties = record.properties
  }
  const status: Record<string, unknown> = {
    version: record.version,
    createdAt: record.createdAt,
    updatedAt: record.updatedAt,
  }
  if (record.deletedAt) status.deletedAt = record.deletedAt
  if (record.canonicalId) status.canonicalId = record.canonicalId
  if (record.formerIds?.length) status.formerIds = record.formerIds
  if (record.propertyMeta && Object.keys(record.propertyMeta).length) {
    status.properties = record.propertyMeta
  }

  const doc: Record<string, unknown> = { kind: record.kind, metadata }
  if (Object.keys(data).length) doc.data = data
  doc.status = status
  return doc
}

const YAML_OPTIONS = {
  lineWidth: 0,
  defaultStringType: "PLAIN",
  defaultKeyType: "PLAIN",
} as const

export function manifestYAML(record: SubstrateRecord): string {
  return stringify(manifestOf(record), YAML_OPTIONS)
}

/** The reading order a schema file authors its declaration in. jsonb lost the
 * stored key order (schema.ts's note), so the definition view re-imposes the
 * authored one instead of serving an alphabetical scramble; anything the
 * substrate grows later falls in after, alphabetically. */
const DECLARATION_ORDER = [
  "authority",
  "package",
  "names",
  "version",
  "displayTemplate",
  "traits",
  "properties",
]

/** The kind's declaration folded back into the document that declared it —
 * the same v1 envelope as the record manifest, with the meta-kind on top and
 * the kind REFERENCE as the id, exactly as
 * `kinds/<authority>/<package>/<name>.yaml` authors it. Comments are gone by design (record 61: the parsed definition is
 * the document, not its source text). */
export function kindManifestOf(kind: KindInfo): Record<string, unknown> {
  const definition = kind.definition ?? {}
  const data: Record<string, unknown> = {}
  for (const key of DECLARATION_ORDER) {
    if (key in definition) data[key] = definition[key]
  }
  for (const key of Object.keys(definition).sort()) {
    if (!(key in data)) data[key] = definition[key]
  }
  return {
    kind: KIND_META_KIND,
    metadata: { id: kind.identity },
    data,
  }
}

export function kindManifestYAML(kind: KindInfo): string {
  return stringify(kindManifestOf(kind), YAML_OPTIONS)
}

/** A kind's browse collection address. The console's data routes mirror the
 * API's collection paths — `/data/{authority}/{package}/{kind}`, and every
 * kind carries all three (decisions 0042 and 0047). */
function collectionHref(kind: KindInfo): string {
  return `/data/${kind.authority}/${kind.package}/${kind.name}`
}

/** Every registry kind by its reference, so any `kind:` a document carries
 * links to that collection. The whole link vocabulary of a DECLARATION view —
 * a declaration names kinds, never record ids. */
export function kindLinkTargets(kinds: KindInfo[]): YamlLinkTargets {
  const kindLinks: Record<string, string> = {}
  for (const k of kinds) kindLinks[k.identity] = collectionHref(k)
  return { ids: {}, kinds: kindLinks }
}

/** The manifest's known references, each with its console address: the record
 * PATHS its reference properties carry, canonicalId/formerIds (the API
 * resolves former ids to the canonical row), the `kind:` references the record
 * actually carries, and every registry kind. Feeds the YAML view's linkified
 * spans. */
export function linkTargetsOf(
  record: SubstrateRecord,
  kinds: KindInfo[]
): YamlLinkTargets {
  const ids: Record<string, string> = {}
  const kindLinks: Record<string, string> = {}

  /** The collection href of a known kind, registering the kind's own link on
   * the way; `undefined` for a kind this repository has not installed. */
  function hrefFor(ref: string): string | undefined {
    const info = kindByIdentity(kinds, ref)
    if (!info) return undefined
    const href = collectionHref(info)
    kindLinks[ref] = href
    return href
  }

  function addFor(ref: string, id?: string) {
    const href = hrefFor(ref)
    if (href && id) ids[id] = `${href}/${id}`
  }

  // Reference-typed property values are typed POINTERS: each stored record
  // PATH deep-links to the referent's detail page. It is keyed by the whole
  // path because that is the text the document carries — the id alone never
  // appears in it.
  const own = kindByIdentity(kinds, record.kind)
  if (own) {
    for (const prop of declaredReferences(own)) {
      const raw = record.properties?.[prop.name]
      const refs = Array.isArray(raw) ? raw : raw == null ? [] : [raw]
      for (const ref of refs) {
        const held = readReference(ref)
        if (!held) continue
        const target = splitRecordPath(held.path)
        if (!target) continue
        const href = hrefFor(target.kind)
        if (href) ids[held.path] = `${href}/${target.id}`
      }
    }
  }

  addFor(record.kind)
  if (own) {
    const collection = collectionHref(own)
    if (record.canonicalId) {
      ids[record.canonicalId] = `${collection}/${record.canonicalId}`
    }
    for (const former of record.formerIds ?? []) {
      ids[former] = `${collection}/${former}`
    }
  }

  // Every registry kind, so any kind reference the document carries links.
  for (const k of kinds) kindLinks[k.identity] = collectionHref(k)

  return { ids, kinds: kindLinks }
}
