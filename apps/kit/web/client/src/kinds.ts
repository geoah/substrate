import { recordKey, referencePath, type SubstrateRecord } from "./records"

/** A kind reference split on its two slashes: the authority is the one
 * segment carrying a dot. */
export interface KindName {
  authority: string
  package: string
  name: string
}

/** A kind reference's parts, or undefined for anything that is not one
 * named in full: `<authority>/<package>/<name>`. */
export function parseKind(reference: string): KindName | undefined {
  const parts = reference.split("/")
  if (parts.length !== 3) return undefined
  const [authority, pkg, name] = parts
  if (!authority.includes(".") || !/^[a-z][a-z0-9]*$/.test(pkg))
    return undefined
  if (!/^[a-z][a-z0-9]*$/.test(name)) return undefined
  return { authority, package: pkg, name }
}

/** A kind an app reads and writes, declared once: its reference, how a
 * record of it reads as the app's own value, and how the app's draft
 * writes as properties. Every hook and write that takes a kind takes one of
 * these, so the reference is spelled in one place. */
export interface Kind<T, D = never> {
  /** The kind's reference, in full. */
  readonly kind: string
  readonly name: KindName
  read(record: SubstrateRecord): T
  /** The properties a create of `draft` writes. */
  write(draft: D): Record<string, unknown>
  is(record: { kind: string }): boolean
  /** A record's key where several kinds share one store. */
  key(id: string): string
  /** The value a reference property to one of its records is written as. */
  ref(id: string): string
}

export interface KindCodec<T, D> {
  read: (record: SubstrateRecord) => T
  write?: (draft: D) => Record<string, unknown>
}

/** Declares a kind. Throws at once for a reference not named in full,
 * which every surface refuses. */
export function defineKind<T, D = never>(
  kind: string,
  codec: KindCodec<T, D>
): Kind<T, D> {
  const name = parseKind(kind)
  if (!name) {
    throw new Error(
      `${kind} is not a kind named in full: <authority>/<package>/<name>.`
    )
  }
  return {
    kind,
    name,
    read: codec.read,
    write: (draft: D) => {
      if (!codec.write) throw new Error(`${kind} declares no write.`)
      return codec.write(draft)
    },
    is: (record) => record.kind === kind,
    key: (id) => recordKey(kind, id),
    ref: (id) => referencePath(kind, id),
  }
}
