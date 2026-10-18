# 03: Blood tests, biomarkers and clinical data standards

Researcher slice: lab results as data. Date: 2026-10-08.

How to read this file:

- **[web]** means I checked it on the web during this session (URL given).
- **[nlm]** on a LOINC code means I looked the code up in the NLM Clinical Tables LOINC API
  (`https://clinicaltables.nlm.nih.gov/api/loinc_items/v3/search?terms=<code>&sf=LOINC_NUM`) and
  the long common name matched. Every LOINC code in the catalogue in Part 3 was checked this way
  unless it carries **(r)**.
- **(r)** or **[recall]** means from memory, not re-verified today. Treat as unverified.
- Conversion factors in Part 2 and Part 3 are computed from molar masses (recall, arithmetic
  shown) and match the factors commonly published in SI conversion tables. Reference ranges are
  typical adult ranges from recall; every lab publishes its own.

---

## Part 1: providers, and how a person gets their results as data

### 1.1 Summary table

| Source | Public API for the person's own data? | Standard spoken | Auth | Objects returned | Ranges and flags in the result? |
| --- | --- | --- | --- | --- | --- |
| Quest (MyQuest) | No consumer developer API. Results flow to Apple Health Records (FHIR R4) and to the ordering EHR | FHIR R4 (Apple, Quanum EHR), HL7 v2 ORU to EHRs | Apple: SMART-style OAuth inside the Health app; Quanum EHR FHIR API: client id issued by Quest on request | Observation, DiagnosticReport | Yes: Quest's FHIR Observations carry LOINC, value, units, referenceRange, interpretation [web, secondary] |
| Labcorp (Patient portal, OnDemand) | No open developer portal; FHIR and HL7 integration only via agreements. Apple Health Records participant since 2018 | FHIR R4, HL7 v2 | Agreement; Apple Health OAuth | ServiceRequest, DiagnosticReport, Observation, Specimen | Yes in FHIR and in v2 OBX-7/OBX-8 |
| Function Health | No official API. PDF download only; community exporters use a reverse-engineered Firebase-JWT API | Proprietary JSON (unofficial) | Firebase JWT (unofficial) | Biomarker results by category | PDF yes; unofficial JSON includes ranges (per exporter README) |
| InsideTracker | No consumer export API. B2B "Terra" platform has a blood data connector ingesting PDFs, images, Apple Health, HL7 and FHIR (Aug 2026) | Ingests HL7/FHIR; output proprietary | B2B | Biomarker results, ~120 biomarkers | Uses its own "optimal zones" beside lab ranges |
| Everlywell (incl. Everlywell 360) | No public API; enterprise partnerships. Lab partner is Quest for 360 | n/a | n/a | PDF + dashboard, 83 biomarkers in 360 | PDF yes |
| SiPhox Health | Partner REST API (orders, customers, results, webhooks, sandbox); consumer app | Proprietary REST JSON | Partner API keys | Orders, results | Unverified field shape |
| Superpower | No API found; PDF export; imports old PDFs via AI digitisation | n/a | n/a | 100+ biomarkers | PDF yes |
| Levels | No API for labs found. Added blood testing (28 or 100+ markers) on 2025-06-25 alongside CGM | n/a | n/a | Dashboard | Unverified |
| Thriva (UK) | Partner REST API, JSON:API spec, OAuth token endpoint, webhooks. Resources: Order, Test, Result Set, Biomarker Result, User, Escalation | JSON:API | OAuth client credentials (Create OAuth Token endpoint) | Result Set of Biomarker Results | Field shape not visible in public docs index |
| Medichecks (UK) | No consumer CSV or API ("not able to download your results as a spreadsheet/CSV"); PDF only. Partners: API "customisable options" on request | n/a | n/a | PDF | PDF yes, with doctor commentary |
| Randox Health (UK) | No export API found; email report + app | n/a | n/a | Up to 150 biomarkers | PDF |
| Quest + Apple Health app panel | New: announced 2026-09-09, a $119 panel of 50+ biomarkers purchasable inside Apple's Health app, launching later in 2026 | Results land in Health app (mechanism unverified; likely the existing clinical records path) | Apple | | |
| Carrot Care | Consumer iOS app, on-device; imports PDF/photo by OCR, claims ~95% extraction accuracy, 1,000+ biomarkers, phenotypic age | none | n/a | Local | Yes, keeps reference ranges |
| Ulta Lab Tests | Lab marketplace (Quest), PDF download, basic history | n/a | n/a | PDF | PDF |
| "Lab Me" | Could not identify this product; unverified | | | | |
| 1upHealth | Yes. FHIR R4 API aggregating payer and provider data; `Patient/{id}/$everything`; Patient Connect covers USCDI v1 | FHIR R4 (4.0.1), US Core | OAuth2 / SMART | Full FHIR bundle incl. Observation (lab) | As the source system sent them |
| Health Gorilla | Yes. FHIR R4 first. Lab Network (order + result with Quest, Labcorp, BioReference and 100+ labs); Patient Access Service via API with NIST IAL2 identity proofing | FHIR R4 | OAuth2; IAL2 for patient access | DiagnosticReport, Observation, ServiceRequest | Yes (from lab feed) |
| Metriport | Yes, open source (self-hostable) Medical API over CommonWell and Carequality; converts C-CDA, HL7 v2 and PDF to FHIR R4, dedups, codes with LOINC/SNOMED | FHIR R4 | API key (org-level, HIPAA treatment purpose) | Consolidated FHIR bundle | As sourced |
| Particle Health | Yes, commercial. Carequality, CommonWell, eHealth Exchange, TEFCA/QHIN, Surescripts. Formats: FHIR R4, C-CDA, "Flat", "Deltas"; a LABS dataset | FHIR R4, C-CDA, flat JSON | OAuth client credentials | Labs dataset, FHIR | As sourced |
| Fasten Health | `fasten-onprem` (GPL-3.0) is **archived since 2026-07-18** and could only take manual entry or uploaded FHIR bundles; direct provider pulls are in the commercial "Fasten Connect". `fasten-sources` library has SMART-on-FHIR client definitions | FHIR R4 | SMART on FHIR | FHIR bundles | As sourced |
| Apple Health Records | Yes on device. `HKClinicalRecord` samples; `fhirResource` property holds the raw FHIR JSON; only FHIR R4 (plus legacy DSTU2) supported; read-only; per-type permission; needs the Clinical Health Records entitlement | FHIR R4 | On-device HealthKit permission | `HKClinicalTypeIdentifier.labResultRecord` holding an Observation | Whatever the source FHIR server put there |
| US Patient Access APIs | Yes, by regulation. ONC Cures Act (g)(10) certified EHRs expose SMART on FHIR + US Core; CMS-0057-F payer Patient Access API (deadline 2027-01-01) must include lab results with dates of service from 2016-01-01, within 1 business day | FHIR R4.0.1, US Core 3.1.1 (and later), SMART App Launch 1.0 | SMART on FHIR (OAuth2, PKCE) | US Core Laboratory Result Observation, DiagnosticReport | Yes; `interpretation` and `referenceRange` are Must Support in US Core |
| CMS Blue Button 2.0 | Yes, but claims only (Medicare Parts A, B, D). No lab values | FHIR R4, CARIN Blue Button IG | OAuth2 | ExplanationOfBenefit, Patient, Coverage | No (claims carry the procedure code that a test was billed, not the value) |
| Epic MyChart / open.epic | Yes for patient-facing apps: register at fhir.epic.com, request `Observation.Read (Labs) (R4)`, query `Observation?patient=..&category=laboratory` | FHIR R4, US Core | SMART on FHIR, patient logs in with MyChart | Observation, DiagnosticReport | Yes |
| NHS App / GP Connect (UK) | GP Connect Access Record: Structured is FHIR **STU3** (1.x line), for NHS systems, not consumer apps; a patient-facing GP Connect variant exists as a draft IG. Only filed results are shared with patients | FHIR STU3 (CareConnect profiles) | NHS Spine / CIS2; patient-facing via NHS login (draft) | "Investigations": Observation, DiagnosticReport, Specimen | Yes in the structured record |
| OCR / PDF parsing | The realistic common path for consumer labs | none | none | Text tables | Usually on the page (range text, H/L column) |

### 1.2 Notes per source

**Quest.** Quest publishes a Quanum EHR FHIR API reference guide (Nov 2021, Nov 2022 PDFs) for
"healthcare vendors in the USA seeking read-only access" to Quanum EHR patient data; access by
emailed request form, Quest issues a client id [web:
https://www.questdiagnostics.com/content/dam/corporate/restricted/documents/qps_qecs/Quanum_EHR_FHIR_API_Nov22.pdf].
Sonora Quest (a JV) publishes a similar FHIR API guide [web:
https://www.sonoraquest.com/media/17385/sonora_quest_laboratories_fhir_api_reference_guide_121422.pdf].
MyQuest results integrate into Apple Health Records (since 2018) [web:
https://www.medtechdive.com/news/quest-diagnostics-adds-apple-health-integration-for-lab-results/541981/].
On 2026-09-09 Quest and Apple announced lab ordering inside the Health app: a $119 panel of more
than 50 cardiometabolic and organ-function biomarkers, drawn at about 2,000 Quest patient service
centers, launching later in 2026, not in all states, alongside a Health app redesign with a
"Longevity" tab and a "Health Age" score [web:
https://telehealth.org/news/quest-diagnostics-will-sell-a-119-50-biomarker-lab-panel-inside-apples-health-app/,
https://www.macobserver.com/tips/round-ups/apple-health-labs-119-quest-diagnostics-50-biomarkers/,
https://hlth.com/insights/news/apple-and-quest-diagnostics-expand-health-tracking-with-redesigned-health-app].
For substrate this matters: Apple Health is becoming the place where consumer lab results land
as FHIR, and an iOS companion reading `HKClinicalRecord` would get them.

**Labcorp.** No public developer portal; FHIR and HL7 by agreement; resources ServiceRequest,
DiagnosticReport, Observation, Patient, Practitioner, Specimen, Organization [web:
https://github.com/api-evangelist/labcorp]. Apple Health Records since Oct 2018 [web:
https://ir.labcorp.com/news-releases/news-release-details/labcorp-enables-health-records-iphone].

**Function Health.** Official export is a PDF from `my.functionhealth.com/documents` [web:
https://help.empirical.health/article/18-how-to-export-function-health-results]. Community tools
`bogini/function-health-exporter` (Firebase JWT, JSON in 19+ files) and
`daveremy/function-health-mcp` (MCP server, offline-first) use a reverse-engineered API [web:
https://github.com/bogini/function-health-exporter, https://github.com/daveremy/function-health-mcp].
Labs are run by Quest. Markets "160 biomarkers" (100+ at the first draw, ~60 at the follow-up);
independent counts find ~110 distinct markers [web, secondary review sites].

**InsideTracker.** Consumer upload of PDF/PNG/JPEG, OCR extracts biomarker, value, unit, date
[web: https://www.insidetracker.com/a/articles/a-super-simple-way-to-use-blood-test-results-from-your-doctor].
Aug 2026: "source-agnostic blood data connector" in the consumer app and the B2B Terra platform,
ingesting print and PDF reports, Apple Health files, and HL7/FHIR EMR feeds, ~120 biomarkers [web:
https://hitconsultant.net/2026/08/17/insidetracker-launches-universal-blood-data-connector-terra-platform/].

**SiPhox Health.** Partner REST API with ordering, customer management, result retrieval,
webhooks and a sandbox; panels from 22 to 50 biomarkers [web:
https://siphoxhealth.com/partner/platform-features]. Response schema not public; unverified.

**Thriva.** Partner API docs at `docs.thriva.io`: JSON:API, OAuth token endpoint, Orders, Bulk
Orders, Tests, Result Sets (a Result Set "is simply the set of Biomarker Results that have been
analysed for a Test"), Result Set attachments (the PDF), Users, Escalations (out-of-range alerts),
Appointments, webhooks [web: https://docs.thriva.io/docs, https://docs.thriva.io/llms.txt]. Per-field
shape of a Biomarker Result was not in the public pages I could reach; unverified.

**Medichecks.** PDF only for consumers; "not able to download your results as a spreadsheet/CSV
file currently"; API for partners on request [web:
https://support.medichecks.com/hc/en-gb/articles/31142619957021-Can-I-print-my-results,
https://support.medichecks.com/hc/en-gb/articles/27347149508765-Can-you-set-up-API-integration].

**Health Gorilla.** FHIR R4 first; Lab Network with Quest, Labcorp, BioReference and 100+ labs;
Patient Access Service is API-only and requires NIST IAL2 identity verification [web:
https://healthgorilla.com/home/policies/product-documentation,
https://www.medplum.com/docs/integration/health-gorilla/receiving-results]. Medplum's integration
docs show results arriving as DiagnosticReport + Observation.

**Metriport.** Open source (AGPL, recall), hosted or self-hosted; CommonWell + Carequality;
normalises C-CDA, HL7 v2 and PDF into FHIR R4 with LOINC/SNOMED coding [web:
https://github.com/api-evangelist/metriport, https://deepwiki.com/metriport/metriport]. Access
requires a treatment purpose of use; a personal data server is not a covered entity, so this is
not a consumer path (recall, unverified).

**Particle Health.** Carequality, CommonWell, eHealth Exchange, TEFCA/QHIN, state HIEs,
Surescripts; FHIR R4, C-CDA, Flat, Deltas; a LABS data-retrieval dataset [web:
https://docs.particlehealth.com/docs/data-retrieval-apis]. Same purpose-of-use caveat.

**1upHealth.** FHIR R4 (4.0.1) platform; `GET /{FHIRVersion}/Patient/{id}/$everything`
aggregates every connected source; Patient Connect covers USCDI v1 [web:
https://docs.1up.health/help-center/Content/en-US/get-started/cms-patient-access/developer-overview.html].

**Fasten Health.** `fastenhealth/fasten-onprem`: GPL-3.0, archived and read-only since
2026-07-18; "not able to import data from healthcare providers directly. You can only ... manually
enter data, or upload FHIR Bundles" [web: https://github.com/fastenhealth/fasten-onprem]. Fasten's
historic value is `fasten-sources`: a catalogue of SMART-on-FHIR endpoints and OAuth client
definitions for thousands of US provider portals [web: https://github.com/orgs/fastenhealth/repositories].

**Apple Health Records.** `HKClinicalRecord` wraps an `HKFHIRResource` whose `data` is the raw
FHIR JSON; FHIR R4 supported; read-only; needs the Clinical Health Records entitlement and
per-type consent [web: https://developer.apple.com/videos/play/wwdc2020/10669/,
https://support.mydatahelps.org/hc/en-us/articles/4412375685523-Apple-HealthKitV2-Clinical-Records-Export-Format].
Stanford's `HealthKitOnFHIR` converts HealthKit quantity samples to FHIR Observations with LOINC
and UCUM [web: https://github.com/StanfordBDHG/HealthKitOnFHIR]. A user's Health app XML export
(`export.zip`) also contains `clinical-records/*.json` FHIR files (recall, unverified).

**US Patient Access (SMART on FHIR, CMS).** CMS-0057-F: four FHIR APIs due 2027-01-01; the payer
Patient Access API must include lab results (USCDI) with dates of service on or after 2016-01-01,
within one business day of receipt; FHIR 4.0.1, US Core 3.1.1, SMART App Launch 1.0 [web:
https://www.cms.gov/files/document/cms-0057-f.pdf, https://cms-0057-f.com/patient-access-api].
Blue Button 2.0 is claims only: ExplanationOfBenefit, Patient, Coverage; no lab values [web:
https://bluebutton.cms.gov/data/understanding-the-data/]. US Core Laboratory Result Observation:
`status`, `category=laboratory`, `code` (LOINC, extensible) mandatory; `subject`, `effective[x]`,
`value[x]`, `specimen`, `dataAbsentReason`, `interpretation`, `referenceRange` Must Support;
constraint us-core-3 "SHALL use UCUM for coded quantity units"; us-core-4 "SHOULD use Snomed CT for
coded Results" [web: https://hl7.org/fhir/us/core/STU6.1/StructureDefinition-us-core-observation-lab.html].
Each EHR's patient-facing FHIR endpoint is per health system, so a consumer app needs a directory
of endpoints (Epic publishes one at open.epic.com/MyApps/Endpoints [web]).

**Epic / MyChart.** Free registration at fhir.epic.com; choose audience "Patients"; select
`Observation.Read (Labs) (R4)`, `Observation.Search (Labs) (R4)`, `DiagnosticReport.*`; patient
authorises with MyChart credentials; refresh tokens available for apps that qualify [web:
https://open.epic.com/interface/FHIR, https://fhir.epic.com/Documentation?docId=testpatients].

**NHS (UK).** GP Connect Access Record: Structured, FHIR STU3, covers medications, allergies,
investigations (test results), immunisations; only filed results are returned to patient-facing
services [web: https://developer.nhs.uk/apis/gpconnect-1-6-0/accessrecord_structured.html,
https://digital.nhs.uk/developer/api-catalogue/gp-connect-access-record-structured-fhir]. A
patient-facing variant is a draft IG [web:
https://simplifier.net/guide/GP-Connect-Patient-Facing-Access-Record/Home/Introduction/Introduction?version=current].
There is no general consumer API onto the NHS App; access is limited to assured suppliers (recall).
Consumer UK labs (Thriva, Medichecks, Randox, Forth) are PDF-out.

### 1.3 OCR and PDF parsing: the realistic common path

For consumer labs (Function, Superpower, Medichecks, Ulta, Everlywell, InsideTracker, Randox) the
only artefact a person can export today is a PDF. Every aggregator (InsideTracker, Superpower,
Carrot Care, Metriport) ships a PDF or photo extractor. What a parser must cope with (recall from
real lab PDFs, consistent with the products above):

- One row per analyte with columns: name, value, flag (H, L, sometimes `*`, `A`, `HIGH`), unit,
  reference range (as text: `70-99`, `<200`, `>39`, `See note`, `Desirable <100`), lab site.
- Values with qualifiers: `<0.5`, `>1000`, `NEGATIVE`, `Not Detected`, `1:80`, `TNP` (test not
  performed), `QNS` (quantity not sufficient), `Hemolyzed`, `CANCELLED`.
- Ranges that differ by sex and age, and US "optimal" ranges overlaid by the consumer brand
  (Function, InsideTracker show their own zones beside the lab's).
- Panels: a header (Lipid Panel) with component rows; calculated rows (LDL calc, eGFR, HOMA-IR,
  ratios) mixed with measured rows.
- Units missing on ratio or count rows; units in non-UCUM spelling (`x10E3/uL`, `Thousand/uL`,
  `mIU/L`, `IU/mL`, `mcg/dL`, `µg/dL`).
- Footnotes that change meaning (fasting state, method change, "reference range changed on").
- Corrected reports reissued as a new PDF with "CORRECTED REPORT" in the header.

Implication: a PDF importer is a provider like any other, but its mirror kind is "a parsed lab
report" with a `blobref` to the PDF, and every extracted value needs provenance (page, row) and a
confidence, plus a user-review state. LOINC assignment from free-text names is a separate,
fallible step (the LOINC Top 2000+ is the natural target set for that mapping).

---

## Part 2: standards

### 2.1 FHIR R4 Observation

Field list (R4, 4.0.1) [web: https://hl7.org/fhir/R4/observation.html]: `identifier`, `basedOn`,
`partOf`, `status`, `category`, `code`, `subject`, `focus`, `encounter`, `effective[x]`, `issued`,
`performer`, `value[x]`, `dataAbsentReason`, `interpretation`, `note`, `bodySite`, `method`,
`specimen`, `device`, `referenceRange`, `hasMember`, `derivedFrom`, `component`.

| Element | Shape and notes |
| --- | --- |
| `status` | required, modifier. `registered`, `preliminary`, `final`, `amended`, `corrected`, `cancelled`, `entered-in-error`, `unknown` (the four first fetched [web]; the full list is recall and standard R4) |
| `category` | `laboratory`, `vital-signs`, `social-history`, `imaging`, `survey`, `exam`, `therapy`, `activity`, `procedure` |
| `code` | CodeableConcept; LOINC for labs |
| `effective[x]` | `effectiveDateTime`, `effectivePeriod`, `effectiveTiming`, `effectiveInstant`. For labs this is specimen collection time (physiologically relevant time), not result time |
| `issued` | instant the result was released |
| `value[x]` | 11 types: `Quantity`, `CodeableConcept`, `string`, `boolean`, `integer`, `Range`, `Ratio`, `SampledData`, `time`, `dateTime`, `Period` [web] |
| `dataAbsentReason` | why no value (e.g. `not-performed`, `error`, `unknown`, `asked-unknown`). Invariant obs-6: only present if `value[x]` is absent [web] |
| `interpretation` | 0..*, CodeableConcept from v3 ObservationInterpretation (below) |
| `referenceRange` | 0..*: `low` (SimpleQuantity), `high` (SimpleQuantity), `type` (normal, recommended, treatment, therapeutic, pre, post...), `appliesTo` (sex, race, age group, pregnancy...), `age` (Range), `text` [web] |
| `component` | 0..*: `code`, `value[x]` (same 11 types), `dataAbsentReason`, `interpretation`, `referenceRange`. Used when parts are not meaningful alone (blood pressure systolic + diastolic). Invariant obs-7: a component code must not equal the parent code if the parent has a value [web] |
| `hasMember` | references to other Observations: how a **panel** is represented (CBC Observation with members), or DiagnosticReport.result |
| `derivedFrom` | inputs to a calculated value (eGFR derived from creatinine; HOMA-IR from glucose and insulin) |
| `method` | CodeableConcept (e.g. immunoassay vs LC/MS/MS). Matters: testosterone by LC/MS/MS vs immunoassay differ |
| `specimen` | Reference(Specimen) |
| `device` | Reference(Device or DeviceMetric): analyser, meter, CGM |

**Quantity** [web: https://hl7.org/fhir/R4/datatypes.html#Quantity]: `value` (decimal, precision
is significant: 5.0 is not 5), `comparator` (`<`, `<=`, `>=`, `>`), `unit` (human text), `system`
(`http://unitsofmeasure.org`), `code` (UCUM). Invariant qty-3: if a code is present, the system
must be too. `SimpleQuantity` forbids `comparator` (so referenceRange low and high cannot carry
one). **Range** = `low`, `high` SimpleQuantity, inclusive. **Ratio** = `numerator`,
`denominator` Quantity (titres like 1:80). **SampledData** = `origin`, `period` (ms), `factor`,
`lowerLimit`, `upperLimit`, `dimensions`, `data` (space-separated decimals with `E`, `L`, `U`
codes): the FHIR answer to waveforms and dense series.

**ObservationInterpretation** codes [web:
https://hl7.org/fhir/R4/valueset-observation-interpretation.html]: normality `N`, `A`, `AA`, `H`,
`HH`, `HU`, `L`, `LL`, `LU`; change `B`, `W`, `U`, `D`; susceptibility `S`, `I`, `R`, `NS`, `SDD`,
`SYN-S`, `SYN-R`, `NCL`; presence `POS`, `DET`, `NEG`, `ND`, `IND`, `E`; technical `<`, `>`, `IE`;
other `CAR`, `NR`, `RR`, `WR`, `EXP`, `UNE`, `EX`, `HX`, `LX`. These map one-to-one onto HL7 v2
OBX-8 flags (table 0078).

### 2.2 DiagnosticReport and Specimen

DiagnosticReport [web: https://hl7.org/fhir/R4/diagnosticreport.html]: `status` (`registered`,
`partial`, `preliminary`, `final`, plus `amended`, `corrected`, `appended`, `cancelled`,
`entered-in-error`, `unknown`, the latter from recall), `category` (LAB, plus v2-0074 section
codes such as HM, CH), `code` (LOINC panel code, e.g. 58410-2 CBC), `effective[x]`, `issued`,
`performer`, `resultsInterpreter`, `specimen`, `result` (Reference(Observation)), `conclusion`,
`conclusionCode`, `presentedForm` (the PDF as an Attachment). The report is the "document";
Observations are the atoms.

Specimen (recall): `type` (v2-0487 or SNOMED: serum, plasma, whole blood, urine, saliva, DBS
dried blood spot, capillary blood), `collection.collected[x]`, `collection.method`,
`collection.bodySite`, `collection.fastingStatus[x]` (CodeableConcept from v2-0916 `F` fasting,
`NF` not fasting, `NG` not given, or a Duration), `receivedTime`, `container`, `condition`
(hemolyzed, lipemic, v2-0493). Fasting state is therefore on the Specimen, not the Observation, in
FHIR. LOINC also has `49541-6 Fasting status - Reported` [nlm] and `55420-4 Hours after meal` [nlm]
for when it is reported as its own observation.

### 2.3 Corrected, amended and cancelled results

| Standard | Mechanism |
| --- | --- |
| FHIR | Same resource id, new version (`meta.versionId`), `status` moves `final` to `amended` or `corrected`; `cancelled` if never completed; `entered-in-error` to retract. History via `_history`. Report-level status mirrors. |
| HL7 v2 | OBX-11 result status: `F` final, `P` preliminary, `C` correction (replaces a final result), `A` amended, `X` cannot obtain / cancelled, `W` post original as wrong, `D` delete, `I` pending, `R` not verified [web: https://saga-it.com/docs/hl7/segments/obx]. The lab resends the whole ORU with the corrected OBX. OBR-25 carries report status |
| OMOP | No version or status field; ETL overwrites (or keeps last). Correction history is lost |
| openEHR | Every composition is versioned (`VERSIONED_COMPOSITION`), `test_result_status` / "Overall test status" element (registered, partial, preliminary, final, amended, corrected, appended, cancelled, entered in error) in `laboratory_test_result` (recall) |
| Lab PDFs | A new PDF marked "CORRECTED" or "AMENDED", usually without a machine link to the original |

Fit with substrate: the changelog already keeps every write. A result record keyed by
(source, accession or order id, LOINC) that gets a `put` with `status: corrected` keeps the old
value in the changelog for free; a `status` state machine (`preliminary` to `final` to
`amended`/`corrected`, any to `entered-in-error`) is the natural declaration.

### 2.4 LOINC

Logical Observation Identifiers Names and Codes, Regenstrief Institute. A code is `NNNNN-C` with
a mod-10 check digit. Each term is a 6-axis fully specified name (recall, consistent with the
fields the NLM API exposes):

| Part | Meaning | Examples |
| --- | --- | --- |
| Component (analyte) | what is measured, with optional challenge and adjustment | `Glucose`, `Cholesterol.in LDL`, `Glucose^2H post 75 g glucose PO` |
| Property | kind of quantity | `MCnc` mass concentration, `SCnc` substance (molar) concentration, `ACnc` arbitrary concentration, `NCnc` number concentration (counts), `CCnc` catalytic concentration (enzymes, U/L), `MFr` mass fraction, `VFr` volume fraction, `NFr` number fraction (differential %), `MRto` mass ratio, `EntVol`, `EntMass`, `Titr`, `PrThr` presence/threshold, `Type`, `Imp`, `Find`, `MSCnc` mass or substance concentration (a discouraged "either" property), and a newer `{Measurement}` property |
| Time | `Pt` point in time, `24H` 24-hour collection |
| System | specimen: `Ser/Plas`, `Bld`, `BldA` arterial, `BldV` venous, `BldC` capillary, `RBC`, `Urine`, `Saliva`, `Stool`, `DBS` |
| Scale | `Qn` quantitative, `Ord` ordinal (Pos/Neg, 1+ 2+), `Nom` nominal (organism, genotype), `Nar` narrative, `Doc` document |
| Method | optional: `Automated count`, `Test strip`, `High sensitivity method`, `LC/MS/MS`, `Immunoassay`, `Calculated`, `Detection limit <= 0.005 mIU/L` |

Consequences that matter for a data model:

1. **Property is in the identity, unit is not.** Glucose in mg/dL is `2345-7` (MCnc) and in
   mmol/L is `14749-6` (SCnc) [nlm]. g/dL and g/L of hemoglobin share `718-7` (both MCnc). So a
   mass-to-molar conversion changes the LOINC code; a mg/dL to g/L conversion does not.
2. **Method splits codes.** hs-CRP `30522-7` vs CRP `1988-5`; LDL calculated `13457-7` vs direct
   `18262-6` vs unspecified `2089-1`; TSH `3016-3` vs TSH by detection limit `11580-8` [nlm].
   Two labs measuring "the same" analyte often send different codes; trend views need a grouping
   (LOINC Groups, or a local "analyte" concept above LOINC).
3. **Specimen splits codes.** Glucose serum `2345-7` vs whole blood `2339-0` [nlm]; magnesium serum
   `19123-9` vs RBC `26746-8`/`2597-3` [nlm].
4. **Fasting and challenge live in the component.** `1558-6 Fasting glucose` vs `2345-7` [nlm];
   `20436-2 Glucose 2 hours post dose glucose` [nlm].
5. **Deprecated codes stay in data.** e.g. `21000-5` RDW is Deprecated [nlm] (now `788-0`). Mappings
   must keep the stored code and resolve to the current one.
6. The new `{Measurement}` property (e.g. `108691-7 Urate [Measurement] in Serum or Plasma`
   [nlm]) appears to be a unit-agnostic "any measurement of this analyte" term. Its intended use is
   unverified; it looks designed for exactly the "trend across mass and molar units" problem.

**Answer lists** (`LL` ids): ordinal and nominal codes bind a list of allowed answers (`LA` ids),
e.g. a Positive/Negative list, or Detected/Not detected. Answers commonly also carry SNOMED codes:
Positive `10828004`, Negative `260385009`, Detected `260373001`, Not detected `260415000` (recall).

**Top 2000+**: Regenstrief and NLM publish "LOINC Top 2000+ Lab Observations" in a US version
(mass units) and an SI version (molar units), as PDF, Excel and CSV, at loinc.org/usage [web:
https://loinc.org/news/new-si-version-of-the-loinc-top-2000-lab-observations,
https://lhncbc.nlm.nih.gov/CHRB/Projects/top-LOINC-codes.html]. NLM says the top ~300 codes cover
about 98% of US lab result volume (recall, matches the NLM project page's framing). This list is
the right seed for a substrate catalogue: ship it as data, not code.

**Free lookup**: NLM Clinical Tables LOINC API (used for every [nlm] check in this file) returns
`LOINC_NUM`, `LONG_COMMON_NAME`, `COMPONENT`, `PROPERTY`, `EXAMPLE_UCUM_UNITS`, `STATUS` and more,
no key needed. FHIR terminology servers (`tx.fhir.org`, LOINC's own `fhir.loinc.org`, the latter
needs a free login) expose `$lookup`, `$validate-code`, `$translate` (recall).

Licence: LOINC is free to use under the LOINC licence (attribution, no modification of the
content); redistribution in a product is allowed (recall).

### 2.5 UCUM

The Unified Code for Units of Measure, mandated by FHIR for coded quantities
(`system = http://unitsofmeasure.org`) and by US Core (us-core-3) [web].

Grammar (from the spec [web: https://ucum.org/ucum]):

- Base units: meter `m`, second `s`, gram `g` (not kilogram), radian `rad`, kelvin `K`, coulomb
  `C`, candela `cd`. Everything else is derived.
- Prefixes only on metric atoms (`mg`, `umol`, `dL`, `fL`); `u` is micro in the case-sensitive
  form. Longest-match rule.
- Operators `.` (multiply) and `/` (divide); exponents follow the atom (`m2`, `10*3`, `10*9/L`).
- **Square brackets** enclose customary and special atoms: `[IU]`, `[iU]`, `[degF]`, `[in_i]`,
  `[lb_av]`, `[pH]`, `[mi_i]`, `[ppm]`.
- **Curly-brace annotations** are "meaningless by definition": `mg/g{creat}`, `{cells}/uL`,
  `mL/min/{1.73_m2}`, `{ratio}`. A lone annotation equals `1`. Annotations do not affect
  conversion.
- **Arbitrary units** (`[IU]`, `[arb'U]`, `[USP'U]`, `[APL'U]`, `[GPL'U]`...): "not comparable with
  any other arbitrary unit"; no conversion is possible between them or to mass. WHO IU of
  prolactin, of insulin and of vitamin D are different things that share a symbol.
- **Special units** on non-ratio scales (`Cel`, `[degF]`, `[pH]`, `B` bel, `dB`, `Np`) carry
  conversion functions, not factors (Celsius to kelvin is an offset).
- Two code variants: case-sensitive (`c/s`, the one FHIR uses) and case-insensitive (`c/i`,
  legacy).
- `mol` is a dimensionless count (6.02214076 x 10^23 in the current spec text [web]), so mol and
  g are not commensurable: **converting mg/dL to mmol/L needs the analyte's molar mass**, which is
  a property of the analyte, never of the unit. `eq` (equivalents) additionally needs the charge.

Resources:

- `ucum-essence.xml`: the machine-readable table of all atoms, prefixes and definitions, published
  by the UCUM organisation (github.com/ucum-org/ucum, recall).
- `ucum-lhc` (NLM Lister Hill Center, JavaScript, npm `@lhncbc/ucum-lhc`): validation,
  conversion, and since v6/v7 `convertUnitTo(from, value, to, {molecularWeight, charge})` for
  mass to mole and mole to equivalent conversions; v7.1.1 returns "require molecular weight" when
  missing [web: https://github.com/LHNCBC/ucum-lhc,
  https://github.com/lhncbc/ucum-lhc/blob/master/CHANGELOG.md]. Demo at
  ucum.nlm.nih.gov/ucum-lhc/demo.html.
- Java: `org.fhir:ucum` (the FHIR reference implementation's UCUM service, recall). Python:
  `pint` does not speak UCUM natively; `ucumvert` / `fhir.resources` wrappers exist (recall,
  unverified). Go: no mature UCUM library I know of (recall); substrate would need to parse
  ucum-essence itself or carry a closed table.

Common lab UCUM codes (the strings to store; recall, standard): `mg/dL`, `g/dL`, `g/L`,
`mmol/L`, `umol/L`, `nmol/L`, `pmol/L`, `ng/mL`, `ng/dL`, `pg/mL`, `ug/dL`, `ug/L`, `mg/L`,
`U/L`, `[IU]/L`, `m[IU]/L`, `u[IU]/mL`, `10*3/uL`, `10*6/uL`, `10*9/L`, `10*12/L`, `%`, `fL`, `pg`,
`mL/min/{1.73_m2}`, `mm/h`, `s`, `{INR}`, `{ratio}`, `mg/g{creat}`, `mg/mmol{creat}`, `[pH]`,
`{titer}`, `kU/L` (specific IgE, kUA/L), `[arb'U]/mL`, `mmol/mol` (HbA1c IFCC).

What labs actually send is often not UCUM: `x10E3/uL`, `K/uL`, `thou/mm3`, `mcg/dL`, `mIU/mL`,
`IU/mL`, `mEq/L`, `ratio`, `%` for HbA1c. A canonicaliser needs a synonym table to UCUM first,
then UCUM arithmetic, then (for mass and molar) an analyte table.

#### 2.5.1 Mass vs molar: the analyte table

UCUM converts mg/dL to g/L by itself (factor 0.01). It cannot convert mg/dL to mmol/L. That needs
`factor = 10 / molar_mass` (mg/dL to mmol/L) or `10000 / molar_mass` (mg/dL to umol/L). The table
below is what a canonicaliser must ship per analyte. Factors: multiply the conventional (US) value
by the factor to get SI. Molar masses and factors are recall plus arithmetic; they agree with
widely published SI tables (AMA Manual of Style SI table, Young's tables).

| Analyte | Molar mass used (g/mol) | US unit | SI unit | US to SI factor | Notes |
| --- | --- | --- | --- | --- | --- |
| Glucose | 180.16 | mg/dL | mmol/L | 0.0555 | 100 mg/dL = 5.55 mmol/L |
| Cholesterol (total, HDL, LDL, non-HDL) | 386.65 | mg/dL | mmol/L | 0.02586 | |
| Triglycerides | ~885 (as triolein) | mg/dL | mmol/L | 0.01129 | convention, not a single molecule |
| Creatinine | 113.12 | mg/dL | umol/L | 88.4 | |
| Urea nitrogen (BUN) to urea | 28.01 (N2) | mg/dL | mmol/L urea | 0.357 | UK reports urea, US reports BUN; different analytes |
| Uric acid (urate) | 168.11 | mg/dL | umol/L | 59.48 | |
| Bilirubin | 584.66 | mg/dL | umol/L | 17.1 | |
| Calcium | 40.08 | mg/dL | mmol/L | 0.2495 | |
| Magnesium | 24.305 | mg/dL | mmol/L | 0.4114 | |
| Phosphate (as P) | 30.97 | mg/dL | mmol/L | 0.3229 | |
| Iron, TIBC, UIBC | 55.845 | ug/dL | umol/L | 0.1791 | |
| Zinc | 65.38 | ug/dL | umol/L | 0.153 | |
| Copper | 63.55 | ug/dL | umol/L | 0.1574 | |
| Selenium | 78.97 | ug/L | umol/L | 0.01266 | |
| Lead | 207.2 | ug/dL | umol/L | 0.04826 | |
| Mercury | 200.59 | ug/L | nmol/L | 4.985 | |
| 25-OH vitamin D | 400.6 | ng/mL | nmol/L | 2.496 | |
| Vitamin B12 (cobalamin) | 1355.4 | pg/mL | pmol/L | 0.7378 | |
| Folate | 441.4 | ng/mL | nmol/L | 2.266 | |
| Vitamin A (retinol) | 286.45 | ug/dL | umol/L | 0.0349 | |
| Vitamin E (alpha-tocopherol) | 430.7 | mg/L | umol/L | 2.322 | |
| Vitamin B6 (PLP) | 247.1 | ng/mL | nmol/L | 4.046 | |
| Vitamin C (ascorbate) | 176.12 | mg/dL | umol/L | 56.78 | |
| Homocysteine | 135.18 | mg/L | umol/L | 7.397 | US also reports umol/L |
| Testosterone (total) | 288.42 | ng/dL | nmol/L | 0.03467 | |
| Testosterone (free) | 288.42 | pg/mL | pmol/L | 3.467 | |
| Estradiol | 272.38 | pg/mL | pmol/L | 3.671 | |
| Estrone | 270.37 | pg/mL | pmol/L | 3.699 | |
| Progesterone | 314.46 | ng/mL | nmol/L | 3.180 | |
| Cortisol | 362.46 | ug/dL | nmol/L | 27.59 | |
| DHEA-S | 368.5 | ug/dL | umol/L | 0.02714 | |
| Androstenedione | 286.4 | ng/dL | nmol/L | 0.0349 | |
| DHT | 290.4 | ng/dL | nmol/L | 0.0344 | |
| 17-OH progesterone | 330.5 | ng/dL | nmol/L | 0.0303 | |
| Aldosterone | 360.4 | ng/dL | pmol/L | 27.74 | |
| Free T4 | 776.87 | ng/dL | pmol/L | 12.87 | |
| Total T4 | 776.87 | ug/dL | nmol/L | 12.87 | |
| Free T3 | 650.98 | pg/mL | pmol/L | 1.536 | |
| Total T3 | 650.98 | ng/dL | nmol/L | 0.01536 | |
| Insulin | ~5808 | uIU/mL | pmol/L | 6.0 (some labs 6.945) | IU-based, so the factor is a convention, not a molar mass; refuse silent conversion |
| C-peptide | 3020 | ng/mL | nmol/L | 0.331 | |
| IGF-1 | 7649 | ng/mL | nmol/L | 0.131 | |
| PTH (intact) | 9425 | pg/mL | pmol/L | 0.106 | |
| ACTH | 4541 | pg/mL | pmol/L | 0.220 | |
| AMH | ~140,000 dimer | ng/mL | pmol/L | 7.14 | conventional |
| NT-proBNP | 8457 | pg/mL | pmol/L | 0.118 | |
| BNP | 3464 | pg/mL | pmol/L | 0.289 | |
| Lactate | 90.08 | mg/dL | mmol/L | 0.111 | |
| Ammonia | 17.03 | ug/dL | umol/L | 0.587 | |
| Hemoglobin (monomer, Fe basis) | 16,114 | g/dL | mmol/L | 0.6206 | Netherlands and Scandinavia report Hb in mmol/L; most of Europe g/L (factor 10) |
| HbA1c | n/a | % (NGSP/DCCT) | mmol/mol (IFCC) | IFCC = (NGSP - 2.15) x 10.929 | affine, not a factor; also eAG mg/dL = 28.7 x A1c - 46.7 |
| Lp(a) | varies with apo(a) isoform size | mg/dL | nmol/L | none valid (2.0 to 2.5 is sometimes used) | refuse conversion; store as measured |
| Prolactin | n/a | ng/mL | mIU/L | ~21.2 (WHO IRP 84/500) | assay-calibrator specific, arbitrary unit |
| Sodium, potassium, chloride, bicarbonate | | mEq/L | mmol/L | 1.0 | monovalent; calcium mEq/L x 0.5 = mmol/L |
| Mass-only changes | | g/dL, mg/dL | g/L, mg/L | x10 | UCUM handles these without the table |
| Counts | | 10^3/uL | 10^9/L | 1.0 | UCUM handles |
| Urine albumin/creatinine | | mg/g | mg/mmol | 0.113 | |

Design consequences:

- A canonical value is (analyte concept, property kind, unit). Mass to molar changes the LOINC
  code (2345-7 to 14749-6), so "convert" means "derive a second observation", not "re-express".
- Arbitrary-unit analytes (IU-based hormones, antibodies in U/mL, specific IgE kUA/L, ESR mm/h)
  are only comparable within the same assay. Trends across labs must flag method changes.
- "Exact numbers or refusal": factors are irrational in practice (10/180.16). Converted values
  should be stored as decimals with a declared precision (significant figures of the source), and
  marked derived with the factor used, so a rebuild reproduces them byte for byte.

### 2.6 SNOMED CT for findings

SNOMED CT is the coded-answer and finding vocabulary next to LOINC (LOINC names the question,
SNOMED the answer or the clinical finding). US Core us-core-4 says coded results SHOULD use SNOMED
[web]. Typical uses: qualitative answers (Positive 10828004, Negative 260385009, Detected
260373001, Not detected 260415000), organisms in microbiology, specimen types (Serum 119364003,
Plasma 119361006, Venous blood 122555007, Urine 122575003: recall), and findings derived from
labs (Hyperlipidemia 55822004, Iron deficiency anemia 87522002: recall). SNOMED requires an
affiliate licence outside member countries (US and UK are members, so free for users there;
recall). LOINC and SNOMED International have a cooperation agreement and a LOINC Ontology
extension that expresses LOINC parts in SNOMED (recall).

### 2.7 HL7 v2 ORU^R01

The wire format every US lab speaks to EHRs. Message `ORU^R01`: `MSH` header, `PID` patient,
`ORC` common order, `OBR` observation request (panel, accession, collection time OBR-7, result
status OBR-25), then one `OBX` per result, `NTE` notes, and `SPM` specimen in 2.5.1 [web:
https://saga-it.com/docs/hl7/messages/oru].

| OBX field | Meaning |
| --- | --- |
| OBX-2 | value type: `NM` numeric, `SN` structured numeric, `ST` string, `TX` text, `CE`/`CWE` coded, `ED` embedded document (PDF), `DT`, `TM`, `TS` |
| OBX-3 | observation identifier: LOINC code plus the lab's local code |
| OBX-4 | sub-id (groups repeated or related OBX) |
| OBX-5 | value |
| OBX-6 | units (UCUM in 2.5.1 Meaningful Use profiles, else free text) |
| OBX-7 | reference range as string, e.g. `13.0-17.0`, `<200` |
| OBX-8 | abnormal flags: `N`, `L`, `H`, `LL`, `HH`, `A`, `AA`, `<`, `>`, `POS`, `NEG`... (table 0078) |
| OBX-11 | result status `F`, `P`, `C`, `A`, `X`, `W`, `D`, `I`, `R` |
| OBX-14 | date/time of the observation |
| OBX-17 | method |
| OBX-18 | equipment instance |
| OBX-19 | analysis date/time |
| OBX-23..25 | performing organisation name, address, medical director (CLIA) |

The `SN` structured numeric type is the v2 answer to qualified values:
`<comparator>^<num1>^<separator/suffix>^<num2>`, so `<^5` is "<5", `^1^:^80` is a 1:80 titre,
`^10^-^20` is a range "10-20" (recall, standard).

### 2.8 openEHR

Lab results use `OBSERVATION.laboratory_test_result.v1` with one or more
`CLUSTER.laboratory_test_analyte.v1` slotted in, even for a single analyte, for query consistency
[web: https://openehr.atlassian.net/wiki/spaces/healthmod/pages/91139266/Implementing+Laboratory+Tests+in+openEHR].
The analyte cluster holds analyte name (coded, LOINC), result value (`DV_QUANTITY` with UCUM
units, or `DV_CODED_TEXT`, `DV_PROPORTION`, `DV_COUNT`, `DV_TEXT`), reference range guidance,
interpretation, validation time, result status, comment (recall). `DV_QUANTITY` carries
`magnitude`, `units`, `precision`, plus `normal_range`, `normal_status`, `magnitude_status`
(`<`, `>`, `<=`, `>=`, `=`, `~`): openEHR's comparator (recall). Queried with AQL
(`SELECT ... FROM EHR e CONTAINS COMPOSITION c CONTAINS OBSERVATION o[openEHR-EHR-OBSERVATION.laboratory_test_result.v1] CONTAINS CLUSTER a[openEHR-EHR-CLUSTER.laboratory_test_analyte.v1]`)
[web: https://docs.ehrbase.org/docs/EHRbase/Explore/AQL/Contains]. Takeaway: openEHR treats the
panel as the unit of storage and the analyte as a nested cluster, which is the opposite choice
from FHIR (atom first, panel by reference).

### 2.9 OMOP CDM MEASUREMENT (v5.4)

Columns [web: https://ohdsi.github.io/CommonDataModel/cdm54.html]: `measurement_id`,
`person_id`, `measurement_concept_id` (standard concept, usually LOINC), `measurement_date`,
`measurement_datetime`, `measurement_time`, `measurement_type_concept_id` (provenance: lab,
EHR, patient-reported), `operator_concept_id` (`<`, `<=`, `=`, `>=`, `>`), `value_as_number`
(float), `value_as_concept_id` (qualitative answer), `unit_concept_id` (UCUM concept),
`range_low`, `range_high` (same unit as the value), `provider_id`, `visit_occurrence_id`,
`visit_detail_id`, `measurement_source_value`, `measurement_source_concept_id`,
`unit_source_value`, `unit_source_concept_id`, `value_source_value`, `measurement_event_id`,
`meas_event_field_concept_id`.

Lessons: OMOP is the shape analysts use for population queries (one narrow row per value, with a
comparator column, a concept for qualitative values, and the source strings kept beside the
standard ids). `value_as_number` and `value_as_concept_id` may both be filled [web:
https://forums.ohdsi.org/t/measurement-table-are-value-as-concept-id-and-value-as-number-mutually-exclusive/10975].
No status or correction history, no specimen, no interpretation flag (derive from range), float
values.

### 2.10 Open mHealth and IEEE 1752.1

Open mHealth's `omh:unit-value` is `{value: number, unit: string}`, both required, unit drawn
from UCUM, SI and English units, constrained per schema (blood-glucose limits units to `mg/dL`
and `mmol/L`) [web: https://www.openmhealth.org/schemas/omh_blood-glucose/,
https://github.com/openmhealth/schemas/blob/main/schema/omh/unit-value-1.0.json]. The OMH schemas
are deprecated in favour of IEEE 1752.1 (2021), which kept the same shapes [web, same source].
Data points have a header (id, creation time, schema id, acquisition provenance: source name,
modality `sensed` vs `self-reported`) and a body; time is `effective_time_frame` (a
`date_time` or a `time_interval`) and summary values carry `descriptive_statistic` (average,
maximum, minimum, count, median...) (recall). Blood glucose adds `specimen_source`,
`temporal_relationship_to_meal`, `temporal_relationship_to_sleep` (recall). Good model for
wearables; thin for labs (no reference range, no LOINC).

### 2.11 IEEE 11073 and ISO/IEEE PHD

ISO/IEEE 11073-10101 is the medical device nomenclature (MDC codes: 32-bit = 16-bit partition +
16-bit term, e.g. `MDC_CONC_GLU_CAPILLARY_WHOLEBLOOD`, `MDC_PRESS_BLD_NONINV_SYS`,
`MDC_PULS_OXIM_SAT_O2`) [web: https://terminology.hl7.org/6.4.0/CodeSystem-v3-mdc.html,
https://wiki.ihe.net/index.php/PCD_Rosetta_Terminology_Mapping_Overview]. The Rosetta Terminology
Mapping (RTMMS at NIST) lists, per MDC code, the allowed UCUM units and enumerations. ISO/IEEE
11073-20601 plus the 104xx device specialisations (10417 glucose meter, 10407 BP, 10415 weighing
scale) are the Personal Health Device (PHD) protocol, carried today over Bluetooth Low Energy
(Bluetooth SIG "Generic Health Sensor" uses 11073 nomenclature) [web:
https://www.bluetooth.com/wp-content/uploads/2024/03/GenericHealthSensorGuide.pdf]. FHIR's
Personal Health Device IG maps PHD observations to Observation with both MDC and LOINC codes
(recall). Relevance: home devices (glucose meters, BP cuffs, scales) speak MDC, labs speak LOINC;
vital-sign LOINCs in FHIR have MDC equivalents.

### 2.12 Qualified and awkward values: how each standard carries them

| Case | Example | FHIR R4 | HL7 v2 | OMOP | Suggested substrate shape |
| --- | --- | --- | --- | --- | --- |
| Below detection | `<5 mg/L` | `valueQuantity {value: 5, comparator: "<"}`; interpretation `<` optional | `SN` `<^5` | `operator_concept_id` `<`, `value_as_number` 5 | value decimal + `comparator` enum |
| Above range | `>1000` | comparator `>` | `SN` `>^1000` | operator `>` | same |
| Qualitative | `Not detected` | `valueCodeableConcept` SNOMED 260415000 or LOINC LA answer | `CWE` | `value_as_concept_id` | coded value: system + code + display |
| Ordinal | `2+` protein | `valueCodeableConcept` or `valueString` | `ST`/`CWE` | concept | coded value |
| Titre | `1:80` | `valueRatio {numerator 1, denominator 80}` (or `valueString`) | `SN` `^1^:^80` | `value_as_number` 80 (lossy) | ratio pair, plus derived denominator for ordering |
| Text | `See comment` | `valueString`, `note` | `TX`/`FT` | `value_source_value` | text value |
| Not performed | `TNP`, `QNS`, hemolyzed | no value, `dataAbsentReason`; specimen `condition` | OBX-11 `X`, NTE | row dropped | absent reason enum |
| Range result | `10-20` | `valueRange` | `SN` `^10^-^20` | lossy | low/high pair |
| Panel | CBC | DiagnosticReport.result or Observation.hasMember | OBR + OBX* | none (flat rows) | report record + result records referencing it |
| Multi-part | BP 120/80 | `component` | two OBX with OBX-4 sub-id | two rows | components, or two records |
| Calculated | eGFR, HOMA-IR, LDL calc | `derivedFrom` + method `Calculated` | OBX with method | row | reference to inputs + formula id |
| Fasting | fasting glucose | Specimen.collection.fastingStatus, or LOINC 1558-6 | SPM, or OBX for 49541-6 | none | property on the specimen/report |
| Specimen | serum vs whole blood | `specimen`, and LOINC system axis | SPM-4 | none (in concept) | enum + implicit in code |
| Lab range | 70-99 | `referenceRange[]` low/high/text, appliesTo, age | OBX-7 string | range_low/high | list of range objects, with text kept verbatim |
| Range by sex/age | Testosterone M vs F | referenceRange.appliesTo, .age | lab picks one, sends string | one pair | lab-supplied range wins; catalogue range is fallback |
| Flags | H, L, HH, LL, A | `interpretation` | OBX-8 | none | enum, from source; derived flag kept separate |
| Correction | value changed | status `corrected`, new version | OBX-11 `C` | overwrite | status state + changelog history |

Reference ranges vary by lab, method, sex, age, pregnancy trimester, menstrual phase, time of day
(cortisol), and ethnicity (eGFR used to, until CKD-EPI 2021 dropped race: `98979-8` [nlm] vs the
race-split MDRD `48642-3`/`48643-1` [nlm]). Consumer services add "optimal" ranges that are not
lab reference ranges. A result must carry the range that came with it; a catalogue range is a
labelled fallback, never a silent replacement.

---

## Part 3: biomarker catalogue

### 3.1 Legend

- **LOINC**: primary US (mass) code first; SI (molar) code after `/` where a separate code
  exists. All [nlm]-verified except entries marked (r).
- **Units**: as UCUM codes. **Factor**: multiply US value to get SI. `=` means same unit both
  sides. `n/a` means no valid conversion.
- **Range**: typical adult reference interval (recall). Every lab publishes its own; ranges
  differ by sex, age, method and lab; do not hard-code.
- **Offered by** (consumer services; based on public marketing pages and reviews, category-level
  and not verified per marker): `All` = basic chemistry offered by essentially every service
  (Function, Superpower, InsideTracker, Everlywell, SiPhox, Levels, Thriva, Medichecks, Randox,
  Quest and Labcorp consumer, Ulta). `Lng` = longevity tier (Function, Superpower, InsideTracker
  Ultimate, Levels Complete, Medichecks/Randox advanced, SiPhox). `Hor` = hormone panels (most
  services, sex-specific). `Spc` = specialty labs (Genova, Doctor's Data, Vibrant, Boston Heart,
  Cleveland HeartLab, OmegaQuant, TruDiagnostic) or a la carte from Quest/Labcorp consumer
  menus. `Clin` = clinical only, rarely consumer.

### 3.2 Complete blood count (panel `58410-2` CBC panel - Blood by Automated count [nlm])

| # | Marker (aliases) | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | White blood cells (WBC, leukocytes) | 6690-2 | 10*3/uL | 10*9/L | = | 4.0-11.0 | infection, inflammation, marrow | All |
| 2 | Red blood cells (RBC, erythrocytes) | 789-8 | 10*6/uL | 10*12/L | = | M 4.5-5.9, F 4.1-5.1 | anaemia, polycythaemia | All |
| 3 | Hemoglobin (Hb, Hgb) | 718-7 / 59260-0 (mmol) | g/dL | g/L | x10 (mmol/L x0.6206) | M 13.5-17.5, F 12.0-15.5 g/dL | oxygen capacity, anaemia | All |
| 4 | Hematocrit (Hct, PCV) | 4544-3 | % | L/L | x0.01 | M 41-53, F 36-46 % | red cell volume fraction | All |
| 5 | MCV (mean corpuscular volume) | 787-2 | fL | fL | = | 80-100 | microcytic vs macrocytic anaemia; B12/folate | All |
| 6 | MCH | 785-6 | pg | pg | = | 27-33 | Hb per cell | All |
| 7 | MCHC | 786-4 | g/dL | g/L | x10 | 32-36 g/dL | Hb concentration in cells | All |
| 8 | RDW (RDW-CV) | 788-0 (21000-5 deprecated) | % | % | = | 11.5-14.5 | red cell size variation; mortality marker; PhenoAge input | All |
| 9 | Platelets (PLT) | 777-3 | 10*3/uL | 10*9/L | = | 150-400 | clotting, marrow | All |
| 10 | MPV (mean platelet volume) | 32623-1 | fL | fL | = | 7.5-11.5 | platelet turnover | All |
| 11 | Neutrophils, absolute | 751-8 | 10*3/uL | 10*9/L | = | 1.5-7.8 | bacterial infection, stress | All |
| 12 | Neutrophils % | 770-8 | % | % | = | 40-75 | | All |
| 13 | Lymphocytes, absolute | 731-0 | 10*3/uL | 10*9/L | = | 1.0-4.0 | viral infection, immune status | All |
| 14 | Lymphocytes % | 736-9 | % | % | = | 20-45 | PhenoAge input | All |
| 15 | Monocytes, absolute | 742-7 | 10*3/uL | 10*9/L | = | 0.2-0.9 | chronic inflammation | All |
| 16 | Monocytes % | 5905-5 | % | % | = | 2-10 | | All |
| 17 | Eosinophils, absolute | 711-2 | 10*3/uL | 10*9/L | = | 0.0-0.5 | allergy, parasites | All |
| 18 | Eosinophils % | 713-8 | % | % | = | 0-6 | | All |
| 19 | Basophils, absolute | 704-7 | 10*3/uL | 10*9/L | = | 0.0-0.2 | allergy, myeloproliferative | All |
| 20 | Basophils % | 706-2 | % | % | = | 0-2 | | All |
| 21 | Reticulocytes % | 17849-1 / 4679-7 | % | % | = | 0.5-2.5 | marrow response | Spc |
| 22 | Neutrophil to lymphocyte ratio (NLR) | none found (derived) | {ratio} | | = | ~1-3 | systemic inflammation | Lng (derived) |

### 3.3 Metabolic panel: CMP and BMP (panels `24323-8` CMP, `51990-0` BMP [nlm])

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range (US) | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 23 | Glucose | 2345-7 / 14749-6 | mg/dL | mmol/L | 0.0555 | 70-99 fasting | glycaemia | All |
| 24 | Urea nitrogen (BUN) | 3094-0 / 14937-7 (urea N mol), 22664-7 (urea) | mg/dL | mmol/L | 0.357 (as urea) | 7-20 | kidney, protein intake, hydration | All |
| 25 | Creatinine | 2160-0 / 14682-9 | mg/dL | umol/L | 88.4 | M 0.74-1.35, F 0.59-1.04 | kidney filtration, muscle mass | All |
| 26 | BUN/creatinine ratio | 3097-3 | {ratio} | | = | 10-20 | prerenal vs renal | All |
| 27 | eGFR, CKD-EPI 2021 (race-free) | 98979-8 (older: 62238-1, 33914-3, 48642-3, 48643-1) | mL/min/{1.73_m2} | same | = | >=90 (60-89 mildly reduced) | kidney function, CKD staging | All |
| 28 | Sodium | 2951-2 | mmol/L (mEq/L) | mmol/L | = | 135-145 | water balance | All |
| 29 | Potassium | 2823-3 | mmol/L | mmol/L | = | 3.5-5.1 | cardiac, renal | All |
| 30 | Chloride | 2075-0 | mmol/L | mmol/L | = | 98-107 | acid-base | All |
| 31 | Carbon dioxide, total (bicarbonate) | 2028-9 | mmol/L | mmol/L | = | 22-29 | acid-base | All |
| 32 | Anion gap | 1863-0 | mmol/L | mmol/L | = | 8-16 | metabolic acidosis (calculated) | Most |
| 33 | Calcium | 17861-6 / 2000-8 | mg/dL | mmol/L | 0.2495 | 8.6-10.3 | bone, parathyroid | All |
| 34 | Calcium, ionized | 1994-3 | mmol/L | mmol/L | = | 1.12-1.32 | free calcium | Clin |
| 35 | Protein, total | 2885-2 | g/dL | g/L | x10 | 6.0-8.3 | nutrition, liver, immunoglobulins | All |
| 36 | Albumin | 1751-7 (BCG 61151-7, BCP 61152-5) | g/dL | g/L | x10 | 3.5-5.0 | liver synthesis, nutrition; PhenoAge input | All |
| 37 | Globulin (calculated) | 10834-0 | g/dL | g/L | x10 | 2.0-3.5 | immune proteins | All |
| 38 | Albumin/globulin ratio | 1759-0 | {ratio} | | = | 1.1-2.5 | | All |
| 39 | Bilirubin, total | 1975-2 / 14631-6 | mg/dL | umol/L | 17.1 | 0.1-1.2 | liver, haemolysis, Gilbert's | All |
| 40 | Alkaline phosphatase (ALP) | 6768-6 | U/L | U/L (ukat/L x0.0167) | = | 44-147 | liver (cholestasis), bone; PhenoAge input | All |
| 41 | AST (SGOT) | 1920-8 | U/L | U/L | = | 10-40 | liver, muscle | All |
| 42 | ALT (SGPT) | 1742-6 | U/L | U/L | = | 7-56 (lab-specific; "optimal" lower) | liver injury, NAFLD | All |
| 43 | AST/ALT ratio (De Ritis) | 1916-6 | {ratio} | | = | ~0.8-1.5 | alcoholic vs fatty liver | Derived |

### 3.4 Extended liver, pancreas, muscle (hepatic panel `24325-3` [nlm])

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 44 | GGT (gamma-glutamyl transferase) | 2324-2 | U/L | U/L | = | M 8-61, F 5-36 | biliary, alcohol, oxidative stress, CV risk | All |
| 45 | Bilirubin, direct (conjugated) | 1968-7 | mg/dL | umol/L | 17.1 | 0.0-0.3 | cholestasis | Most |
| 46 | Bilirubin, indirect (unconjugated) | 1971-1 / 14630-8 | mg/dL | umol/L | 17.1 | 0.2-0.8 | haemolysis, Gilbert's | Most |
| 47 | LDH (lactate dehydrogenase) | 2532-0 (14804-9 by method) | U/L | U/L | = | 140-280 | tissue damage, haemolysis | Lng |
| 48 | Amylase | 1798-8 | U/L | U/L | = | 30-110 | pancreas | Lng |
| 49 | Lipase | 3040-3 | U/L | U/L | = | 10-140 | pancreas | Lng |
| 50 | Creatine kinase (CK) | 2157-6 | U/L | U/L | = | M 39-308, F 26-192 | muscle damage, statin myopathy, training | Lng |
| 51 | Ammonia | (r) 16362-6 | ug/dL | umol/L | 0.587 | 15-45 | liver failure | Clin |
| 52 | Alpha-1 antitrypsin | 1825-9 | mg/dL | g/L | x0.01 | 100-200 | AATD (lung, liver) | Spc |

### 3.5 Lipids and cardiovascular risk (lipid panels `24331-1`, `57698-3` [nlm])

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range / target | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 53 | Cholesterol, total | 2093-3 / 14647-2 | mg/dL | mmol/L | 0.02586 | <200 | lipid status | All |
| 54 | Triglycerides | 2571-8 / 14927-8 | mg/dL | mmol/L | 0.01129 | <150 fasting | insulin resistance, diet | All |
| 55 | HDL cholesterol | 2085-9 / 14646-4 | mg/dL | mmol/L | 0.02586 | M >40, F >50 | reverse transport | All |
| 56 | LDL cholesterol, calculated (Friedewald, Martin-Hopkins, NIH/Sampson) | 13457-7 | mg/dL | mmol/L | 0.02586 | <100 (risk-dependent) | atherogenic cholesterol | All |
| 57 | LDL cholesterol, direct | 18262-6 / 22748-8 (moles, any method) | mg/dL | mmol/L | 0.02586 | <100 | | Most |
| 58 | LDL cholesterol (unspecified method) | 2089-1 | mg/dL | mmol/L | 0.02586 | | | |
| 59 | Non-HDL cholesterol | 43396-1 / 70204-3 | mg/dL | mmol/L | 0.02586 | <130 | all atherogenic lipoproteins | All |
| 60 | Total cholesterol/HDL ratio | 9830-1 | {ratio} | | = | <5 (ideal <3.5) | | All |
| 61 | LDL/HDL ratio | 11054-4 | {ratio} | | = | <3.5 | | Some |
| 62 | Apolipoprotein B (ApoB) | 1884-6 / 76482-9; ApoB-100 1871-3 | mg/dL | g/L | x0.01 | <90 (risk targets <80, <65) | atherogenic particle count; best single lipid risk marker | Lng, Function, Superpower, SiPhox |
| 63 | Apolipoprotein A-I | 1869-7 | mg/dL | g/L | x0.01 | M >120, F >140 | HDL particle protein | Lng |
| 64 | ApoB/ApoA-I ratio | 9340-1 is percentile; (r) ratio code | {ratio} | | = | <0.7 to 0.8 | | Spc |
| 65 | Lipoprotein(a), mass | 10835-7 | mg/dL | | n/a | <30 mg/dL | inherited ASCVD and aortic stenosis risk, measure once | Lng |
| 66 | Lipoprotein(a), molar | 43583-4 | nmol/L | nmol/L | n/a | <75 nmol/L (some use <125) | preferred (isoform-independent) | Lng |
| 67 | LDL particle number (LDL-P, NMR) | 54434-6 (Lipoprotein.beta.subparticle [Moles/volume]) | nmol/L | nmol/L | = | <1000 | particle burden | Spc (Labcorp NMR LipoProfile) |
| 68 | Small dense LDL cholesterol | 90364-1 / 96959-2 | mg/dL | mmol/L | 0.02586 | <30 | atherogenic subfraction | Spc |
| 69 | LDL size / pattern (A/B) | 17782-4, 35505-7 | nm / type | | | pattern A | subfraction | Spc |
| 70 | Lp-PLA2 (mass / activity) | 39804-0 / 86951-1 | ng/mL; nmol/min/mL | | | activity <225 | vascular inflammation | Spc |
| 71 | Oxidized LDL | 54238-1 / 111519-5 | U/L | | | lab-specific | oxidative stress | Spc |
| 72 | Myeloperoxidase (MPO) | 99407-9 | pmol/L or ng/mL | | | <470 pmol/L | plaque instability | Spc |
| 73 | TMAO (trimethylamine N-oxide) | 90360-9 | umol/L | umol/L | = | <6.2 | gut-derived CV risk | Spc |
| 74 | Lipoprotein insulin resistance score (LP-IR) | 62255-5 | score | | | <45 | insulin resistance from NMR | Spc |
| 75 | GlycA | 82730-3 | umol/L | umol/L | = | <400 | systemic inflammation (NMR) | Spc, Lng |

### 3.6 Glucose, insulin and metabolic health

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 76 | Glucose, fasting | 1558-6 | mg/dL | mmol/L | 0.0555 | 70-99; 100-125 prediabetes | glycaemia | All |
| 77 | Glucose, whole blood (meter) | 2339-0 | mg/dL | mmol/L | 0.0555 | | capillary | devices |
| 78 | Glucose 2 h post 75 g (OGTT) | 20436-2 | mg/dL | mmol/L | 0.0555 | <140 | glucose tolerance | Clin |
| 79 | HbA1c | 4548-4 (by HPLC 17856-6, calc 17855-8) | % | mmol/mol | (x-2.15)x10.929 | <5.7 %; 5.7-6.4 prediabetes; >=6.5 diabetes | 3-month glycaemia | All |
| 80 | Estimated average glucose (eAG) | 27353-2 | mg/dL | mmol/L | 0.0555 | | derived from A1c | Most (derived) |
| 81 | Insulin, fasting | 20448-7 / 14796-7 | u[IU]/mL | pmol/L | 6.0 (convention) | 2-20 ("optimal" <8) | insulin resistance | Lng, Function, Superpower, SiPhox |
| 82 | HOMA-IR | 47214-2 (Homeostasis model assessment) | {index} | | = | <1.9 (cut-offs vary) | insulin resistance = glucose(mg/dL) x insulin / 405 | Lng (derived) |
| 83 | C-peptide | 1986-9 | ng/mL | nmol/L | 0.331 | 0.8-3.5 | endogenous insulin production | Spc |
| 84 | Fructosamine | 33805-3 / 15069-8 | umol/L | umol/L | = | 200-285 | 2-3 week glycaemia | Spc |
| 85 | Adiponectin | 47828-9 | ug/mL | mg/L | = | sex-specific, ~4-26 | insulin sensitivity | Spc |
| 86 | Leptin | 21365-2 | ng/mL | ug/L | = | sex and BMI dependent | satiety, adiposity | Spc |
| 87 | Beta-hydroxybutyrate | 6873-4 | mmol/L | mmol/L | = | <0.4 (ketosis 0.5-3) | ketosis | Spc, devices |
| 88 | Lactate | 2524-7 | mmol/L | mmol/L | = | 0.5-2.2 | anaerobic metabolism | Clin, sport |
| 89 | Insulin resistance score (CardioIQ) | 92845-7 | score | | | | Quest proprietary | Spc |

### 3.7 Thyroid

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 90 | TSH | 3016-3 (high-sens 11580-8, 11579-0) | m[IU]/L (uIU/mL) | m[IU]/L | = | 0.4-4.5 | thyroid axis screen | All |
| 91 | Free T4 | 3024-7 / 14920-3 | ng/dL | pmol/L | 12.87 | 0.8-1.8 | thyroid hormone | All |
| 92 | Free T3 | 3051-0 / 14928-6 | pg/mL | pmol/L | 1.536 | 2.3-4.2 | active hormone | Most |
| 93 | Total T4 | 3026-2 | ug/dL | nmol/L | 12.87 | 5.0-12.0 | | Some |
| 94 | Total T3 | 3053-6 | ng/dL | nmol/L | 0.01536 | 80-200 | | Some |
| 95 | Reverse T3 | 3052-8 / 14929-4 | ng/dL | nmol/L | 0.01536 | 10-24 | conversion, illness | Lng, Spc |
| 96 | TPO antibodies | 8099-4 | [IU]/mL | kU/L | = | <35 (assay) | Hashimoto's | Lng, Function, Medichecks |
| 97 | Thyroglobulin antibodies | 8098-6 | [IU]/mL | kU/L | = | <4 (assay) | autoimmune thyroid | Lng |
| 98 | Thyroglobulin | 3013-0 | ng/mL | ug/L | = | 1.5-38 | thyroid cancer follow-up | Clin |
| 99 | TSI (thyroid stimulating immunoglobulin) | 30567-2 | % or [IU]/L | | | <140 % baseline | Graves' | Clin |
| 100 | TSH receptor antibody (TRAb) | 5385-0 | [IU]/L | | | <1.75 | Graves' | Clin |

### 3.8 Iron studies (panel `50190-8` [nlm])

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 101 | Iron, serum | 2498-4 / 14798-3 | ug/dL | umol/L | 0.1791 | 60-170 | circulating iron | All |
| 102 | TIBC | 2500-7 | ug/dL | umol/L | 0.1791 | 250-450 | transferrin capacity | All |
| 103 | UIBC | 2501-5 / 22753-8 | ug/dL | umol/L | 0.1791 | 110-370 | | Most |
| 104 | Transferrin saturation | 2502-3 | % | fraction | x0.01 | 20-50 | iron deficiency vs overload (haemochromatosis >45) | All |
| 105 | Ferritin | 2276-4 / 14723-1 | ng/mL | ug/L | = (pmol/L x2.247) | M 30-400, F 15-150 | iron stores; acute phase | All |
| 106 | Transferrin | 3034-6 | mg/dL | g/L | x0.01 | 200-360 | | Some |
| 107 | Soluble transferrin receptor | 30248-9 | mg/L | mg/L | = | 1.8-4.6 | iron deficiency despite inflammation | Spc |
| 108 | Haptoglobin | 4542-7 | mg/dL | g/L | x0.01 | 30-200 | haemolysis | Clin |

### 3.9 Vitamins

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 109 | 25-hydroxyvitamin D (D2+D3) | 62292-8 / 68438-1; D3 only 1989-3 | ng/mL | nmol/L | 2.496 | 30-100 (deficient <20) | vitamin D status | All |
| 110 | 1,25-dihydroxyvitamin D (calcitriol) | 1649-3 / 14566-4 | pg/mL | pmol/L | 2.6 | 18-72 | active hormone; not a status marker | Clin |
| 111 | Vitamin B12 | 2132-9 / 14685-2 | pg/mL | pmol/L | 0.7378 | 200-900 | B12 status | All |
| 112 | Active B12 (holotranscobalamin) | (r) not found | pmol/L | pmol/L | = | >50 | early B12 deficiency | Medichecks, Thriva |
| 113 | Methylmalonic acid | 13964-2 (moles); 2629-4 (mass) | nmol/L | nmol/L | = | <400 | functional B12 deficiency | Spc |
| 114 | Folate, serum | 2284-8 / 14732-2 | ng/mL | nmol/L | 2.266 | >5.4 | folate status | Most |
| 115 | Folate, RBC | 2283-0 / 14731-4 | ng/mL | nmol/L | 2.266 | >280 | long-term folate | Lng |
| 116 | Homocysteine | 13965-9 (moles); 2428-1 (mass) | umol/L | umol/L | = | 5-15 ("optimal" <9) | B12/folate/B6 status, CV risk, MTHFR | Lng, Function, Superpower |
| 117 | Vitamin A (retinol) | 2923-1 / 14905-4 | ug/dL | umol/L | 0.0349 | 20-80 | | Spc |
| 118 | Vitamin E (alpha-tocopherol) | 1823-4 / 14590-4 | mg/L | umol/L | 2.322 | 5.5-17 | | Spc |
| 119 | Vitamin B6 (pyridoxal 5-phosphate) | 30552-4 / 62236-5 | ng/mL | nmol/L | 4.046 | 5-50 | | Spc |
| 120 | Vitamin B1 (thiamine, whole blood) | 2998-3 / 32554-8 | nmol/L or ug/L | nmol/L | | 70-180 nmol/L | | Spc |
| 121 | Vitamin B2 (riboflavin) | 2924-9 | ug/L | nmol/L | 2.657 | 1-19 | | Spc |
| 122 | Vitamin C (ascorbate) | 14622-5 (moles) | mg/dL | umol/L | 56.78 | 0.4-2.0 mg/dL | | Spc |
| 123 | Vitamin K1 (phylloquinone) | 9622-2 / 58793-1 | ng/mL | nmol/L | 2.22 | 0.1-2.2 | | Spc |

### 3.10 Minerals and electrolytes beyond the CMP

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 124 | Magnesium, serum | 19123-9 / 2601-3 | mg/dL | mmol/L | 0.4114 | 1.7-2.2 | Mg status (insensitive) | Most |
| 125 | Magnesium, RBC | 26746-8 (mass) / 2597-3 (moles) | mg/dL | mmol/L | 0.4114 | 4.2-6.8 mg/dL | intracellular Mg | Lng, Function, Superpower |
| 126 | Phosphate (phosphorus) | 2777-1 / 14879-1 | mg/dL | mmol/L | 0.3229 | 2.5-4.5 | bone, kidney | Most |
| 127 | Zinc | 5763-8 / 14955-9 | ug/dL | umol/L | 0.153 | 60-130 | immune, skin, testosterone | Lng |
| 128 | Copper | 5631-7 / 14665-4 | ug/dL | umol/L | 0.1574 | 70-140 | Wilson's, deficiency | Lng |
| 129 | Ceruloplasmin | 2064-4 / 47600-2 | mg/dL | g/L | x0.01 | 20-35 | copper transport | Spc |
| 130 | Selenium, serum/plasma | 5724-0 (whole blood 5722-4) | ug/L | umol/L | 0.01266 | 70-150 | thyroid, antioxidant | Lng |
| 131 | Iodine, urine | 2495-0 | ug/L | ug/L | = | 100-199 (population median) | iodine intake | Spc |

### 3.11 Heavy metals and toxins

| # | Marker | LOINC | US unit | SI unit | Factor | Typical / action level | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 132 | Lead, blood | 5671-3 (venous 77307-7) / 14807-2 | ug/dL | umol/L | 0.04826 | <3.5 (CDC reference value) | exposure | Function, Superpower, Spc |
| 133 | Mercury, blood | 5685-3 | ug/L | nmol/L | 4.985 | <10 | fish intake, exposure | Function, Superpower, Spc |
| 134 | Arsenic, blood | 5583-0 | ug/L | nmol/L | 13.35 | <13 | exposure | Spc |
| 135 | Cadmium, blood | 5609-3 | ug/L | nmol/L | 8.9 | <1.2 non-smoker | smoking, exposure | Spc |
| 136 | Aluminium, serum | 5574-9 | ug/L | umol/L | 0.0371 | <6 | dialysis, exposure | Spc |
| 137 | Cotinine | 110656-6 ([Measurement]) | ng/mL | | | <10 non-smoker | nicotine exposure | Spc |

### 3.12 Male hormones

| # | Marker | LOINC | US unit | SI unit | Factor | Typical adult male range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 138 | Testosterone, total | 2986-8 / 14913-8 | ng/dL | nmol/L | 0.03467 | 264-916 (method dependent) | androgen status | Hor, All men's panels |
| 139 | Testosterone, free | 2991-8 / 14914-6 | pg/mL | pmol/L | 3.467 | 46-224 (calc. or equilibrium dialysis) | bioactive androgen | Hor |
| 140 | Testosterone, bioavailable (free + weakly bound) | 2990-0 / 41018-3 | ng/dL | nmol/L | 0.03467 | 110-575 | | Spc |
| 141 | SHBG | 13967-5 | nmol/L | nmol/L | = | M 10-57, F 18-144 | binding protein; insulin resistance, liver | Hor |
| 142 | Estradiol (E2) | 2243-4 / 14715-7 | pg/mL | pmol/L | 3.671 | M 10-40 | aromatisation | Hor |
| 143 | DHT (dihydrotestosterone, androstanolone) | 1848-1 | ng/dL | nmol/L | 0.0344 | 30-85 | 5-alpha reductase activity | Spc |
| 144 | LH (luteinizing hormone) | 10501-5 | m[IU]/mL | [IU]/L | = | 1.7-8.6 | pituitary drive | Hor |
| 145 | FSH (follicle stimulating hormone) | 15067-2 (2286-3 moles) | m[IU]/mL | [IU]/L | = | 1.5-12.4 | spermatogenesis | Hor |
| 146 | Prolactin | 2842-3 (15081-3 units) | ng/mL | m[IU]/L | ~21.2 (assay) | 4-15 | pituitary | Hor |
| 147 | PSA, total | 2857-1 (immunoassay 83112-3) | ng/mL | ug/L | = | <4.0 (age-specific) | prostate | Hor, Lng men |
| 148 | PSA, free | 10886-0 | ng/mL | ug/L | = | | | Spc |
| 149 | PSA free/total % | 72576-2 | % | | | >25 % lower risk | prostate cancer risk stratification | Spc |
| 150 | Androstenedione | 14603-5 (moles); LC/MS/MS 107493-9 | ng/dL | nmol/L | 0.0349 | 40-150 | adrenal/gonadal androgen | Spc |

### 3.13 Female hormones and fertility

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 151 | Estradiol (E2) | 2243-4 / 14715-7 | pg/mL | pmol/L | 3.671 | cycle-phase dependent (follicular 20-150, mid-cycle up to 400, postmenopausal <30) | ovarian function | Hor |
| 152 | Progesterone | 2839-9 / 14890-8 | ng/mL | nmol/L | 3.180 | luteal >3 confirms ovulation; follicular <1 | ovulation, luteal phase | Hor |
| 153 | FSH | 15067-2 | m[IU]/mL | [IU]/L | = | phase-dependent; postmenopausal >25 | ovarian reserve, menopause | Hor |
| 154 | LH | 10501-5 | m[IU]/mL | [IU]/L | = | phase-dependent; LH:FSH >2 suggests PCOS | | Hor |
| 155 | AMH (anti-Mullerian hormone) | 38476-8 / 48377-6 (immunoassay 83104-0 / 83105-7) | ng/mL | pmol/L | 7.14 | age-dependent, ~1-4 ng/mL at 30 | ovarian reserve | Hor, Function, Thriva, Medichecks |
| 156 | Prolactin | 2842-3 | ng/mL | m[IU]/L | ~21.2 | 5-25 non-pregnant | | Hor |
| 157 | Testosterone, total (female) | 2986-8 | ng/dL | nmol/L | 0.03467 | 8-60 | PCOS, hyperandrogenism | Hor |
| 158 | SHBG (female) | 13967-5 | nmol/L | nmol/L | = | 18-144 | free androgen index input | Hor |
| 159 | Free androgen index | (r) not looked up | % | | | <5 | 100 x T(nmol/L) / SHBG | UK services (derived) |
| 160 | Estrone (E1) | 2258-2 | pg/mL | pmol/L | 3.699 | | postmenopausal estrogen | Spc |
| 161 | 17-OH progesterone | 14569-8 (moles) | ng/dL | nmol/L | 0.0303 | <200 follicular | CAH | Spc |
| 162 | hCG, beta subunit | 21198-7 | m[IU]/mL | [IU]/L | = | <5 non-pregnant | pregnancy | Clin |

### 3.14 Adrenal, pituitary, growth, bone

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 163 | Cortisol, serum | 2143-6 / 14675-3; AM 9813-7 | ug/dL | nmol/L | 27.59 | AM 6-23 | stress axis, Addison's, Cushing's | Lng, Hor |
| 164 | Cortisol, saliva | 2142-8 / 51844-9 | ug/dL | nmol/L | 27.59 | late-night <0.09 | diurnal curve | Spc (DUTCH, Everlywell) |
| 165 | Cortisol, 24 h urine | 14158-0 / 32310-5 | ug/(24.h) | nmol/(24.h) | 2.759 | 4-50 | | Spc |
| 166 | DHEA-S | 2191-5 / 14688-6 | ug/dL | umol/L | 0.02714 | age and sex dependent (M 30s 120-520) | adrenal androgen, ageing | Lng, Hor |
| 167 | ACTH | 2141-0 | pg/mL | pmol/L | 0.220 | 7-63 (AM) | pituitary-adrenal | Clin |
| 168 | IGF-1 | 2484-4 / 29775-4 | ng/mL | nmol/L | 0.131 | age-dependent (30s ~100-300) | growth hormone axis, longevity | Lng |
| 169 | IGFBP-3 | 2483-6 | ug/mL | mg/L | = | 3.4-7.8 | | Spc |
| 170 | Growth hormone (somatotropin) | 2963-7 | ng/mL | ug/L | = | pulsatile | | Clin |
| 171 | Aldosterone | 1763-2 | ng/dL | pmol/L | 27.74 | 3-16 upright | hyperaldosteronism | Clin |
| 172 | Renin activity | 2915-7 | ng/mL/h | | | 0.5-4.0 | | Clin |
| 173 | Pregnenolone | 2837-3 / 25507-5 | ng/dL | nmol/L | 0.0316 | 10-230 | steroid precursor | Spc |
| 174 | PTH, intact | 2731-8 / 14866-8 | pg/mL | pmol/L | 0.106 | 15-65 | calcium regulation | Lng, Clin |

### 3.15 Inflammation and immunity

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 175 | hs-CRP | 30522-7 | mg/L | mg/L (nmol/L x9.52) | = | <1 low CV risk, 1-3 average, >3 high | low-grade inflammation, CV risk | All |
| 176 | CRP (standard) | 1988-5 | mg/L or mg/dL | mg/L | mg/dL x10 | <10 mg/L | acute inflammation; PhenoAge input (ln mg/dL, recall) | Most |
| 177 | ESR (sedimentation rate) | 4537-7 (Westergren) / 30341-2 | mm/h | mm/h | = | M <15, F <20 (age-adjusted) | inflammation | Lng |
| 178 | Fibrinogen | 3255-7 | mg/dL | g/L | x0.01 | 200-400 | clotting, inflammation | Lng, Spc |
| 179 | Interleukin-6 | 26881-3 | pg/mL | ng/L | = | <5 | inflammation, inflammageing | Spc |
| 180 | ANA (antinuclear antibody) screen | 8061-4 | Pos/Neg + titre | | | negative or <1:80 | autoimmune disease | Function, Superpower |
| 181 | Rheumatoid factor (IgM) | 11573-3 | [IU]/mL | kU/L | = | <14 | rheumatoid arthritis | Function, Superpower |
| 182 | Anti-CCP antibodies | 53027-9 | U/mL | | | <20 | rheumatoid arthritis (specific) | Spc |
| 183 | Immunoglobulin A, total | 2458-8 | mg/dL | g/L | x0.01 | 70-400 | IgA deficiency (needed for coeliac interpretation) | Spc |
| 184 | Immunoglobulin G | 2465-3 | mg/dL | g/L | x0.01 | 700-1600 | | Spc |
| 185 | Calprotectin, stool | 38445-3 (mass/mass) | ug/g | | | <50 | intestinal inflammation (IBD) | Spc |

### 3.16 Kidney (beyond the CMP; renal panel `24362-6` [nlm])

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 186 | Cystatin C | 33863-2 | mg/L | mg/L | = | 0.6-1.0 | filtration independent of muscle mass | Lng, Function, Superpower |
| 187 | eGFR (cystatin C) | 50210-4 | mL/min/{1.73_m2} | | = | >=90 | | Lng |
| 188 | Urine albumin/creatinine ratio (uACR) | 9318-7 (14959-1 microalbumin; 14585-4 molar) | mg/g | mg/mmol | 0.113 | <30 mg/g | early kidney damage, CV risk | Lng, Function |
| 189 | Urine albumin (microalbumin) | 14957-5 | mg/L | mg/L | = | <30 | | Lng |
| 190 | Urine creatinine | 2161-8 | mg/dL | mmol/L | 0.0884 | 20-320 | normaliser | Lng |
| 191 | Uric acid | 3084-1 / 14933-6 | mg/dL | umol/L | 59.48 | M 3.4-7.0, F 2.4-6.0 | gout, metabolic syndrome, fructose | All |

### 3.17 Cardiac and coagulation

| # | Marker | LOINC | US unit | SI unit | Factor | Typical range | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 192 | Troponin I, high sensitivity | 89579-7 (panel 89577-1) | ng/L | ng/L | = | <99th percentile (assay, ~<20) | myocardial injury | Clin, some Lng |
| 193 | Troponin T, high sensitivity | 67151-1 | ng/L | ng/L | = | <14 | myocardial injury | Clin |
| 194 | Troponin I (contemporary) | 10839-9 | ng/mL | ug/L | = | <0.04 | | Clin |
| 195 | NT-proBNP | 33762-6 | pg/mL | pmol/L | 0.118 | <125 (<75 yr) | heart failure, cardiac strain | Lng (Superpower, Function) |
| 196 | BNP | 30934-4 | pg/mL | pmol/L | 0.289 | <100 | heart failure | Clin |
| 197 | CK-MB | 13969-1 | ng/mL | ug/L | = | <5 | myocardial injury (legacy) | Clin |
| 198 | D-dimer | 48065-7 (FEU) | ug/mL FEU | mg/L FEU | = | <0.5 | thrombosis | Clin |
| 199 | Prothrombin time (PT) | 5902-2 / 5964-2 | s | s | = | 11-13.5 | coagulation, liver, warfarin | Clin |
| 200 | INR | 6301-6 / 34714-6 | {INR} | | = | 0.8-1.1 (2-3 on warfarin) | | Clin, home meters |
| 201 | aPTT | 14979-9 | s | s | = | 25-35 | intrinsic pathway | Clin |

### 3.18 Urinalysis (panels `24356-8` complete, `24357-6` dipstick [nlm])

| # | Marker | LOINC | Scale and unit | Typical | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- |
| 202 | Color | 5778-6 | Nom (yellow, amber, red...) | yellow | hydration, blood, bilirubin | Function, Superpower, Clin |
| 203 | Appearance (clarity) | 5767-9 | Nom (clear, cloudy) | clear | | |
| 204 | Specific gravity | 5811-5 (strip), 2965-2 | Qn {ratio}, 1.005-1.030 | 1.005-1.030 | concentration | |
| 205 | pH | 5803-2 | Qn [pH] | 5.0-8.0 | acid-base, stones | |
| 206 | Protein | 20454-5 (presence) / 5804-0 (mg/dL) | Ord neg/trace/1+..4+ or mg/dL | negative | kidney | |
| 207 | Glucose | 5792-7 (mg/dL), 2349-9 (presence) | Ord or mg/dL | negative | diabetes, SGLT2 | |
| 208 | Ketones | 5797-6 (mg/dL), 2514-8 (presence) | Ord | negative | ketosis | |
| 209 | Bilirubin | 5770-3 | Ord | negative | liver | |
| 210 | Urobilinogen | 20405-7 | mg/dL (0.2-1.0) or Ord | normal | liver, haemolysis | |
| 211 | Blood (haemoglobin) | 5794-3 | Ord | negative | bleeding, stones | |
| 212 | Nitrite | 32710-6 (presence) | Ord Pos/Neg | negative | bacterial infection | |
| 213 | Leukocyte esterase | 5799-2 | Ord | negative | infection | |
| 214 | WBC per HPF (microscopy) | 5821-4 | #/[HPF] | 0-5 | infection | |
| 215 | RBC per HPF | 13945-1 | #/[HPF] | 0-2 | bleeding | |
| 216 | Casts per LPF | 9842-6 | #/[LPF] | none to few hyaline | kidney disease | |
| 217 | Sodium, urine | 2955-3 | mmol/L | variable | salt intake | Spc |
| 218 | Potassium, urine | 2828-2 | mmol/L | variable | | Spc |

### 3.19 Coeliac, allergy and food sensitivity

| # | Marker | LOINC | Unit | Typical | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- |
| 219 | tTG-IgA (tissue transglutaminase IgA) | 31017-7 | U/mL | <4 (assay) negative | coeliac disease (requires normal total IgA) | Function, Superpower, Everlywell, Medichecks |
| 220 | Total IgA | 2458-8 | mg/dL | 70-400 | rule out IgA deficiency | same |
| 221 | Deamidated gliadin peptide IgA/IgG panel | 57776-7 (panel); IgA [Measurement] 109392-1 | U/mL | negative | coeliac (useful when IgA deficient) | Spc |
| 222 | Endomysial IgA antibody | 10362-2 (presence), 10863-9 (titre) | Pos/Neg, titre | negative | coeliac confirmation | Spc |
| 223 | HLA-DQ2/DQ8 genotype | (r) not looked up | Nom | | coeliac susceptibility (rules out if absent) | Spc |
| 224 | IgE, total | 19113-0 | k[IU]/L | <100 | atopy | Function, Everlywell, Thriva |
| 225 | Specific IgE, peanut (f13) | 6206-7 (component Ara h 1, 3, 6: 58779-0, 58777-4, 90880-6) | kU/L (kUA/L) | <0.35 class 0 | peanut sensitisation | Allergy panels (Everlywell, Quest, Labcorp) |
| 226 | Specific IgE, dust mite (D. pteronyssinus, d1) | (r) 6095-4; component rDer p 1 88708-3 | kU/L | <0.35 | | Allergy panels |
| 227 | Specific IgE, cat dander (e1) | 6833-8 | kU/L | <0.35 | | Allergy panels |
| 228 | Specific IgE, timothy grass (g6) | 6265-3 | kU/L | <0.35 | hay fever | Allergy panels |
| 229 | Specific IgE, egg white (f1) | 6106-9 | kU/L | <0.35 | | Allergy panels |
| 230 | Specific IgE, cow's milk (f2) | 7258-7 | kU/L | <0.35 | | Allergy panels |
| 231 | Food IgG, e.g. peanut IgG | 6732-2 (units), 35542-0 (mass); IgG4 56351-0 | U/mL or ug/mL | none valid | "Food sensitivity" (Everlywell, others). Allergy societies (AAAAI, EAACI) state IgG/IgG4 testing does not diagnose food intolerance (recall) | Everlywell, YorkTest, consumer |

Specific IgE results are also reported as RAST/ImmunoCAP classes 0-6 (ordinal), a second
representation of the same quantitative value; a catalogue should store kU/L and derive the class.

### 3.20 Fatty acids and nutrition status

| # | Marker | LOINC | Unit | Typical / target | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- | --- |
| 232 | Omega-3 index (EPA+DHA % of RBC fatty acids) | 88998-0 (NLM lists system as Serum or Plasma; OmegaQuant measures RBC; note the mismatch); risk category 88999-8 | % | >8 % desirable, <4 % high risk | CV risk, omega-3 intake | Function, Superpower, InsideTracker, OmegaQuant |
| 233 | EPA/AA ratio (AA/EPA) | 88996-4 (EPA/AA molar ratio, Ser/Plas); 90909-3 (AA/EPA mass ratio, blood) | {ratio} | AA/EPA <3 to 11 depending on lab | inflammatory balance | Spc |
| 234 | Omega-6:omega-3 ratio | (r) not looked up | {ratio} | <4 | | Spc |
| 235 | EPA in RBC | 75097-6 | % or umol/L | | | Spc |

### 3.21 Infectious disease and blood group (commonly in consumer "full body" panels)

| # | Marker | LOINC | Scale | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- |
| 236 | HIV-1/2 Ag/Ab combination | 105080-6 (presence); panel 83101-6 | Ord | HIV | Everlywell, Superpower add-on, Clin |
| 237 | Hepatitis B surface antigen | 5195-3 | Ord | HBV infection | Clin, add-ons |
| 238 | Hepatitis C antibody | 16128-1 | Ord | HCV exposure | Clin, add-ons |
| 239 | Treponema pallidum antibody (syphilis) | (r) 17724-6 by IFA; panel 34954-8 | Ord | syphilis | Everlywell STI kits |
| 240 | ABO group | 883-9 (ABO+Rh 882-1) | Nom | blood type | Function, some |
| 241 | Rh type | 10331-7 | Nom | | Function, some |

### 3.22 Genetic markers

| # | Marker | LOINC | Value shape | Indicates | Offered by |
| --- | --- | --- | --- | --- | --- |
| 242 | APOE genotype (e2/e3/e4) | 21619-2 "APOE gene mutations found [Identifier] ... Nominal" | Nom: e2/e3, e3/e3, e3/e4, e4/e4... | Alzheimer's and lipid risk | Superpower, Function add-on, 23andMe (rsid rs429358 + rs7412) |
| 243 | MTHFR C677T | 28005-7 [Genotype] | Nom: CC, CT, TT | folate metabolism; weak clinical value | Spc, 23andMe |
| 244 | MTHFR C677T + A1298C | 94414-0 | Nom | | Spc |
| 245 | Generic "gene X mutations found" | 41103-3 (template) | Nom | placeholder term labs use with a gene in the component | |
| 246 | HFE (C282Y, H63D) | (r) not looked up | Nom | haemochromatosis | Spc |

Genetic results do not fit "a value with a unit". FHIR uses the Genomics Reporting IG
(Observation with components for gene, variant HGVS, genotype, zygosity; recall). Consumer
raw-data files (23andMe, AncestryDNA) are tab-separated `rsid chromosome position genotype`
with ~600k rows; a different storage problem (one blob, derived genotype records for the few
variants a user cares about).

### 3.23 Ageing composites, epigenetic and microbiome

| # | Marker | Standard code | Shape | Notes | Offered by |
| --- | --- | --- | --- | --- | --- |
| 247 | Phenotypic age (Levine 2018 PhenoAge) | none in LOINC (search found none) | years | computed from albumin, creatinine, glucose, CRP, lymphocyte %, MCV, RDW, ALP, WBC and age (recall) | Carrot Care, InsideTracker InnerAge (own formula), Function "biological age" (2026), Apple "Health Age" (2026, inputs unverified) |
| 248 | Epigenetic age (DNA methylation clocks: Horvath, Hannum, GrimAge, PhenoAge-DNAm, DunedinPACE pace of ageing) | none | years; pace is a rate (years per year) | from saliva or blood spot methylation arrays; clock version is part of the meaning | TruDiagnostic (TruAge), Tally Health, Elysium Index, Function add-on (recall) |
| 249 | Glycan age | none | years | IgG glycosylation | GlycanAge |
| 250 | Telomere length | 35463-9 is "Telomere analysis [Identifier]" only | kb or T/S ratio | method-dependent (qPCR vs flow-FISH) | TeloYears (discontinued, recall), Spc |
| 251 | Gut microbiome profile | none for composition; zonulin stool 99399-8, calprotectin stool 38445-3 | relative abundance per taxon (hundreds of rows), diversity indices | 16S vs shotgun metagenomics; taxonomy versions | Viome, Thorne, Ombre, Zoe |
| 252 | NAD+ (intracellular) | (r) not looked up | uM | | Jinfiniti, Spc |

### 3.24 Count and coverage

The tables above list 252 rows. Allowing for the duplicates that appear under two panels on
purpose (estradiol, FSH, LH, prolactin, testosterone and SHBG in both sex tables; total IgA in
3.15 and 3.19), there are about 240 distinct markers, with about 230 LOINC codes checked against
the NLM API today.

Coverage check against the requested list: ApoB (62), Lp(a) (65, 66), hs-CRP (175),
homocysteine (116), IGF-1 (168), cortisol (163-165), DHEA-S (166), testosterone free and total
(138-140), SHBG (141), estradiol (142, 151), progesterone (152), FSH (145, 153), LH (144, 154),
prolactin (146, 156), AMH (155), vitamin D 25-OH (109), B12 (111), folate (114-115), ferritin
(105), omega-3 index (232), HbA1c (79), fasting insulin (81), HOMA-IR (82), uric acid (191), GGT
(44), ALT/AST (41-42), albumin (36), eGFR (27, 187), cystatin C (186), RBC magnesium (125), zinc
(127), copper (128), selenium (130), heavy metals (132-136), coeliac (219-223), allergy IgE
(224-230), food sensitivity (231), microbiome (251), APOE and MTHFR (242-244), epigenetic age
(248).

---

## Part 4: what this means for a substrate design (input to the proposal, not a proposal)

1. **Three layers.** Every source in Part 1 ends up as one of: a FHIR bundle (Apple, Epic, 1up,
   Health Gorilla, Metriport, Particle, CMS payers), a proprietary JSON (Thriva, SiPhox,
   unofficial Function), or a PDF. Mirrors keep those shapes. A projected canonical "lab result"
   kind is where LOINC, UCUM and the comparator live.
2. **The value type needs more than a number and a unit.** At minimum: decimal value with
   preserved precision, comparator (`<`, `<=`, `>=`, `>`), UCUM code plus the source's unit
   string, or a coded value (system, code, display), or a ratio (titre), or text, or an absent
   reason. This is the money precedent again: a structured value type with its own validation
   and filter semantics. "Filter value < 5" must say what it does with a stored `<5`.
3. **Identity of the analyte is a LOINC code plus a grouping.** LOINC splits by property,
   specimen and method; trend views need a "same analyte" grouping above LOINC (glucose serum mass,
   glucose serum molar, fasting glucose, whole blood glucose). That grouping is data (a catalogue
   package), not code.
4. **Conversion is derivation, not re-expression.** UCUM-only conversions (g/dL to g/L, 10*3/uL to
   10*9/L) are safe and exact up to decimal scaling. Mass to molar needs the analyte table above
   and changes the LOINC code; IU-based and arbitrary units, Lp(a) and HbA1c must refuse or use a
   declared affine formula. Converted values should record the factor and source, so a rebuild is
   deterministic.
5. **Ranges and flags come from the source.** Store the lab's referenceRange list and
   interpretation as received; a catalogue range is a labelled fallback; a derived flag is a
   separate property.
6. **Panels are reports.** A report record (DiagnosticReport analogue: accession, lab, issued,
   status, the PDF as a blobref) referenced by each result record fits "a reference is the only
   link".
7. **Corrections are status moves on the same record.** The changelog keeps the old value.
8. **Specimen and fasting are context, not value.** Specimen type, collection time (the `at`),
   fasting state, method and device belong on the result or the report.
9. **Volume is small for labs, large for wearables.** A heavy user has perhaps a few thousand lab
   values in a lifetime; the scale problem is wearables (other researchers' slices). Labs need
   rich semantics, not throughput.

---

## Sources (primary first)

- FHIR R4 Observation: https://hl7.org/fhir/R4/observation.html
- FHIR R4 DiagnosticReport: https://hl7.org/fhir/R4/diagnosticreport.html
- FHIR R4 data types (Quantity, Range, Ratio, SampledData): https://hl7.org/fhir/R4/datatypes.html
- FHIR ObservationInterpretation value set: https://hl7.org/fhir/R4/valueset-observation-interpretation.html
- US Core Laboratory Result Observation: https://hl7.org/fhir/us/core/STU6.1/StructureDefinition-us-core-observation-lab.html
- UCUM specification: https://ucum.org/ucum
- ucum-lhc: https://github.com/LHNCBC/ucum-lhc and CHANGELOG
- NLM Clinical Tables LOINC API: https://clinicaltables.nlm.nih.gov/api/loinc_items/v3/search
- LOINC Top 2000+ (US and SI): https://loinc.org/news/new-si-version-of-the-loinc-top-2000-lab-observations, https://lhncbc.nlm.nih.gov/CHRB/Projects/top-LOINC-codes.html
- OMOP CDM v5.4: https://ohdsi.github.io/CommonDataModel/cdm54.html
- HL7 v2 OBX and ORU: https://saga-it.com/docs/hl7/segments/obx, https://saga-it.com/docs/hl7/messages/oru, https://hapifhir.github.io/hapi-hl7v2/v251/apidocs/ca/uhn/hl7v2/model/v251/segment/OBX.html
- openEHR lab archetypes: https://openehr.atlassian.net/wiki/spaces/healthmod/pages/91139266/Implementing+Laboratory+Tests+in+openEHR
- Open mHealth: https://github.com/openmhealth/schemas/blob/main/schema/omh/unit-value-1.0.json, https://www.openmhealth.org/schemas/omh_blood-glucose/
- IEEE 11073 MDC in HL7 terminology: https://terminology.hl7.org/6.4.0/CodeSystem-v3-mdc.html; IHE RTM: https://wiki.ihe.net/index.php/PCD_Rosetta_Terminology_Mapping_Overview
- CMS-0057-F: https://www.cms.gov/files/document/cms-0057-f.pdf; Blue Button: https://bluebutton.cms.gov/data/understanding-the-data/
- Epic: https://open.epic.com/interface/FHIR, https://fhir.epic.com/
- NHS GP Connect: https://digital.nhs.uk/developer/api-catalogue/gp-connect-access-record-structured-fhir, https://developer.nhs.uk/apis/gpconnect-1-6-0/accessrecord_structured.html
- Apple Health Records: https://developer.apple.com/videos/play/wwdc2020/10669/, https://github.com/StanfordBDHG/HealthKitOnFHIR
- Quest: https://www.questdiagnostics.com/content/dam/corporate/restricted/documents/qps_qecs/Quanum_EHR_FHIR_API_Nov22.pdf; Apple panel: https://telehealth.org/news/quest-diagnostics-will-sell-a-119-50-biomarker-lab-panel-inside-apples-health-app/, https://www.macobserver.com/tips/round-ups/apple-health-labs-119-quest-diagnostics-50-biomarkers/
- Labcorp: https://github.com/api-evangelist/labcorp, https://ir.labcorp.com/news-releases/news-release-details/labcorp-enables-health-records-iphone
- Function Health: https://help.empirical.health/article/18-how-to-export-function-health-results, https://github.com/bogini/function-health-exporter, https://github.com/daveremy/function-health-mcp
- InsideTracker: https://hitconsultant.net/2026/08/17/insidetracker-launches-universal-blood-data-connector-terra-platform/
- Everlywell 360: https://support.everlywell.com/article/1870-everlywell-360-membership
- SiPhox: https://siphoxhealth.com/partner/platform-features
- Superpower: https://superpower.com/
- Levels: https://www.levels.com/blog/new-levels-core-complete
- Thriva: https://docs.thriva.io/docs, https://docs.thriva.io/llms.txt
- Medichecks: https://support.medichecks.com/hc/en-gb/articles/31142619957021-Can-I-print-my-results, https://support.medichecks.com/hc/en-gb/articles/27347149508765-Can-you-set-up-API-integration
- Randox: https://randoxhealth.com/en-GB/product/clinic/everyman-everywoman-test
- Carrot Care: https://apps.apple.com/us/app/carrot-care-bloodwork-tracker/id1518603405
- Ulta Lab Tests: https://www.ultalabtests.com/view-track-results
- 1upHealth: https://docs.1up.health/help-center/Content/en-US/get-started/cms-patient-access/developer-overview.html
- Health Gorilla: https://healthgorilla.com/home/policies/product-documentation, https://www.medplum.com/docs/integration/health-gorilla/receiving-results
- Metriport: https://github.com/api-evangelist/metriport
- Particle Health: https://docs.particlehealth.com/docs/data-retrieval-apis
- Fasten: https://github.com/fastenhealth/fasten-onprem
