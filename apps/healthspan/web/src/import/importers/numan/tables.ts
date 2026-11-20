// Numan's codes and words, as data (apps/healthspan/docs/importers/
// 01-numan.md): each lab's analyte code to the catalogue entry it is, the
// code under which Numan keeps its own clinical range for an entry, and how
// a sample was taken in words.

/** Randox's analyte codes, as Numan relays them, to catalogue entries. */
export const RANDOX_ANALYTES: Readonly<Record<string, string>> = {
  Albumin: "albumin",
  BasophilCount: "basophils",
  "Cholesterol / HDL Ratio": "cholesterol_hdl_ratio",
  EosinophilCount: "eosinophils",
  Estradiol: "estradiol",
  FollicleStimulatingHormone: "fsh",
  "Free Testosterone": "free_testosterone",
  "Hb A1C": "hba1c",
  HCTRatio: "haematocrit",
  HDLCholesterol: "hdl_cholesterol",
  HGB: "haemoglobin",
  LDLCholesterol: "ldl_cholesterol",
  LuteinisingHormone: "lh",
  LymphocyteCount: "lymphocytes",
  MCV: "mcv",
  MeanCellHaemConc: "mchc",
  MeanCellHaemoglobin: "mch",
  MonocyteCount: "monocytes",
  NeutrophilCount: "neutrophils",
  "Non HDL cholesterol": "non_hdl_cholesterol",
  Platelets: "platelets",
  Prolactin: "prolactin",
  RedBloodCells: "rbc",
  SHBG: "shbg",
  Testosterone: "testosterone",
  TotalCholesterol: "total_cholesterol",
  TPSA: "psa",
  Triglyceride: "triglycerides",
  WhiteBloodCells: "wbc",
  "25-OHVitaminD": "vitamin_d",
  AlanineAminotransferase: "alt",
  AlkalinePhosphataseA: "alp",
  Creatinine: "creatinine",
  eGFR: "egfr",
  Ferritin: "ferritin",
  FolicAcid: "folate",
  FT4: "free_t4",
  GammaGTP: "ggt",
  ThyroidStimulatingHormone: "tsh",
  Urea: "urea",
  VitaminB12: "vitamin_b12",
  AspartateTransferase: "ast",
  FreeAndrogenIndex: "free_androgen_index",
  Globulin: "globulin",
  TotalBilirubin: "bilirubin",
  TotalProtein: "total_protein",
  Magnesium: "magnesium",
}

/** Hurdle's biomarker codes to catalogue entries. */
export const HURDLE_BIOMARKERS: Readonly<Record<string, string>> = {
  TSH: "tsh",
  CREA: "creatinine",
  CHDD: "cholesterol_hdl_ratio",
  FT4: "free_t4",
  TRI: "triglycerides",
  FERR: "ferritin",
  ALB: "albumin",
  UREA: "urea",
  TEST: "testosterone",
  HDL: "hdl_cholesterol",
  ALT: "alt",
  CHO: "total_cholesterol",
  LDL: "ldl_cholesterol",
  ALP: "alp",
  EGFR: "egfr",
  GGT: "ggt",
  HBA1C: "hba1c",
  VIDC: "vitamin_d",
  B12: "vitamin_b12",
  FOL: "folate",
}

/** The blood pressure a Randox order carries, measured at the clinic, to
 * catalogue entries. */
export const ORDER_PRESSURES: readonly (readonly [
  field: "patientSystolicBloodPressure" | "patientDiastolicBloodPressure",
  entry: string,
])[] = [
  ["patientSystolicBloodPressure", "systolic_blood_pressure"],
  ["patientDiastolicBloodPressure", "diastolic_blood_pressure"],
]

/** The method of the blood pressure a Randox order carries. */
export const ORDER_PRESSURE_METHOD =
  "measured at the clinic, sent with the order"

/** The code Numan keeps its own clinical range of an entry under, in a
 * test's `observations_measurements[].identifier_short`. */
export const CLINICAL_CODES: Readonly<Record<string, string>> = {
  albumin: "ALB",
  alp: "ALP",
  alt: "ALT",
  ast: "AST",
  vitamin_b12: "B12",
  basophils: "BASOPHILS",
  bilirubin: "BILI",
  hba1c: "B_HBA1C",
  cholesterol_hdl_ratio: "CHDD",
  total_cholesterol: "CHO",
  creatinine: "CREA",
  eosinophils: "EOSINOPHILS",
  estradiol: "ESTRADIOL",
  free_androgen_index: "FAI",
  ferritin: "FERR",
  folate: "FOLA",
  free_testosterone: "FREE_TESTOSTERONE",
  fsh: "FSH",
  free_t4: "FT4",
  egfr: "GFR",
  ggt: "GGT",
  globulin: "GLOB",
  haematocrit: "HAEMATOCRIT",
  haemoglobin: "HAEMOGLOBIN",
  hdl_cholesterol: "HDL",
  ldl_cholesterol: "LDL",
  lh: "LH",
  lymphocytes: "LYMPHOCYTES",
  mch: "MCH",
  mchc: "MCHC",
  mcv: "MCV",
  magnesium: "MG",
  monocytes: "MONOCYTES",
  neutrophils: "NEUTROPHILS",
  non_hdl_cholesterol: "NHDF",
  platelets: "PLATELETS",
  prolactin: "PROLACTIN",
  total_protein: "PROT",
  psa: "PSA",
  rbc: "RBC",
  shbg: "SHBG",
  testosterone: "TEST",
  triglycerides: "TRI",
  tsh: "TSH",
  urea: "UREA",
  vitamin_d: "VIDC",
  wbc: "WCC",
}

/** How a sample was taken, from its `collection_detail` and
 * `collection_method`; any other pair, or none, is a home kit's. */
export const METHODS: readonly {
  detail: string
  method: string
  words: string
}[] = [
  { detail: "in-clinic", method: "venous", words: "venous, in clinic" },
  { detail: "tasso", method: "capillary", words: "capillary, Tasso+ device" },
  {
    detail: "fingerprick",
    method: "capillary",
    words: "capillary, finger prick",
  },
]

export const HOME_KIT = "capillary microtainer, home kit"

/** Hurdle's interpretation as a flag. */
export const HURDLE_FLAGS: Readonly<Record<string, "N" | "H" | "L">> = {
  NORMAL: "N",
  HIGH: "H",
  LOW: "L",
  DEFICIENT: "L",
}

/** The material of every Hurdle row: a capillary home kit's. */
export const HURDLE_SPECIMEN = "capillary blood"
