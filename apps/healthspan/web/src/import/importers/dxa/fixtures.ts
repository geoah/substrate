// Invented GE Lunar printouts for the dxa importer's tests: PDFs whose text
// sits where the scanner's printouts put theirs, in Greek, with invented
// values. Only tests import it.
//
// The PDFs use the standard Helvetica with every character this file shows
// outside ASCII given a byte of its own and named `uniXXXX` in the font's
// encoding (pdf.js reads those names as the characters), and every glyph
// half an em wide, so an item's width is known.

/** A run of text placed at `x`, its baseline at `y`. */
export interface Run {
  text: string
  x: number
  y: number
  size?: number
}

const EM = 0.5

/** How wide a run is in this fixture's font. */
export function widthOf(text: string, size = 9): number {
  return [...text].length * EM * size
}

/** A run centred on `cx`. */
export function centred(text: string, cx: number, y: number, size = 9): Run {
  return { text, x: cx - widthOf(text, size) / 2, y, size }
}

/** A run ending at `right`. */
export function rightAt(text: string, right: number, y: number, size = 9): Run {
  return { text, x: right - widthOf(text, size), y, size }
}

/** A PDF of the pages' runs; `title`, where given, is the document's
 * title (the enCORE report template a printout names). */
export function pdfOf(pages: readonly Run[][], title?: string): Uint8Array {
  const codes = new Map<string, number>()
  for (const page of pages)
    for (const run of page)
      for (const c of run.text) {
        const cp = c.codePointAt(0)!
        if ((cp < 32 || cp > 126) && !codes.has(c))
          codes.set(c, 128 + codes.size)
      }
  if (codes.size > 128) throw new Error("too many characters for one font")
  const names = [...codes.keys()]
    .map(
      (c) =>
        `/uni${c.codePointAt(0)!.toString(16).toUpperCase().padStart(4, "0")}`
    )
    .join(" ")
  const literal = (text: string) => {
    let s = ""
    for (const c of text) {
      const code = codes.get(c)
      if (code !== undefined) s += `\\${code.toString(8).padStart(3, "0")}`
      else s += c === "(" || c === ")" || c === "\\" ? `\\${c}` : c
    }
    return `(${s})`
  }
  const objects: string[] = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    `<< /Type /Pages /Kids [${pages.map((_, i) => `${5 + i * 2} 0 R`).join(" ")}] /Count ${pages.length} >>`,
    `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 32 /LastChar 255 /Widths [${Array<number>(
      224
    )
      .fill(EM * 1000)
      .join(
        " "
      )}] /Encoding << /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [128 ${names}] >> >>`,
    `<< /Producer (fixtures)${title ? ` /Title (${title})` : ""} >>`,
  ]
  pages.forEach((runs, i) => {
    const content = runs
      .map(
        (r) =>
          `BT /F1 ${r.size ?? 9} Tf 1 0 0 1 ${r.x.toFixed(2)} ${r.y.toFixed(2)} Tm ${literal(r.text)} Tj ET`
      )
      .join("\n")
    objects.push(
      `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents ${6 + i * 2} 0 R >>`
    )
    objects.push(
      `<< /Length ${content.length} >>\nstream\n${content}\nendstream`
    )
  })
  let out = "%PDF-1.4\n"
  const offsets: number[] = []
  objects.forEach((o, i) => {
    offsets.push(out.length)
    out += `${i + 1} 0 obj\n${o}\nendobj\n`
  })
  const xref = out.length
  out += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`
  out += offsets
    .map((o) => `${String(o).padStart(10, "0")} 00000 n \n`)
    .join("")
  out += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R /Info 4 0 R >>\nstartxref\n${xref}\n%%EOF\n`
  return Uint8Array.from(out, (c) => c.charCodeAt(0))
}

/** The enCORE report templates' titles. */
export const TITLES = {
  bone: "Crystal Reports ActiveX Designer - CorePatient.rpt",
  composition: "Crystal Reports ActiveX Designer - CoreTbComp.rpt",
  trend: "Crystal Reports ActiveX Designer - CompEnhanced.rpt",
} as const

/** The header every page of a scan's printouts carries. */
export interface Header {
  /** `D/M/YYYY`. */
  date: string
  /** `H:MM:SS πμ`. */
  time: string
  /** `D/M/YYYY H:MM:SS μμ`. */
  analyzed: string
  /** The scanner's file name; left off where undefined. */
  file?: string
}

/** A page's header: the centre's letterhead, the patient's details and the
 * scan's measured and analysed times, all invented. */
export function header(h: Header): Run[] {
  const [aDate, aTime, aHalf] = h.analyzed.split(" ")
  const runs: Run[] = [
    { text: "INVENTED IMAGING CENTRE", x: 160, y: 785, size: 18 },
    { text: "Ασθενής:", x: 29, y: 732 },
    { text: "TESTER, ALEX", x: 135, y: 732 },
    { text: "Ύψος / Βάρος:", x: 29, y: 708 },
    { text: "170,0 εκ. 70,0 kg", x: 135, y: 708 },
    { text: "Μετρηθείσα:", x: 297.6, y: 708 },
    { text: h.date, x: 405.6, y: 708 },
    { text: h.time, x: 464, y: 708 },
    { text: "(10,50)", x: 522.6, y: 708 },
    { text: "Analyzed:", x: 297.6, y: 696 },
    { text: aDate, x: 405.6, y: 696 },
    { text: `${aTime} ${aHalf}`, x: 464, y: 696 },
    { text: "(10,50)", x: 522.6, y: 696 },
    { text: "Lunar Prodigy Advance", x: 25, y: 120, size: 6.72 },
  ]
  if (h.file)
    runs.push({ text: `Όνομα αρχείου: ${h.file}`, x: 25, y: 153, size: 6.72 })
  return runs
}

/** A bone density table row: label, BMD, and where printed the young
 * adult %, T-score, age matched % and Z-score. */
export type BmdRow = [string, string, string?, string?, string?, string?]

/** The bone density table as printout 1 prints it: region, BMD (g/cm²),
 * young adult (%), T-score, age matched (%), Z-score, `-` where a region
 * has none; its rows from `top` down. */
export function bmdTable(rows: readonly BmdRow[], top = 470): Run[] {
  const s = 8.28
  const cols = [379, 430, 465, 505, 541]
  const runs: Run[] = [
    centred("BMD", cols[0], top + 25, s),
    centred("Νεαρός ενήλικας", 449, top + 25, s),
    centred("Προσαρμογή", 524, top + 25, s),
    centred("(g/cm²)", cols[0], top + 15.8, 6.72),
    centred("(%)", cols[1], top + 15.8, 6.72),
    centred("T-Score", cols[2], top + 15.8, 6.72),
    centred("(%)", cols[3], top + 15.8, 6.72),
    centred("Z-Score", cols[4], top + 15.8, 6.72),
    { text: "Περιοχή", x: 287, y: top + 14.1, size: s },
  ]
  rows.forEach((r, i) => {
    const y = top - i * 11
    runs.push({ text: r[0], x: 288, y, size: s })
    for (let c = 0; c < 5; c++)
      runs.push(centred(r[c + 1] ?? "-", cols[c], y, s))
  })
  return runs
}

/** The supplementary results table of a re-saved bone density printout's
 * second page: region, BMD, young adult %, T-score, age matched %,
 * Z-score, BMC (g) and area (cm²). */
export function supplementaryTable(
  rows: readonly [string, string, string, string][]
): Run[] {
  const s = 9
  const cols = [205, 260, 300, 345, 390, 450, 520]
  const runs: Run[] = [
    { text: "ΣΥΜΠΛΗΡΩΜΑΤΙΚΑ ΑΠΟΤΕΛΕΣΜΑΤΑ [Ολόσωμη]", x: 25, y: 670, size: 12 },
    centred("BMD", cols[0], 655, s),
    centred("Νεαρός", cols[1], 655, s),
    centred("Προσαρμογή", cols[3], 655, s),
    centred("Οστική μάζα", cols[5], 655, s),
    centred("Επιφάνεια", cols[6], 655, s),
    { text: "Περιοχή", x: 25, y: 644, size: s },
    centred("(g/cm²)", cols[0], 645.4, 6.72),
    centred("(%)", cols[1], 645.4, 6.72),
    centred("T-Score", cols[2], 645.4, 6.72),
    centred("(%)", cols[3], 645.4, 6.72),
    centred("Z-Score", cols[4], 645.4, 6.72),
    centred("(g)", cols[5], 645.4, 6.72),
    centred("(cm²)", cols[6], 645.4, 6.72),
  ]
  rows.forEach(([label, bmd, bmc, area], i) => {
    const y = 626 - i * 12
    runs.push({ text: label, x: 25, y, size: s })
    runs.push(centred(bmd, cols[0], y, s))
    for (let c = 1; c < 5; c++) runs.push(centred("-", cols[c], y, s))
    runs.push(rightAt(bmc, cols[5] + 12, y, s))
    runs.push(rightAt(area, cols[6] + 12, y, s))
  })
  return runs
}

/** The bone density trend table: measured date, age, BMD, its change
 * against the baseline (% and %/yr), each change with a `*`. */
export function bmdTrend(rows: readonly [string, string, string][]): Run[] {
  const s = 8.28
  const runs: Run[] = [
    { text: "Τάση: Σύνολο", x: 398, y: 352, size: 9 },
    { text: "Μετρηθείσα", x: 289, y: 336.4, size: s },
    centred("Ηλικία", 370, 336.4, s),
    centred("BMD", 427, 336.4, s),
    centred("Γραμμή βάση", 485, 336.4, s),
    centred("Γραμμή βάση", 541, 336.4, s),
    centred("(έτη)", 370, 328.4, 6.72),
    centred("(g/cm²)", 427, 328.4, 6.72),
    centred("(%)", 485, 328.4, 6.72),
    centred("(%/yr)", 541, 328.4, 6.72),
    { text: "Ημερομηνία", x: 289, y: 326.8, size: s },
  ]
  rows.forEach(([date, age, bmd], i) => {
    const y = 311.5 - i * 11
    const base = i === rows.length - 1
    runs.push({ text: date, x: 289, y, size: s })
    runs.push(centred(age, 370, y, s))
    runs.push(centred(bmd, 427, y, s))
    runs.push(centred(base ? "γρ. Βάσης" : "-1,0", 482, y, s))
    runs.push(centred(base ? "γρ. Βάσης" : "-2,0", 541, y, s))
    if (!base) {
      runs.push({ text: "*", x: 492.2, y: y + 1.7, size: 6.72 })
      runs.push({ text: "*", x: 552.3, y: y + 1.7, size: 6.72 })
    }
  })
  return runs
}

/** A body composition row: label, tissue (%fat), region (%fat), tissue,
 * fat, lean and BMC in grams, total mass in kg or `-`. */
export type CompositionRow = [
  string,
  string,
  string,
  string,
  string,
  string,
  string,
  string,
]

/** The body composition table (`ΣΥΝΘΕΣΗ ΣΩΜΑΤΟΣ`) and, where given, the
 * fat mass ratios under it. */
export function compositionTable(
  rows: readonly CompositionRow[],
  ratios?: readonly [string, string, string]
): Run[] {
  const s = 9
  const cols = [127, 195, 263, 330, 398, 465, 532]
  const labels = [
    "Ιστός",
    "Περιοχή",
    "Ιστός",
    "Λίπος",
    "Ιστός χωρίς",
    "Οστική μάζα",
    "Συνολική",
  ]
  const units = ["(%Λίπος)", "(%Λίπος)", "(g)", "(g)", "(g)", "(g)", "(κιλά)"]
  const runs: Run[] = [
    { text: "ΣΥΝΘΕΣΗ ΣΩΜΑΤΟΣ", x: 25, y: 670, size: 12.72 },
    ...labels.map((l, i) => centred(l, cols[i], 654.8, s)),
    ...units.map((u, i) => centred(u, cols[i], 645.4, 6.72)),
    { text: "Περιοχή", x: 25, y: 644.3, size: s },
  ]
  rows.forEach((r, i) => {
    const y = 626 - i * 12
    runs.push({ text: r[0], x: 25, y, size: s })
    r.slice(1).forEach((v, c) => runs.push(rightAt(v, cols[c] + 10, y, s)))
  })
  if (ratios) {
    const y = 626 - rows.length * 12 - 30
    const at = [89, 214, 340]
    runs.push({ text: "FAT MASS RATIOS", x: 26, y, size: 12.72 })
    runs.push(centred("Κορμός/", at[0], y - 14.4, s))
    runs.push(centred("Κνήμες/", at[1], y - 14.4, s))
    runs.push(centred("(Βραχίονες+Κνήμες)/", at[2], y - 14.4, s))
    runs.push(centred("Σύνολο", at[0], y - 26.4, s))
    runs.push(centred("Σύνολο", at[1], y - 26.4, s))
    runs.push(centred("Κορμός", at[2], y - 26.4, s))
    ratios.forEach((v, i) => runs.push(centred(v, at[i], y - 40.8, s)))
  }
  return runs
}

/** The tissue quantitation table a body composition printout's first page
 * can carry: legs, trunk and total with their centile, which this importer
 * does not read. */
export function quantitationTable(): Run[] {
  const s = 8.28
  const cols = [120, 160, 200, 240, 280, 320]
  const runs: Run[] = [
    ...["Ιστός", "Εκατοστό", "Συν.", "Παχύς", "Αδύνατος", "BMC"].map((l, i) =>
      centred(l, cols[i], 470, s)
    ),
    { text: "Περιοχή", x: 40, y: 461, size: s },
    ...["(%Λίπος)", "Μάζα", "(g)", "(g)", "(g)"].map((l, i) =>
      centred(l, cols[i === 0 ? 0 : i + 1], 461, 6.72)
    ),
  ]
  ;[
    ["Κνήμες", "34,0", "-", "-", "7.100", "13.500", "750"],
    ["Σύνολο", "31,9", "-", "63,5", "20.100", "42.000", "1.740"],
  ].forEach((r, i) => {
    const y = 449 - i * 11
    runs.push({ text: r[0], x: 40, y, size: s })
    r.slice(1).forEach((v, c) => runs.push(centred(v, cols[c], y, s)))
  })
  return runs
}

/** A composition trend row: measured date, age, tissue (%fat), centile,
 * total mass (kg), region (%fat), tissue, fat, lean, BMC, fat free (g). */
export type TrendRow = [
  string,
  string,
  string,
  string,
  string,
  string,
  string,
  string,
  string,
  string,
  string,
]

/** A fat distribution trend row: measured date, age, android (%fat),
 * gynoid (%fat), A/G ratio, total body (%fat). */
export type DistributionRow = [string, string, string, string, string, string]

/** The trend printout's two tables. */
export function trendTables(
  composition: readonly TrendRow[],
  distribution: readonly DistributionRow[]
): Run[] {
  const s = 8.28
  const cols = [172, 222, 262, 298, 338, 377, 418, 449, 478, 508, 546]
  const labels = [
    "Ημερομηνία",
    "Ηλικία",
    "Ιστός",
    "Εκατοστό",
    "Συν. Μάζα",
    "Περιοχή",
    "Ιστός",
    "Παχύς",
    "Αδύνατο",
    "BMC",
    "Fat Free",
  ]
  const runs: Run[] = [
    { text: "Τάση: Σύνολο", x: 156, y: 539.3, size: s },
    ...labels.map((l, i) =>
      i === 0
        ? { text: l, x: 156, y: 523.3, size: 6.72 }
        : centred(l, cols[i], 523.3, 6.72)
    ),
    { text: "Μέτρησης", x: 156, y: 514.4, size: 6.72 },
    centred("(έτη)", cols[1], 514.4, 6.72),
    centred("(%Λίπος)", cols[2], 514.4, 6.72),
    centred("(κιλά)", cols[4], 514.4, 6.72),
    centred("(%Λίπος)", cols[5], 514.4, 6.72),
    centred("(g)", cols[6], 514.4, 6.72),
    centred("(g)", cols[7], 514.4, 6.72),
    { text: "ς", x: 463.2, y: 514.4, size: 6.72 },
    centred("(g)", cols[9], 514.4, 6.72),
    centred("(g)", cols[10], 514.4, 6.72),
    centred("(g)", cols[8], 505.6, 6.72),
  ]
  composition.forEach((r, i) => {
    const y = 490 - i * 11
    runs.push({ text: r[0], x: 156, y, size: s })
    r.slice(1).forEach((v, c) => runs.push(centred(v, cols[c + 1], y, s)))
  })
  const top = 490 - composition.length * 11 - 30
  const dcols = [172, 243, 304, 373, 447, 528]
  const dlabels = [
    "Ημερομηνία",
    "Ηλικία",
    "Android",
    "Gynoid",
    "A/G",
    "Total Body",
  ]
  runs.push({ text: "Τάση: Fat Distribution", x: 156, y: top + 12, size: s })
  dlabels.forEach((l, i) =>
    runs.push(
      i === 0
        ? { text: l, x: 156, y: top, size: 6.72 }
        : centred(l, dcols[i], top, 6.72)
    )
  )
  ;["(έτη)", "(%Fat)", "(%Fat)", "Ratio", "(%Fat)"].forEach((l, i) =>
    runs.push(centred(l, dcols[i + 1], top - 8.9, 6.72))
  )
  runs.push({ text: "Μέτρησης", x: 156, y: top - 8.9, size: 6.72 })
  distribution.forEach((r, i) => {
    const y = top - 24 - i * 10.2
    runs.push({ text: r[0], x: 156, y, size: s })
    r.slice(1).forEach((v, c) => runs.push(centred(v, dcols[c + 1], y, s)))
  })
  return runs
}

// ---- invented scans ----

/** Grams as the printout prints them, `.` between thousands. */
const g = (n: number) => String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ".")

/** A composition table row from its fat and lean grams; the tissue is
 * their sum. */
function comp(
  label: string,
  tissueFat: string,
  regionFat: string,
  fat: number,
  lean: number,
  bmc: number,
  mass = "-"
): CompositionRow {
  return [
    label,
    tissueFat,
    regionFat,
    g(fat + lean),
    g(fat),
    g(lean),
    g(bmc),
    mass,
  ]
}

export const COMPOSITION: CompositionRow[] = [
  comp("Αρ. Βραχίονας", "31,5", "30,2", 1050, 2290, 140),
  comp("Αρ. Σκέλος", "35,0", "33,9", 3600, 6690, 380),
  comp("Αρ. Κορμός", "28,4", "27,9", 4100, 10330, 230),
  comp("Σύν. Αρ. μελών", "31,0", "30,1", 9400, 20900, 880),
  comp("Δεξ. Βραχίονας", "31,0", "29,8", 1010, 2250, 135),
  comp("Δεξ. Σκέλος", "34,6", "33,5", 3550, 6710, 375),
  comp("Δεξ. Κορμός", "28,0", "27,5", 4000, 10290, 228),
  comp("Σύν. Δεξ. μελών", "30,6", "29,7", 9200, 20850, 870),
  comp("Βραχίονες", "31,3", "30,0", 2060, 4540, 275),
  comp("Κνήμες", "34,8", "33,7", 7150, 13400, 755),
  comp("Κορμός", "28,2", "27,7", 8100, 20620, 458),
  comp("Android", "33,9", "33,4", 1330, 2600, 31),
  comp("Gynoid", "38,8", "37,6", 3410, 5380, 180),
  comp("Σύνολο", "30,8", "29,9", 18600, 41750, 1750, "62,1"),
]

export const C: Header = {
  date: "15/7/2024",
  time: "9:05:12 πμ",
  analyzed: "15/7/2024 9:12:40 πμ",
  file: "c7k2m9q1zz.dfb",
}

export const C_BMD: BmdRow[] = [
  ["Κεφαλή", "1,880"],
  ["Βραχίονες", "0,701"],
  ["Κνήμες", "1,105"],
  ["Κορμός", "0,790"],
  ["Πλευρές", "0,602"],
  ["Πύελος", "0,960"],
  ["Σπονδ. Στήλη", "0,885"],
  ["Σύνολο", "1,012", "91", "-0,9", "97", "-0,2"],
]

export const C_TREND: TrendRow[] = [
  [
    "15/7/2024",
    "52,3",
    "30,8",
    "58",
    "62,1",
    "29,9",
    "60.350",
    "18.600",
    "41.750",
    "1.750",
    "43.500",
  ],
  [
    "3/10/2023",
    "51,5",
    "33,6",
    "71",
    "64,9",
    "32,6",
    "63.100",
    "21.200",
    "41.900",
    "1.790",
    "43.690",
  ],
]
export const C_DISTRIBUTION: DistributionRow[] = [
  ["15/7/2024", "52,3", "33,9", "38,8", "0,87", "30,8"],
  ["3/10/2023", "51,5", "37,2", "40,1", "0,93", "33,6"],
]

/** Scan C's three printouts: bone density (one page, with its scores and
 * the BMD trend), body composition with the fat mass ratios, trend. */
export function scanC(): Record<string, Uint8Array> {
  return {
    "scans/c-result-1.pdf": pdfOf(
      [
        [
          ...header(C),
          ...bmdTable(C_BMD),
          ...bmdTrend([
            ["15/7/2024", "52,3", "1,012"],
            ["3/10/2023", "51,5", "1,020"],
          ]),
        ],
      ],
      TITLES.bone
    ),
    "scans/c-result-2.pdf": pdfOf(
      [
        [
          ...header({ ...C, file: undefined }),
          ...compositionTable(COMPOSITION, ["0,43", "0,38", "0,93"]),
        ],
      ],
      TITLES.composition
    ),
    "scans/c-result-3.pdf": pdfOf(
      [[...header(C), ...trendTables(C_TREND, C_DISTRIBUTION)]],
      TITLES.trend
    ),
  }
}

export const B: Header = {
  date: "20/2/2024",
  time: "7:30:05 μμ",
  analyzed: "22/2/2024 10:02:11 πμ",
  file: "b3x8n4p0aa.dfb",
}

export const B_SUPPLEMENTARY: [string, string, string, string][] = [
  ["Κεφαλή", "1,872", "402", "215"],
  ["Αριστερός βραχίονας", "0,698", "121", "173"],
  ["Αριστερό σκέλος", "1,110", "349", "314"],
  ["Αριστερός κορμός", "0,788", "228", "290"],
  ["Σύνολο αριστερών", "1,005", "872", "868"],
  ["Δεξιός βραχίονας", "0,690", "118", "171"],
  ["Δεξιό σκέλος", "1,098", "344", "313"],
  ["Δεξιός κορμός", "0,781", "225", "288"],
  ["Σύνολο δεξιών μελών", "0,999", "863", "860"],
  ["Βραχίονες", "0,694", "239", "344"],
  ["Κνήμες", "1,104", "693", "627"],
  ["Κορμός", "0,785", "453", "578"],
  ["Πλευρές", "0,598", "131", "219"],
  ["Πύελος", "0,955", "180", "188"],
  ["Σπονδυλική στήλη", "0,880", "142", "161"],
  ["Σύνολο", "1,009", "1.735", "1.728"],
]

/** Scan B's table: scan C's regions, its own android, gynoid and total. */
export const B_COMPOSITION: CompositionRow[] = [
  ...COMPOSITION.slice(0, 11),
  comp("Android", "35,8", "35,3", 1450, 2600, 30),
  comp("Gynoid", "39,9", "38,7", 3560, 5370, 178),
  comp("Σύνολο", "32,4", "31,4", 20150, 41980, 1735, "63,9"),
]

export const B_TREND: TrendRow[] = [
  [
    "20/2/2024",
    "51,9",
    "32,4",
    "-",
    "63,9",
    "31,4",
    "62.130",
    "20.150",
    "41.980",
    "1.735",
    "43.715",
  ],
  [
    "3/10/2023",
    "51,5",
    "33,6",
    "71",
    "64,9",
    "32,6",
    "63.100",
    "21.201",
    "41.900",
    "1.790",
    "43.690",
  ],
]
export const B_DISTRIBUTION: DistributionRow[] = [
  ["20/2/2024", "51,9", "35,8", "39,9", "0,90", "32,4"],
  ["3/10/2023", "51,5", "37,2", "40,1", "0,93", "33,6"],
]

/** Scan B, re-saved: its bone density printout's first page holds the short
 * table and its second the supplementary results; its body composition
 * printout's first page the tissue quantitation, its second the table and
 * the ratios. */
export function scanB(): Record<string, Uint8Array> {
  return {
    "scans/b-result-1-fixed.pdf": pdfOf(
      [
        [
          ...header(B),
          ...bmdTable(
            B_SUPPLEMENTARY.filter(
              ([l]) => !/ /.test(l) || l === "Σπονδυλική στήλη"
            ).map(([l, v]): BmdRow => [
              l === "Σπονδυλική στήλη" ? "Σπονδυλική" : l,
              v,
            ])
          ),
        ],
        [...header(B), ...supplementaryTable(B_SUPPLEMENTARY)],
      ],
      TITLES.bone
    ),
    "scans/b-result-2-fixed.pdf": pdfOf(
      [
        [...header(B), ...quantitationTable()],
        [
          ...header(B),
          ...compositionTable(B_COMPOSITION, ["0,44", "0,39", "0,92"]),
        ],
      ],
      TITLES.composition
    ),
    "scans/b-result-3-fixed.pdf": pdfOf(
      [[...header(B), ...trendTables(B_TREND, B_DISTRIBUTION)]],
      TITLES.trend
    ),
  }
}

/** A trend page typed by hand from a photo of scan D's printout. */
export const TRANSCRIPTION = {
  photo: "d-result-3.jpg",
  note: "typed from the photo; centiles as printed",
  scan: "5/3/2026",
  measured: "11:20:00 πμ",
  analyzed: "5/3/2026 11:31:09 πμ",
  file: "d9t5r2w7kk.dfb",
  composition: [
    {
      date: "5/3/2026",
      age: "54,0",
      tissue_fat: "26,1",
      centile: "35",
      body_mass: "60,2",
      body_fat: "25,3",
      tissue: "58.400",
      fat_mass: "15.240",
      lean_mass: "43.160",
      bone_mass: "1.760",
      fat_free_mass: "44.920",
    },
    {
      date: "12/11/2025",
      age: "53,7",
      tissue_fat: "27,9",
      centile: "44",
      body_mass: "61,0",
      body_fat: "27,0",
      tissue: "59.230",
      fat_mass: "16.530",
      lean_mass: "42.700",
      bone_mass: "1.770",
      fat_free_mass: "44.470",
    },
    {
      date: "15/7/2024",
      age: "52,3",
      tissue_fat: "30,8",
      centile: "58",
      body_mass: "62,1",
      body_fat: "29,9",
      tissue: "60.351",
      fat_mass: "18.601",
      lean_mass: "41.750",
      bone_mass: "1.750",
      fat_free_mass: "43.500",
    },
    {
      date: "3/10/2023",
      age: "51,5",
      tissue_fat: "33,5",
      centile: "-",
      body_mass: "64,9",
      body_fat: "32,5",
      tissue: "63.110",
      fat_mass: "21.150",
      lean_mass: "41.960",
      bone_mass: "1.791",
      fat_free_mass: "43.751",
    },
  ],
  distribution: [
    {
      date: "5/3/2026",
      age: "54,0",
      android_fat: "29,0",
      gynoid_fat: "35,2",
      ag_ratio: "0,82",
      tissue_fat: "26,1",
    },
    {
      date: "12/11/2025",
      age: "53,7",
      android_fat: "30,6",
      gynoid_fat: "36,4",
      ag_ratio: "0,84",
      tissue_fat: "27,9",
    },
    {
      date: "15/7/2024",
      age: "52,3",
      android_fat: "33,9",
      gynoid_fat: "38,8",
      ag_ratio: "0,87",
      tissue_fat: "30,8",
    },
    {
      date: "3/10/2023",
      age: "51,5",
      android_fat: "37,3",
      gynoid_fat: "40,1",
      ag_ratio: "0,93",
      tissue_fat: "33,5",
    },
  ],
}

/** Invented bytes standing in for the photo. */
export const PHOTO = Uint8Array.from([
  0xff, 0xd8, 0xff, 0xe0, 1, 2, 3, 4, 0xff, 0xd9,
])

/** A transcribed page and its photo, by their paths. */
export function transcribedPage(
  page: object = TRANSCRIPTION
): Record<string, string | Uint8Array> {
  return {
    "typed/d-result-3.json": JSON.stringify(page, null, 2),
    "typed/d-result-3.jpg": PHOTO,
  }
}
