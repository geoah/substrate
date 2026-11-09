// What a Numan export holds, as Effect Schemas (apps/healthspan/docs/
// importers/01-numan.md, What it reads): the export a JSON:API document of
// the owner's tests, and each test's `results_raw`, the lab's own rows as a
// JSON string, in one of three shapes. Only the fields the importer reads
// are declared; every other field is kept in the test's own JSON, which the
// report keeps as its file.

import { Schema } from "effect"

const Text = Schema.NullOr(Schema.String)
const optionalText = Schema.optional(Text)

/** One of Numan's own measurements, which carries its clinical range. */
export const Measurement = Schema.Struct({
  identifier_short: Schema.String,
  /** The value and its unit, `2.9 mmol/L`. */
  value: optionalText,
  /** Numan's clinical range, `0.0 - 3.0`, `< 5.2`, `> 1.0`. */
  normal_value: optionalText,
})
export type Measurement = typeof Measurement.Type

/** How a test's sample was taken: `in-clinic` and `venous`, `tasso` and
 * `capillary`. */
const Collection = Schema.Struct({
  collection_detail: optionalText,
  collection_method: optionalText,
})

export const Test = Schema.Struct({
  id: Schema.String,
  type: Schema.optional(Schema.String),
  attributes: Schema.Struct({
    short_id: Schema.String,
    status: Text,
    created_at: Schema.String,
    updated_at: optionalText,
    /** London wall-clock time written with a `Z` it does not mean. */
    sample_taken_date: optionalText,
    sample_accessioned_date: optionalText,
    results_received_at: optionalText,
    /** The lab's own rows, a JSON document as a string. */
    results_raw: Schema.String,
    results: Schema.Array(Collection),
    observations_measurements: Schema.optional(
      Schema.NullOr(Schema.Array(Measurement))
    ),
  }),
  relationships: Schema.optional(
    Schema.NullOr(
      Schema.Struct({
        medical_test_review: Schema.optional(
          Schema.NullOr(
            Schema.Struct({
              data: Schema.optional(
                Schema.NullOr(Schema.Struct({ id: Schema.String }))
              ),
            })
          )
        ),
      })
    )
  ),
})
export type Test = typeof Test.Type

/** A resource the export includes beside its tests: a test's clinical
 * review among them. */
const Included = Schema.Struct({
  id: Schema.optional(Schema.String),
  type: Schema.optional(Schema.String),
  attributes: Schema.optional(
    Schema.NullOr(
      Schema.Struct({
        created_at: optionalText,
        updated_at: optionalText,
      })
    )
  ),
})
export type Included = typeof Included.Type

/** A Numan export: the tests, the resources they name (`included`, each an
 * object or a list of them), and the page it is of. */
export const NumanExport = Schema.Struct({
  data: Schema.Array(Test),
  included: Schema.optional(
    Schema.NullOr(
      Schema.Array(Schema.Union([Included, Schema.Array(Included)]))
    )
  ),
  meta: Schema.optional(
    Schema.NullOr(
      Schema.Struct({
        pagination: Schema.optional(
          Schema.NullOr(
            Schema.Struct({
              current_page: Schema.optional(Schema.Unknown),
              last_page: Schema.optional(Schema.Unknown),
              next_page: Schema.optional(Schema.Unknown),
            })
          )
        ),
      })
    )
  ),
})
export type NumanExport = typeof NumanExport.Type

/** One of Randox's rows, as Numan relays them. */
export const RandoxRow = Schema.Struct({
  analyte: Schema.String,
  displayName: optionalText,
  result: Text,
  units: Text,
  refLow: optionalText,
  refHigh: optionalText,
  /** `H` or `L`, or none. */
  lowHigh: optionalText,
  sampleType: optionalText,
  dateOfReport: optionalText,
})
export type RandoxRow = typeof RandoxRow.Type

const Pressure = Schema.optional(
  Schema.NullOr(Schema.Union([Schema.Number, Schema.String]))
)

/** A Randox order: its rows, and the blood pressure the clinic measured. */
export const RandoxOrder = Schema.Struct({
  orderCreatedDate: optionalText,
  patientSystolicBloodPressure: Pressure,
  patientDiastolicBloodPressure: Pressure,
  reportResults: Schema.Array(RandoxRow),
})
export type RandoxOrder = typeof RandoxOrder.Type

export const RandoxRows = Schema.Array(RandoxRow)

/** One of Hurdle's rows: a biomarker, its result, its interpretation, and
 * the rule each interpretation holds for (`{"NORMAL": ["<3.0"]}`). */
export const HurdleRow = Schema.Struct({
  biomarker: Schema.Struct({
    code: Schema.String,
    name: Text,
    unit: Text,
  }),
  result: Text,
  interpretation: optionalText,
  interpretationData: Schema.optional(
    Schema.NullOr(Schema.Struct({ value: Schema.optional(Schema.Unknown) }))
  ),
  /** Hurdle's id of the whole test, the same on every row of it. */
  testId: optionalText,
})
export type HurdleRow = typeof HurdleRow.Type

export const HurdleRows = Schema.Array(HurdleRow)
