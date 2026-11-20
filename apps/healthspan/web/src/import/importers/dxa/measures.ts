// What a GE Lunar printout measures, and what each is written as: the shared
// metric, the unit as printed, the scanner's own code and name kept as the
// result's coding (apps/healthspan/docs/importers/04-dxa.md, What it writes).

/** A scan's date, `YYYY-MM-DD`: its report's key. */
export type Day = string

/** A body region, written as a regional result's `channel`. */
export type Region =
  | "head"
  | "left_arm"
  | "left_leg"
  | "left_trunk"
  | "left_total"
  | "right_arm"
  | "right_leg"
  | "right_trunk"
  | "right_total"
  | "arms"
  | "legs"
  | "trunk"
  | "ribs"
  | "pelvis"
  | "spine"
  | "android"
  | "gynoid"
  | "total"

/** The bone density table's region labels, by the prefix each starts
 * with, the first that fits taken (`Σύνολο` last, after the left and right
 * totals). */
export const BMD_LABELS: readonly (readonly [string, Region])[] = [
  ["Κεφαλή", "head"],
  ["Αριστερός βραχίονας", "left_arm"],
  ["Αριστερό σκέλος", "left_leg"],
  ["Αριστερός κορμός", "left_trunk"],
  ["Σύνολο αριστερών", "left_total"],
  ["Δεξιός βραχίονας", "right_arm"],
  ["Δεξιό σκέλος", "right_leg"],
  ["Δεξιός κορμός", "right_trunk"],
  ["Σύνολο δεξιών", "right_total"],
  ["Βραχίονες", "arms"],
  ["Κνήμες", "legs"],
  ["Κορμός", "trunk"],
  ["Πλευρές", "ribs"],
  ["Πύελος", "pelvis"],
  ["Σπονδ", "spine"],
  ["Σύνολο", "total"],
]

/** A bone density label's region, or undefined for one not in the table. */
export function bmdRegion(label: string): Region | undefined {
  return BMD_LABELS.find(([prefix]) => label.startsWith(prefix))?.[1]
}

/** The composition table's rows, in the order the scanner prints them; its
 * labels are abbreviated or cut short, so a row is its place. */
export const COMPOSITION_ROWS: readonly Region[] = [
  "left_arm",
  "left_leg",
  "left_trunk",
  "left_total",
  "right_arm",
  "right_leg",
  "right_trunk",
  "right_total",
  "arms",
  "legs",
  "trunk",
  "android",
  "gynoid",
  "total",
]

/** A whole-body figure, as a scan's own printout or a later scan's trend
 * table gives it. */
export type WholeBody =
  | "body_mass"
  | "body_fat"
  | "tissue_fat"
  | "fat_mass"
  | "lean_mass"
  | "fat_free_mass"
  | "bone_mass"
  | "android_fat"
  | "gynoid_fat"
  | "ag_ratio"
  | "bmd_total"
  | "fat_centile"

/** How a figure is written. */
export interface Measure {
  /** The shared metric. */
  metric: string
  /** The unit as the printout prints it. */
  unit: string
  /** The scanner's code for it, the result's `coding.code`. */
  code: string
  /** Its name, the result's `coding.display`. */
  name: string
  /** The region, for one the trend table gives of a region. */
  channel?: Region
}

/** The whole-body figures, in the order a report's results list them. */
export const WHOLE_BODY: readonly (readonly [WholeBody, Measure])[] = [
  [
    "body_mass",
    {
      metric: "health/body_mass",
      unit: "kg",
      code: "Total mass",
      name: "Total mass",
    },
  ],
  [
    "body_fat",
    {
      metric: "health/body_fat_fraction",
      unit: "%",
      code: "Region %Fat (total)",
      name: "Region %Fat (total)",
    },
  ],
  [
    "tissue_fat",
    {
      metric: "health/tissue_fat_fraction",
      unit: "%",
      code: "Tissue %Fat (total)",
      name: "Tissue %Fat (total)",
    },
  ],
  [
    "fat_mass",
    {
      metric: "health/fat_mass",
      unit: "g",
      code: "Fat (total)",
      name: "Fat (total)",
    },
  ],
  [
    "lean_mass",
    {
      metric: "health/lean_mass",
      unit: "g",
      code: "Lean (total)",
      name: "Lean (total)",
    },
  ],
  [
    "fat_free_mass",
    {
      metric: "health/fat_free_mass",
      unit: "g",
      code: "Fat free (total)",
      name: "Fat free (total)",
    },
  ],
  [
    "bone_mass",
    {
      metric: "health/bone_mass",
      unit: "g",
      code: "BMC (total)",
      name: "BMC (total)",
    },
  ],
  [
    "android_fat",
    {
      metric: "health/tissue_fat_fraction",
      unit: "%",
      code: "Android %Fat",
      name: "Android %Fat",
      channel: "android",
    },
  ],
  [
    "gynoid_fat",
    {
      metric: "health/tissue_fat_fraction",
      unit: "%",
      code: "Gynoid %Fat",
      name: "Gynoid %Fat",
      channel: "gynoid",
    },
  ],
  [
    "ag_ratio",
    {
      metric: "health/android_gynoid_ratio",
      unit: "1",
      code: "A/G ratio",
      name: "A/G ratio",
    },
  ],
  [
    "bmd_total",
    {
      metric: "health/bone_mineral_density",
      unit: "g/cm2",
      code: "BMD (total body)",
      name: "BMD (total body)",
    },
  ],
  [
    "fat_centile",
    {
      metric: "health/body_fat_centile",
      unit: "{score}",
      code: "Centile (US reference, v107)",
      name: "Centile (US reference, v107)",
    },
  ],
]

/** The composition table's regional values, each written for a region in
 * its channel. */
export type RegionalValue = "tissueFat" | "regionFat" | "lean" | "fat" | "bmc"

/** Each regional value's measure: its code and name are written with the
 * region after them (`reg_tissue_fat/left_arm`, `Tissue %Fat left_arm`). */
export const REGIONAL: readonly (readonly [RegionalValue, Measure])[] = [
  [
    "tissueFat",
    {
      metric: "health/tissue_fat_fraction",
      unit: "%",
      code: "reg_tissue_fat",
      name: "Tissue %Fat",
    },
  ],
  [
    "regionFat",
    {
      metric: "health/body_fat_fraction",
      unit: "%",
      code: "reg_fat_fraction",
      name: "Region %Fat",
    },
  ],
  [
    "lean",
    {
      metric: "health/lean_mass",
      unit: "g",
      code: "reg_lean_mass",
      name: "Lean",
    },
  ],
  [
    "fat",
    { metric: "health/fat_mass", unit: "g", code: "reg_fat_mass", name: "Fat" },
  ],
  [
    "bmc",
    { metric: "health/bone_mass", unit: "g", code: "reg_bmc", name: "BMC" },
  ],
]

/** A region's bone mineral density, its code and name with the region
 * after them as the regional values'. */
export const REGIONAL_BMD: Measure = {
  metric: "health/bone_mineral_density",
  unit: "g/cm2",
  code: "reg_bmd",
  name: "BMD",
}

/** The total's T-score and Z-score, as the bone density table prints them
 * in its T-Score and Z-Score columns. */
export const T_SCORE: Measure = {
  metric: "health/bmd_t_score",
  unit: "{score}",
  code: "bmd_t_score",
  name: "T-score (total body, young adult)",
}
export const Z_SCORE: Measure = {
  metric: "health/bmd_z_score",
  unit: "{score}",
  code: "bmd_z_score",
  name: "Z-score (total body, age matched)",
}

/** The fat mass ratios, in the order the printout prints them. */
export const RATIOS: readonly Measure[] = [
  {
    metric: "health/trunk_to_total_fat_ratio",
    unit: "1",
    code: "fat_ratio_trunk_total",
    name: "Fat mass ratio trunk/total",
  },
  {
    metric: "health/legs_to_total_fat_ratio",
    unit: "1",
    code: "fat_ratio_legs_total",
    name: "Fat mass ratio legs/total",
  },
  {
    metric: "health/limbs_to_trunk_fat_ratio",
    unit: "1",
    code: "fat_ratio_limbs_trunk",
    name: "Fat mass ratio (arms+legs)/trunk",
  },
]

/** A result's key within its report: the measure (the shared metric's
 * name), then the region where it has one. */
export function resultPart(metric: string, channel?: string): string {
  const name = metric.slice(metric.indexOf("/") + 1)
  return channel ? `${name}/${channel}` : name
}
