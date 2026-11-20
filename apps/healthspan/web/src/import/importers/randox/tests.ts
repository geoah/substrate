// The tests the Randox Health portal reports, by the name it gives each,
// and the analyte each is written as (apps/healthspan/docs/importers/
// 02-randox.md, The tests): the catalogue entry's code where the catalogue
// has one, else the shared metric of a measure it has no entry for. A test
// the table does not name is matched by its name through the catalogue.

import type { AnalyteRef } from "../shared"

const entry = (code: string): AnalyteRef => ({ entry: code })
const metric = (id: string): AnalyteRef => ({ metric: id })

/** Every test the portal is known to report, by its name. */
export const RANDOX_TESTS: Readonly<Record<string, AnalyteRef>> = {
  // personal health measurements
  Height: entry("height"),
  Weight: entry("body_mass"),
  "Body Mass Index (BMI)": entry("bmi"),
  "Oxygen Saturation": metric("health/spo2"),
  "Waist Circumference": entry("waist_circumference"),
  "Hip Circumference": metric("health/hip_circumference"),
  "Waist / Hip Ratio": entry("waist_hip_ratio"),
  Pulse: metric("health/heart_rate"),
  "Systolic Blood pressure": entry("systolic_blood_pressure"),
  "Diastolic Blood pressure": entry("diastolic_blood_pressure"),
  // full blood count
  Haemoglobin: entry("haemoglobin"),
  Haematocrit: entry("haematocrit"),
  "Mean Cell Haemoglobin (MCH)": entry("mch"),
  "Mean Cell Haemoglobin Concentration (MCHC)": entry("mchc"),
  "Red Blood Cell Mean Cell Volume (MCV)": entry("mcv"),
  "Red Blood Cell Count": entry("rbc"),
  "Basophil Count": entry("basophils"),
  "Eosinophil Count": entry("eosinophils"),
  "Lymphocyte Count": entry("lymphocytes"),
  "Monocyte Count": entry("monocytes"),
  "Neutrophil Count": entry("neutrophils"),
  "White Blood Cell Count": entry("wbc"),
  "Platelet Count": entry("platelets"),
  // iron status
  Iron: entry("iron"),
  Ferritin: entry("ferritin"),
  "Total Iron Binding Capacity (TIBC)": entry("tibc"),
  Transferrin: entry("transferrin"),
  "Transferrin Saturation": entry("transferrin_saturation"),
  // heart health
  "Total Cholesterol": entry("total_cholesterol"),
  "LDL Cholesterol": entry("ldl_cholesterol"),
  "HDL Cholesterol": entry("hdl_cholesterol"),
  "Total Cholesterol / HDL Cholesterol Ratio": entry("cholesterol_hdl_ratio"),
  Triglycerides: entry("triglycerides"),
  "Apolipoprotein A-I": entry("apolipoprotein_a1"),
  "Apolipoprotein B": entry("apolipoprotein_b"),
  "Apolipoprotein B / A-I Ratio": metric("labs/apob_apoa1_ratio"),
  "Apolipoprotein CII": metric("labs/apolipoprotein_c2"),
  "Apolipoprotein CIII": entry("apolipoprotein_c3"),
  "Apolipoprotein E": entry("apolipoprotein_e"),
  "Small LDL Cholesterol": metric("labs/small_dense_ldl_cholesterol"),
  "Lipoprotein (a)": entry("lipoprotein_a"),
  "High Sensitivity C-Reactive Protein (hsCRP)": entry("hs_crp"),
  "Creatine Kinase": entry("creatine_kinase"),
  "Fatty Acid Binding Protein-3 (FABP-3)": metric("labs/fabp3"),
  "Cardiovascular Risk Score": metric("labs/cardiovascular_risk"),
  // diabetes health
  Glucose: entry("fasting_glucose"),
  HbA1c: entry("hba1c"),
  Insulin: entry("insulin"),
  "C-peptide": entry("c_peptide"),
  "HOMA-IR": metric("labs/homa_ir"),
  "Microalbumin / Creatinine Ratio": metric(
    "labs/urine_albumin_creatinine_ratio"
  ),
  // metabolic syndrome
  Leptin: metric("labs/leptin"),
  Resistin: metric("labs/resistin"),
  // kidney health
  Creatinine: entry("creatinine"),
  "Estimated Glomerular Filtration Rate (eGFR)": entry("egfr"),
  "Cystatin C": entry("cystatin_c"),
  "Calcium (adjusted)": entry("calcium_adjusted"),
  Chloride: entry("chloride"),
  Magnesium: entry("magnesium"),
  Phosphate: entry("phosphate"),
  Potassium: entry("potassium"),
  Sodium: entry("sodium"),
  Urea: entry("urea"),
  "Uric Acid": entry("uric_acid"),
  // urinalysis
  "Bilirubin (Urine)": metric("labs/urine_bilirubin"),
  "Glucose (Urine)": metric("labs/urine_glucose"),
  "Ketones (Urine)": metric("labs/urine_ketones"),
  "Nitrite (Urine)": metric("labs/urine_nitrite"),
  "pH (Urine)": metric("labs/urine_ph"),
  "Protein (Urine)": metric("labs/urine_protein"),
  "Red Blood Cells (Urine)": metric("labs/urine_blood"),
  "Urobilinogen (Urine)": metric("labs/urine_urobilinogen"),
  "White Blood Cells (Urine)": metric("labs/urine_leukocytes"),
  // liver and pancreatic health
  "Alanine Aminotransferase (ALT)": entry("alt"),
  "Alkaline Phosphatase (ALP)": entry("alp"),
  "Aspartate Aminotransferase (AST)": entry("ast"),
  "Gamma-Glutamyltransferase (GGT)": entry("ggt"),
  "Total Bilirubin": entry("bilirubin"),
  "Fibrosis-4 Score (FIB-4)": metric("labs/fib4"),
  Albumin: entry("albumin"),
  "Pancreatic Amylase": entry("pancreatic_amylase"),
  Lipase: entry("lipase"),
  // digestive, nutritional, muscle, bone health
  "H. pylori": metric("labs/h_pylori_antibody"),
  "Total Antioxidant Status (TAS)": metric("labs/total_antioxidant_status"),
  "Folic acid": entry("folate"),
  "Vitamin B12": entry("vitamin_b12"),
  "Vitamin D": entry("vitamin_d"),
  "Rheumatoid Factor (RF)": entry("rheumatoid_factor"),
  "Parathyroid Hormone (PTH)": entry("pth"),
  // allergy, infection and inflammation
  "Immunoglobulin E (IgE)": metric("labs/ige"),
  "C-Reactive Protein (CRP)": entry("crp"),
  "Immunoglobulin A (IgA)": metric("labs/iga"),
  "Immunoglobulin G (IgG)": metric("labs/igg"),
  "Immunoglobulin M (IgM)": metric("labs/igm"),
  "Antistreptolysin O (ASO)": metric("labs/aso"),
  "Complement Component 3 (C3)": metric("labs/complement_c3"),
  "Complement Component 4 (C4)": metric("labs/complement_c4"),
  // pituitary, adrenal, thyroid, prostate and hormonal health
  "Thyroid Stimulating Hormone (TSH)": entry("tsh"),
  "Follicle Stimulating Hormone": entry("fsh"),
  "Luteinising Hormone": entry("lh"),
  Prolactin: entry("prolactin"),
  "Free Thyroxine (FT4)": entry("free_t4"),
  "Free Tri-iodothyronine (FT3)": entry("free_t3"),
  "Anti-Thyroglobulin Antibody (Anti-Tg)": entry("anti_tg"),
  "Anti-Thyroid Peroxidase Antibody (Anti-TPO)": entry("anti_tpo"),
  "Thyroxine-binding globulin (TBG)": metric("labs/tbg"),
  "Total Prostate Specific Antigen (TPSA)": entry("psa"),
  Oestradiol: entry("estradiol"),
  Testosterone: entry("testosterone"),
  "Sex Hormone Binding Globulin": entry("shbg"),
  "Free Testosterone": entry("free_testosterone"),
}

/** Tests of a urine sample whose name does not say so. */
export const URINE_TESTS: ReadonlySet<string> = new Set([
  "Microalbumin / Creatinine Ratio",
])

/** The tests the portal prints that are not read: "Blood pressure" is the
 * systolic value again, with the systolic bands, beside the systolic and
 * diastolic rows of the same panel. */
export const SKIPPED_TESTS: Readonly<Record<string, string>> = {
  "Blood pressure":
    "it repeats the systolic value and its bands beside the systolic and diastolic rows",
}

/** The panel of the measurements the clinic takes itself (height, weight,
 * blood pressure), which have no specimen. */
export const PERSONAL_PANEL = "Personal Health Measurements"

/** The analyte a test is written as: the table's, else the catalogue's
 * match for its name. */
export function testAnalyte(name: string): AnalyteRef {
  return RANDOX_TESTS[name] ?? { name }
}
