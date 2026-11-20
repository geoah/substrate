// What each Healthspan card is, why it matters, what moves it and how it
// is measured, with the published sources behind each scale. Shown in the
// card's info panel; the computed rows (standing, suggestion, age score)
// are added by the card. A vendor's own score keeps its vendor's name;
// nothing here names a device, a lab or a person.

import { DEFS } from "../engine/engine"
import type { MetricId } from "../engine/report"
import { CONTRIBUTOR_LABELS, type SignalId } from "./healthspanMetrics"

export type BodyKey =
  "health/body_mass" | "health/body_fat_fraction" | "health/visceral_fat_rating"

export type HealthspanCardKey = MetricId | SignalId | BodyKey

export interface Source {
  name: string
  url: string
}

export interface CardContent {
  what: string
  why: string
  moves: string
  measuredBy: string
  sources: Source[]
}

/** The published sources the age score's curves come from. */
export const SCORE_SOURCES = {
  whitePaper: {
    name: "WHOOP, Healthspan white paper (2025), the reference curves and targets",
    url: "https://assets.ctfassets.net/rbzqg6pelgqa/3ONehqJslbqxI7CQlwGjfT/36429d6f66940e1fd866a772ed5bfc93/WHOOP_2025_White_Paper_Healthspan__6_.pdf",
  },
  guide: {
    name: "WHOOP support, Healthspan and Pace of Aging guide",
    url: "https://support.whoop.com/s/article/Healthspan-WHOOP-Age-Pace-of-Aging-Guide?language=en_US",
  },
  paluch: {
    name: "Paluch et al. 2022, Lancet Public Health 7:e219-28, daily steps and mortality",
    url: "https://doi.org/10.1016/S2468-2667(21)00302-9",
  },
  spiegelhalter: {
    name: "Spiegelhalter 2016, effective age from hazard ratios",
    url: "https://doi.org/10.1186/s12911-016-0342-z",
  },
} satisfies Record<string, Source>

const S = {
  ...SCORE_SOURCES,
  whoop101: {
    name: "WHOOP developer docs, WHOOP 101",
    url: "https://developer.whoop.com/docs/whoop-101/",
  },
  whoopStrain: {
    name: "WHOOP, How does WHOOP Strain work 101",
    url: "https://www.whoop.com/thelocker/how-does-whoop-strain-work-101/",
  },
  whoopRecovery: {
    name: "WHOOP, How does WHOOP Recovery work 101",
    url: "https://www.whoop.com/thelocker/how-does-whoop-recovery-work-101/",
  },
  whoopSleep: {
    name: "WHOOP support, WHOOP Sleep",
    url: "https://support.whoop.com/s/article/WHOOP-Sleep?language=en_US",
  },
  whoopSleepNeed: {
    name: "WHOOP, How much sleep do I need",
    url: "https://www.whoop.com/thelocker/how-much-sleep-do-adults-need/",
  },
  whoopConsistency: {
    name: "WHOOP, Sleep consistency",
    url: "https://www.whoop.com/thelocker/new-feature-sleep-consistency-why-we-track-it/",
  },
  whoopResp: {
    name: "WHOOP, Understanding respiratory rate",
    url: "https://www.whoop.com/thelocker/understanding-respiratory-rate-what-it-is-whats-normal-and-why-you-should/",
  },
  whoopSkin: {
    name: "WHOOP, How WHOOP tracks skin temperature",
    url: "https://www.whoop.com/thelocker/how-whoop-tracks-skin-temperature/",
  },
  whoopHrv: {
    name: "WHOOP, Good HRV explained: average heart rate variability by age",
    url: "https://www.whoop.com/thelocker/normal-hrv-range-age-gender/",
  },
  whoopRhr: {
    name: "WHOOP, Resting heart rate: what's normal",
    url: "https://www.whoop.com/thelocker/normal-resting-heart-rate-improve-fitness/",
  },
  nunan: {
    name: "Nunan, Sandercock and Brodie 2010, Pacing Clin Electrophysiol 33:1407-17",
    url: "https://pubmed.ncbi.nlm.nih.gov/20663071/",
  },
  nhsScotland: {
    name: "NHS Scotland, Core clinical assessment skills manual",
    url: "https://portal.scot.nhs.uk/CourseDocuments/113011/Core%20Clinical%20Assessment%20Skills%20Manual.pdf",
  },
  nhsCovid: {
    name: "NHS England, Self-monitoring COVID-19 diary",
    url: "https://www.england.nhs.uk/coronavirus/documents/c1541-self-monitoring-covid-19-diary-v2-print-version-january-2022/",
  },
  cleveland: {
    name: "Cleveland Clinic, Blood oxygen level",
    url: "https://my.clevelandclinic.org/health/diagnostics/22447-blood-oxygen-level",
  },
  cdcBmi: {
    name: "CDC, Adult BMI categories (under 18.5 underweight, 18.5 to under 25 healthy)",
    url: "https://www.cdc.gov/bmi/adult-calculator/bmi-categories.html",
  },
  nhsObesity: {
    name: "NHS, Overweight and obesity in adults (25 and 30, lower for some ethnic groups)",
    url: "https://www.nhs.uk/conditions/overweight-and-obesity/",
  },
  nice: {
    name: "NICE NG246, Overweight and obesity management",
    url: "https://www.nice.org.uk/guidance/ng246/chapter/Identifying-and-assessing-overweight-obesity-and-central-adiposity",
  },
  gallagher: {
    name: "Gallagher et al. 2000, Am J Clin Nutr 72:694-701, Table 4 (African American and white adults)",
    url: "https://pubmed.ncbi.nlm.nih.gov/10966886/",
  },
  gallagherTable: {
    name: "Table 4 as reproduced by Akindele, Phillips and Igumbor 2016, J Public Health Afr, Table 1",
    url: "https://pmc.ncbi.nlm.nih.gov/articles/PMC5349253/",
  },
  tanita: {
    name: "Tanita Europe support, How is visceral fat measured by Tanita",
    url: "https://support.tanita.eu/support/solutions/articles/60000677375-how-is-visceral-fat-measured-by-tanita",
  },
  apple: {
    name: "Apple Support, Cardio fitness levels and Apple Watch",
    url: "https://support.apple.com/en-us/108790",
  },
} satisfies Record<string, Source>

export const HEALTHSPAN_CONTENT: Record<HealthspanCardKey, CardContent> = {
  sleepConsistency: {
    what: "How regular your bed and wake times are from night to night, as a percent; 100 is the same times every night.",
    why: "Regular timing keeps the body clock steady and sleep efficient; the model counts consistency under 70 percent as adding years.",
    moves:
      "Bedtime and wake time variability, weekend shifts, late nights, travel.",
    measuredBy:
      "The sleep consistency score a source gives each main sleep; naps are left out. A source that scores none gives this contributor no value.",
    sources: [S.whitePaper, S.whoopConsistency],
  },
  sleepHours: {
    what: "Time asleep per night (light, deep and REM, naps added), not time in bed.",
    why: "Under 7 hours a night adds years in the model; 7 to 9 hours is the adult sleep need sleep guidance gives.",
    moves:
      "Bedtime, time in bed, late caffeine and alcohol, light and screens, a hot room.",
    measuredBy:
      "The stage totals of each sleep, from the first source in your night order; a sleep without stage totals is left out.",
    sources: [S.whitePaper, S.whoopSleepNeed],
  },
  zones13: {
    what: "Minutes a week of recorded workouts at 40 to 80 percent of heart rate reserve, averaged over the days worn and scaled to a week.",
    why: "Easy and moderate aerobic work carries most of the health benefit of exercise; the model credits it up to about 10 hours a week against an age-based target.",
    moves:
      "Walking, cycling, easy running, any recorded workout in zones 1 to 3; all-day heart rate does not count.",
    measuredBy:
      "The zone times of each workout with a measure of effort, from the first source in your workout order.",
    sources: [S.whitePaper],
  },
  zones45: {
    what: "Minutes a week at 80 to 100 percent of heart rate reserve.",
    why: "Short hard efforts raise fitness and VO2 max; the model's target is about 10 minutes a week with a small credit above it.",
    moves: "Intervals, hills, hard sessions.",
    measuredBy:
      "The zone times of each workout with a measure of effort, from the first source in your workout order.",
    sources: [S.whitePaper],
  },
  strength: {
    what: "Minutes a week of strength workouts: strength training, pilates and yoga from any source, and the white paper's list (functional fitness, HIIT and the rest) by the name a source gives the sport.",
    why: "Strength work preserves muscle and bone with age; the model credits it from 40 minutes a week up to 120.",
    moves:
      "Two or three sessions a week, recorded with a heart rate or a strain.",
    measuredBy:
      "Each strength workout's length, where the workout carries zone times, a strain or an average heart rate.",
    sources: [S.whitePaper],
  },
  steps: {
    what: "Steps a day; where two sources count a day, the one your repository ranks first.",
    why: "Daily movement outside workouts lowers mortality risk; the model's target is 8,000 a day (5,600 from age 60).",
    moves: "Walking built into the day: commute, breaks, errands.",
    measuredBy:
      "Daily step totals, ranked by your sources' priorities for steps.",
    sources: [S.whitePaper, S.paluch],
  },
  vo2max: {
    what: "The highest rate of oxygen the body can use in exercise, in ml/kg/min: aerobic fitness.",
    why: "Low cardiorespiratory fitness is one of the strongest predictors of mortality; the model compares yours with an age and sex table.",
    moves:
      "Aerobic volume (zone 2) plus some hard intervals, over months; weight loss raises the per-kilogram figure.",
    measuredBy:
      "A watch's or a lab's estimate; one reading stands for up to a year.",
    sources: [S.whitePaper, S.apple],
  },
  rhr: {
    what: "Heart rate at rest, in beats per minute: a band's at the night's deepest rest, a watch's over the day.",
    why: "A lower resting heart rate goes with higher fitness; the model's target is 60 bpm for men and 64 for women.",
    moves:
      "Aerobic fitness lowers it; alcohol, illness, poor sleep, heat and late meals raise it.",
    measuredBy:
      "The source your repository ranks first for resting heart rate, each value on the night of the sleep it measures.",
    sources: [S.whitePaper, S.whoopRhr],
  },
  leanMass: {
    what: "Everything but fat (muscle, bone, water) as a percent of body weight.",
    why: "Muscle protects strength and metabolism with age; the model adds years below 80 percent for men and 67 for women at 30, lowered with age; it never removes them.",
    moves:
      "Strength training and enough protein raise it; inactivity lowers it.",
    measuredBy:
      "Body composition results: fat-free mass over the same scan's weight, else 100 less its body fat; one reading stands for up to a year.",
    sources: [S.whitePaper],
  },
  recovery: {
    what: "WHOOP's 0 to 100 score of readiness for strain, from HRV, resting heart rate, sleep and respiratory rate.",
    why: "A day-to-day readiness signal, not a long-term health measure: it says how hard to go today. Green 67 to 100 is primed for high strain, yellow 34 to 66 for moderate, red 0 to 33 for rest.",
    moves: "Sleep, alcohol, illness, training load, stress.",
    measuredBy: "WHOOP, each morning.",
    sources: [S.whoopRecovery, S.whoop101],
  },
  sleepPerformance: {
    what: "WHOOP's nightly sleep score, a percent combining sleep consistency, sufficiency (hours against need), stress and efficiency.",
    why: "Good, regular sleep is what recovery rests on; the score shows the night's quality as well as its length. WHOOP's bands: 85 and above optimal, 70 to 84 sufficient, under 70 poor.",
    moves:
      "Regular bed and wake times, time in bed against need, a calm night (alcohol, late meals and stress lower it), waking less.",
    measuredBy: "WHOOP sleep.",
    sources: [S.whoopSleep],
  },
  strain: {
    what: "WHOOP's 0 to 21 measure of the day's cardiovascular load, from heart rate through the day and in workouts.",
    why: "Matching strain to recovery is how training builds fitness without overreaching; strain is not a health outcome in itself. Light 0 to 9, Moderate 10 to 13, High 14 to 17, All Out 18 to 21; members average about 11.",
    moves: "Workout intensity and length, all-day activity.",
    measuredBy: "WHOOP strain.",
    sources: [S.whoop101, S.whoopStrain],
  },
  hrv: {
    what: "Heart rate variability: the beat-to-beat variation during sleep (RMSSD, in ms).",
    why: "Higher and stable HRV goes with good recovery and fitness; a week below your baseline is a load or illness signal. It is personal and falls with age: the middle half of 25-year-old men sit at about 50 to 100 ms and of 45-year-old men at 35 to 60 ms, women at about 45 to 90 and 30 to 55 ms.",
    moves: "Sleep, alcohol, illness, training load, fitness, age.",
    measuredBy: "The nightly RMSSD of the source your repository ranks first.",
    sources: [S.whoopHrv, S.nunan],
  },
  hrvSdnn: {
    what: "Heart rate variability as SDNN, in ms: the spread of beat-to-beat intervals, the measure most watches record.",
    why: "Like RMSSD it is personal and falls with age, so it is read against your own baseline; SDNN and RMSSD are different measures and are never compared.",
    moves: "Sleep, alcohol, illness, training load, fitness, age.",
    measuredBy: "The mean of each day's SDNN readings.",
    sources: [S.nunan],
  },
  respiratoryRate: {
    what: "Breaths per minute during sleep, the night's figure.",
    why: "One of the steadiest signals night to night, so a sustained rise is an early sign of illness or heavy load. 12 to 20 a minute is the adult resting range.",
    moves: "Illness, fever, altitude, alcohol; little else.",
    measuredBy: "The source's nightly summary, during sleep.",
    sources: [S.whoopResp, S.nhsScotland],
  },
  spo2: {
    what: "Oxygen saturation of the blood during sleep, percent.",
    why: "95 to 100 percent is the normal range; nights under 95 can point at disturbed breathing in sleep or illness, and 92 or less is the NHS's urgent threshold.",
    moves: "Sleep position and breathing, altitude, illness, alcohol.",
    measuredBy: "The source's nightly summary, during sleep.",
    sources: [S.nhsCovid, S.cleveland],
  },
  skinTemperature: {
    what: "Wrist skin temperature during sleep, shown as the deviation from your own baseline.",
    why: "A rise from baseline goes with illness and some cycle phases; absolute values vary by person and room, so only the deviation is read.",
    moves: "Illness, the room, alcohol, late exercise, hormonal cycles.",
    measuredBy:
      "The source's nightly summary, during sleep; the baseline is the previous 30 nights.",
    sources: [S.whoopSkin],
  },
  "health/body_mass": {
    what: "Body weight from scans and clinic visits, in kilograms.",
    why: "Weight against height (BMI) is the standard screen for weight-related risk: under 18.5 underweight, 18.5 to 24.9 healthy, 25 to 29.9 overweight, 30 and over obese; lower thresholds apply to some ethnic groups and waist measures suit very muscular people better.",
    moves: "Energy balance over weeks; water and glycogen day to day.",
    measuredBy:
      "Lab results from scans and clinics; the height is the newest any source reports.",
    sources: [S.cdcBmi, S.nhsObesity, S.nice],
  },
  "health/body_fat_fraction": {
    what: "Fat as a percent of body weight.",
    why: "Body fat tracks metabolic and heart risk better than weight alone; healthy ranges rise with age and differ by sex (men 40 to 59: 11 to 21 percent healthy). The ranges are Gallagher's Table 4, from African American and white adults.",
    moves: "Energy balance, strength training, protein.",
    measuredBy: "Lab results from body composition scans and scales.",
    sources: [S.gallagher, S.gallagherTable],
  },
  "health/visceral_fat_rating": {
    what: "Fat around the organs, as a bioimpedance scale rates it (Tanita's rating runs 1 to 59; one unit is about 10 cm² on an MRI scan).",
    why: "Visceral fat is the fat most linked with metabolic and heart risk; on Tanita's rating 1 to 12 is healthy, 13 and over excess. Other makers' ratings do not compare.",
    moves:
      "Aerobic activity, diet, sleep; it responds faster than total weight.",
    measuredBy: "Scale readings in the lab results.",
    sources: [S.tanita],
  },
}

/** VO2 max's "Measured by" when the record holds one reading. */
export const VO2_ONE_READING =
  "The repository holds one reading in two years, so the chart has one point; more readings from your source fill it."

/** "Hours of sleep" to "hours of sleep" mid-sentence; "HRV" and "VO2 max"
 * stay. */
const lower = (s: string) =>
  /^[A-Z][a-z]/.test(s) ? s[0].toLowerCase() + s.slice(1) : s

/** The nine contributors as a sentence list, lower case mid-sentence. */
function contributorList(): string {
  const names = DEFS.map((d) => lower(CONTRIBUTOR_LABELS[d.id]))
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`
}

/** The card's "Age score" row for a signal or body reading. */
export function notInScore(label: string, kind: "signal" | "body"): string {
  return `Not in the score. The age score adds up ${DEFS.length} contributors (${contributorList()}); ${lower(label)} is not one of them, so it changes no years here. It is shown because it ${kind === "signal" ? "describes recovery and readiness from night to night" : "describes your body composition, from scans months apart"}.`
}

/** The explainer's "What is not in the score" row: every signal and body
 * reading with the same sentence once. */
export function notInScoreAll(signals: string[], body: string[]): string {
  const list = (xs: string[]) =>
    xs.length > 1
      ? `${xs.slice(0, -1).join(", ")} and ${xs[xs.length - 1]}`
      : (xs[0] ?? "")
  const low = (xs: string[]) => xs.map(lower)
  return `The ${signals.length} signals (${list(low(signals))}) and ${body.length} body readings (${list(low(body))}). The age score adds up ${DEFS.length} contributors (${contributorList()}); these are not among them, so they change no years here. They are shown because the signals describe recovery and readiness from night to night and the body readings your body composition, from scans months apart.`
}
