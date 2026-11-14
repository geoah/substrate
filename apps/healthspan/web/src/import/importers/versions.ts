// Each importer's version: the plan an import keeps names the version that
// read its files, and the runner writes a plan only by the version that
// made it (docs/plans/healthspan.md, The runner).

import type { BuiltinImporterId, ImporterId } from "./types"

/** The app's own importers' versions. Raise one whenever the same files
 * would be read into other records (a mapping, a key, a unit or a revision
 * corrected), so a plan an older version made is never resumed over what
 * a newer one wrote. */
export const IMPORTER_VERSIONS: Readonly<Record<BuiltinImporterId, number>> = {
  csv: 1,
  manual: 1,
  numan: 1,
  randox: 1,
  dxa: 1,
  nhs: 1,
  mychart: 1,
}

/** An importer's version as this app knows it: the app's own's, else none
 * (another's plan carries its own). */
export function knownVersion(importer: ImporterId): number | undefined {
  return (IMPORTER_VERSIONS as Readonly<Record<string, number>>)[importer]
}
