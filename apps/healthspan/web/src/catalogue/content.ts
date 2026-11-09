// Why the 23 important lab entries matter and what moves them, keyed by the
// entry's code. The Labs info panel shows them as "Why it matters" and "What
// moves it" after the entry's description, with the sources linked. Heart
// UK's pages sit behind a bot challenge, so the British Heart Foundation's
// pages on the same subjects stand in for them.

import type { ContentSource } from "./types"

export interface LabContent {
  why: string
  moves: string
  sources: ContentSource[]
}

const NHS_CHOLESTEROL: ContentSource = {
  title: "NHS, High cholesterol",
  url: "https://www.nhs.uk/conditions/high-cholesterol/",
}
const BHF_LEVELS: ContentSource = {
  title: "British Heart Foundation, Understanding your cholesterol levels",
  url: "https://www.bhf.org.uk/informationsupport/risk-factors/high-cholesterol/understanding-your-cholesterol-levels",
}
const LIVER_TESTS: ContentSource = {
  title: "Liver UK, Liver blood tests",
  url: "https://liveruk.org/about-liver-disease/tests/liver-blood-tests/",
}
const NHS_IRON: ContentSource = {
  title: "NHS, Iron deficiency anaemia",
  url: "https://www.nhs.uk/conditions/iron-deficiency-anaemia/",
}
const NHS_B12: ContentSource = {
  title: "NHS, Vitamin B12 or folate deficiency anaemia",
  url: "https://www.nhs.uk/conditions/vitamin-b12-or-folate-deficiency-anaemia/",
}

export const LAB_CONTENT: Record<string, LabContent> = {
  egfr: {
    why: "Estimated kidney filtration; low values over months mean chronic kidney disease, which also raises heart risk.",
    moves:
      "Blood pressure, diabetes, dehydration on the day, some drugs and supplements (creatine raises creatinine); muscle mass skews the estimate.",
    sources: [
      {
        title: "NHS, Chronic kidney disease, diagnosis",
        url: "https://www.nhs.uk/conditions/kidney-disease/diagnosis/",
      },
      {
        title: "Kidney Care UK, Tests for chronic kidney disease",
        url: "https://kidneycareuk.org/kidney-disease-information/about-kidney-health/tests-for-chronic-kidney-disease/",
      },
    ],
  },
  cystatin_c: {
    why: "A kidney filtration marker that does not depend on muscle mass; used beside eGFR when creatinine misleads.",
    moves: "Kidney function, thyroid disease, steroids, inflammation.",
    sources: [
      {
        title: "NICE NG203, Chronic kidney disease: assessment and management",
        url: "https://www.nice.org.uk/guidance/ng203",
      },
    ],
  },
  total_cholesterol: {
    why: "All blood cholesterol; high levels raise the risk of heart attack and stroke, read together with HDL and non-HDL.",
    moves:
      "Saturated fat, weight, activity, genetics, thyroid function, statins.",
    sources: [NHS_CHOLESTEROL, BHF_LEVELS],
  },
  triglycerides: {
    why: "Blood fats from excess calories; high fasting levels add heart risk and very high levels risk pancreatitis.",
    moves:
      "Sugar and refined carbohydrate, alcohol, weight, activity, whether the test was fasted.",
    sources: [
      {
        title:
          "British Heart Foundation, Serum triglycerides: what level is too high?",
        url: "https://www.bhf.org.uk/informationsupport/heart-matters-magazine/nutrition/ask-the-expert/serum-triglycerides",
      },
      NHS_CHOLESTEROL,
    ],
  },
  ldl_cholesterol: {
    why: "The cholesterol carried into artery walls; lower is better for heart risk.",
    moves: "Saturated fat, weight, genetics, statins and other lipid drugs.",
    sources: [NHS_CHOLESTEROL, BHF_LEVELS],
  },
  hdl_cholesterol: {
    why: "The cholesterol carried away from arteries; higher is protective up to a point.",
    moves: "Exercise, weight, not smoking; genetics.",
    sources: [BHF_LEVELS],
  },
  non_hdl_cholesterol: {
    why: "Total minus HDL: every particle that can enter artery walls; NICE's preferred target measure.",
    moves: "The same as LDL and triglycerides together.",
    sources: [
      {
        title:
          "NICE NG238, Cardiovascular disease: risk assessment and reduction, including lipid modification",
        url: "https://www.nice.org.uk/guidance/ng238",
      },
      BHF_LEVELS,
    ],
  },
  apolipoprotein_b: {
    why: "One ApoB per atherogenic particle, so it counts the particles that cause plaque; a sharper risk marker than LDL.",
    moves: "Saturated fat, weight, insulin resistance, lipid drugs.",
    sources: [
      {
        title:
          "Sniderman AD et al., Apolipoprotein B particles and cardiovascular disease: a narrative review, JAMA Cardiology 2019",
        url: "https://pubmed.ncbi.nlm.nih.gov/31642874/",
      },
    ],
  },
  lipoprotein_a: {
    why: "An inherited lipoprotein that raises heart and valve risk independently of LDL; set by genes and usually measured once.",
    moves:
      "Little: genetics set it; lipid drugs barely move it; knowing it changes how hard to treat the rest.",
    sources: [
      {
        title: "British Heart Foundation, High lipoprotein (a)",
        url: "https://www.bhf.org.uk/informationsupport/risk-factors/lipoprotein-a",
      },
    ],
  },
  tsh: {
    why: "The pituitary's signal to the thyroid; high means an underactive thyroid, low an overactive one.",
    moves:
      "Thyroid disease, iodine, levothyroxine dose, illness; biotin supplements interfere with the test.",
    sources: [
      {
        title: "British Thyroid Foundation, Thyroid function tests",
        url: "https://www.btf-thyroid.org/thyroid-function-tests",
      },
    ],
  },
  testosterone: {
    why: "The main male sex hormone; low levels affect energy, mood, muscle and bone; read with the time of day and SHBG.",
    moves:
      "Sleep, weight, alcohol, strength training, time of day (morning is highest), illness, age.",
    sources: [
      {
        title: "NHS, The 'male menopause'",
        url: "https://www.nhs.uk/conditions/male-menopause/",
      },
      {
        title:
          "British Society for Sexual Medicine guidelines on adult testosterone deficiency (J Sex Med 2017)",
        url: "https://pubmed.ncbi.nlm.nih.gov/29198507/",
      },
    ],
  },
  alt: {
    why: "A liver enzyme released when liver cells are stressed; the main marker of fatty liver and drug effects.",
    moves:
      "Alcohol, weight and fatty liver, medicines and supplements, hard exercise before the test, viral hepatitis.",
    sources: [LIVER_TESTS],
  },
  ast: {
    why: "A liver and muscle enzyme, read with ALT, since muscle damage and exercise raise it too.",
    moves: "The same as ALT, plus muscle work and injury.",
    sources: [LIVER_TESTS],
  },
  vitamin_d: {
    why: "Needed for bone and muscle; deficiency is common in the UK winter and weakens bone.",
    moves:
      "Sun on skin from spring to autumn, supplements, oily fish and fortified foods; weight.",
    sources: [
      {
        title: "NHS, Vitamin D",
        url: "https://www.nhs.uk/conditions/vitamins-and-minerals/vitamin-d/",
      },
    ],
  },
  vitamin_b12: {
    why: "Needed for red cells and nerves; deficiency causes anaemia and nerve damage.",
    moves:
      "Diet (animal foods), absorption (pernicious anaemia, metformin, acid suppressants), supplements.",
    sources: [NHS_B12],
  },
  ferritin: {
    why: "Stored iron; low means iron deficiency, high can mean inflammation, liver stress or iron overload.",
    moves:
      "Dietary iron and absorption, blood loss, inflammation (raises it), alcohol.",
    sources: [NHS_IRON],
  },
  haemoglobin: {
    why: "The oxygen carrier in red cells; low is anaemia, high can follow dehydration or altitude.",
    moves:
      "Iron, B12 and folate, blood loss, hydration, altitude and training.",
    sources: [NHS_IRON, NHS_B12],
  },
  hba1c: {
    why: "Average blood glucose over about three months; the diagnostic test for prediabetes and type 2 diabetes.",
    moves:
      "Carbohydrate load, weight, activity, sleep; anaemia and some haemoglobin variants distort it.",
    sources: [
      {
        title: "Diabetes UK, What is HbA1c?",
        url: "https://www.diabetes.org.uk/about-diabetes/looking-after-diabetes/hba1c",
      },
      {
        title: "NHS, Type 2 diabetes",
        url: "https://www.nhs.uk/conditions/type-2-diabetes/",
      },
    ],
  },
  fasting_glucose: {
    why: "Blood sugar after an overnight fast; a snapshot of glucose control.",
    moves:
      "The previous evening's food, sleep, stress hormones, illness, fasting length.",
    sources: [
      {
        title: "Diabetes UK, Blood sugar levels",
        url: "https://www.diabetes.org.uk/about-diabetes/looking-after-diabetes/blood-sugar-levels",
      },
    ],
  },
  insulin: {
    why: "How much insulin it takes to hold fasting glucose; high with a normal glucose is early insulin resistance.",
    moves:
      "Weight, especially around the waist, activity, sleep, refined carbohydrate.",
    sources: [
      {
        title: "Diabetes UK, Insulin resistance",
        url: "https://www.diabetes.org.uk/about-diabetes/looking-after-diabetes/treatments/insulin/resistance",
      },
    ],
  },
  hs_crp: {
    why: "A sensitive marker of inflammation; chronically raised levels add heart risk; any infection or injury spikes it for days.",
    moves:
      "Infection, injury, intense exercise in the days before, weight, sleep, smoking.",
    sources: [
      {
        title: "Lab Tests Online UK, C-reactive protein (CRP)",
        url: "https://labtestsonline.org.uk/tests/c-reactive-protein-crp",
      },
      {
        title: "Cleveland Clinic, C-reactive protein (CRP) test",
        url: "https://my.clevelandclinic.org/health/diagnostics/23056-c-reactive-protein-crp-test",
      },
    ],
  },
  psa: {
    why: "A prostate protein; raised levels can mean enlargement, infection or cancer and lead to further checks.",
    moves:
      "Age, prostate size, infection, ejaculation or cycling within 48 hours before the test.",
    sources: [
      {
        title: "NHS, PSA test",
        url: "https://www.nhs.uk/tests-and-treatments/psa-test/",
      },
    ],
  },
  uric_acid: {
    why: "The end product of purine breakdown; high levels can form crystals (gout, kidney stones) and go with metabolic risk.",
    moves:
      "Alcohol (beer most), fructose and sugary drinks, red meat and shellfish, dehydration, diuretics, kidney function.",
    sources: [
      { title: "NHS, Gout", url: "https://www.nhs.uk/conditions/gout/" },
      {
        title: "UK Gout Society, Gout and diet",
        url: "https://www.ukgoutsociety.org/gout-and-diet/",
      },
    ],
  },
}
