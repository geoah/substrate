// The importers' one interface (docs/plans/healthspan.md, Importers): an
// importer reads what the owner hands it into a plan, and the one runner
// writes every importer's plan. Reading is pure and written with Effect;
// the import screen crosses into it with `Effect.runPromise` alone.

import type { Effect } from "effect"
import type { MetricRecord } from "../../catalogue/types"
import type { UnitGraph } from "../../labs/units"

export type ImporterId =
  "csv" | "manual" | "numan" | "randox" | "dxa" | "nhs" | "mychart"

/** One file the owner handed in: a picked file, or one file of a picked
 * folder or zip, by its path inside it (`items/visit/12.json`). */
export interface InputFile {
  /** The path inside the folder or zip, `/`-separated, no leading `/`; a
   * picked file's own name. */
  path: string
  size: number
  /** The media type the browser or the archive gives, "" when none. */
  type: string
  bytes(): Promise<Uint8Array>
  text(): Promise<string>
}

/** What the owner handed in, as one set of files. */
export interface Input {
  /** The folder's, the zip's or the single file's name, for the import's
   * `name`. */
  name: string
  files: readonly InputFile[]
}

/** How sure an importer is that an input is its own. */
export interface Match {
  importer: ImporterId
  /** 0 to 1: 1 when the input carries the provider's own marks (a
   * manifest naming its fetcher, the export's schema), lower on a guess. */
  confidence: number
  /** One sentence the import screen can show: "A Numan export of 14
   * tests". */
  why: string
}

/** Something the files do not say that the import needs, asked once on
 * the import screen and kept on the record it names (the scanning
 * centre's name, a hospital's ODS code). */
export interface Question {
  key: string
  label: string
  help?: string
  kind: "text" | "timezone"
  /** A suggestion, from the data or a record already in the repository. */
  suggested?: string
  required: boolean
}

/** A source of measured values as the repository holds it. */
export interface KnownSource {
  /** The id after `observations/source/`. */
  id: string
  name: string
  class?: string
}

/** What reading needs from the repository and the owner. */
export interface Context {
  today: Date
  /** Every `observations/metric` record of the repository. */
  metrics: readonly MetricRecord[]
  units: UnitGraph
  sources: readonly KnownSource[]
  /** The owner's contact, where the setting names one: an importer that
   * writes a patient record links it through `about`. */
  ownerContact?: string
  /** The answers to the importer's questions, by `Question.key`. */
  answers: Readonly<Record<string, string>>
}

/** Where a planned record lives: its id is `DerivedMintedID` of
 * `healthspan/<namespace>/<kind name>/<key>`, the runner's to derive. */
export interface RecordKey {
  /** The kind in full, `substrate.reamde.dev/labs/labreport`. */
  kind: string
  /** The provider's account (`numan`, `dxa/<centre>`, `mychart/<host>`),
   * `ods` for an organization, `source` for a provider's source. */
  namespace: string
  /** The key from the provider's data (docs/plans/healthspan.md, the
   * identity table). */
  key: string
}

/** A reference to another planned record (or one already written under
 * the same key), resolved to its record path when the plan is written.
 * One no import has written is left out, never written dangling. */
export interface PlannedRef {
  $ref: RecordKey
}

/** A document's place in a property, resolved to its digest. */
export interface PlannedDocRef {
  $doc: string
}

/** How the runner treats a record already there:
 * - `maintain`: the provider decision by revision (created, unchanged,
 *   updated, stale), every property the importer writes maintained;
 * - `create`: written where absent, never again (a provider's source);
 * - `merge`: an organization: identifiers unioned, a field filled only
 *   where empty, the revision raised only upward, never stale. */
export type WriteMode = "maintain" | "create" | "merge"

/** One record the plan writes. Properties are as the kind declares them;
 * a reference to a planned record is a `PlannedRef`, a document a
 * `PlannedDocRef`, wherever either appears (also inside arrays and
 * objects). */
export interface PlannedRecord extends RecordKey {
  mode: WriteMode
  properties: Record<string, unknown>
  /** Properties written when the record is created and never again (a
   * report's `review`). */
  createOnly?: readonly string[]
  /** The provider's revision, seconds since the epoch; absent for a
   * `create` record. */
  revision?: number
}

/** A file the plan stores and a record names. */
export interface PlannedDocument {
  /** `blob-sha256-<hex>` of `bytes`. */
  digest: string
  /** A neutral name, `<provider> <kind> <date>.<ext>`: never the provider's
   * file name, which can carry the owner's name and birth date. */
  name: string
  mediaType: string
  bytes: Uint8Array
}

/** Something the importer left out, and why, for the plan's preview and
 * the import's `skipped`. */
export interface LeftOut {
  /** Where: a file path, a test, a row. */
  what: string
  why: string
}

/** What an importer read: the records in write order (a record after
 * every record it names), the documents they name, and what it left out. */
export interface Plan {
  importer: ImporterId
  /** The import's name: the file's, the folder's, the zip's. */
  name: string
  /** The provider's own source, which the import names as `source`; its
   * record is among `records` with mode `create`. */
  source?: RecordKey
  records: PlannedRecord[]
  documents: PlannedDocument[]
  left: LeftOut[]
}

/** Why an importer could not read an input. */
export type ImportError =
  /** A file is not what the provider promised: the field named. */
  | { _tag: "Unreadable"; path: string; field?: string; message: string }
  /** The input is not this importer's. */
  | { _tag: "NotMine"; message: string }
  /** The importer needs answers to these first. */
  | { _tag: "NeedsAnswers"; questions: readonly Question[] }

export interface Importer {
  id: ImporterId
  /** What the import screen offers it as: "Numan export". */
  name: string
  /** One sentence on what to hand it and where to get it. */
  description: string
  /** What the screen asks for: one file, several, or a folder (a zip of
   * the folder is unpacked into the same input). */
  accepts: { kind: "file" | "files" | "folder"; extensions?: readonly string[] }
  /** How sure it is that an input is its own; undefined when it is not. */
  detect(input: Input): Effect.Effect<Match | undefined>
  /** The questions this input needs answered before `read`. */
  questions(input: Input, ctx: Context): Effect.Effect<readonly Question[]>
  read(input: Input, ctx: Context): Effect.Effect<Plan, ImportError>
}
