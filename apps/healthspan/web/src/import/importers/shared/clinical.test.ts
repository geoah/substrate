import { describe, expect, it } from "vitest"
import {
  absoluteUrl,
  address,
  atDate,
  cleanText,
  emisCoding,
  firstLine,
  NHS_NUMBER_SYSTEM,
  nhsNumberIdentifier,
  ODS_SYSTEM,
  organizationRecord,
  organizationRecordAt,
  phone,
  precision,
  SNOMED,
  tidyName,
} from "./clinical"
import { KINDS } from "./keys"

describe("a GP system's code", () => {
  it("is a SNOMED CT description, a concept, or the system's own", () => {
    expect(emisCoding(123456010, "Cough")).toEqual({
      system: SNOMED,
      descriptionId: "123456010",
      display: "Cough",
    })
    expect(emisCoding("123456110", "Cough")?.descriptionId).toBe("123456110")
    expect(emisCoding("1022431000000105", "Haemoglobin")).toEqual({
      system: SNOMED,
      code: "1022431000000105",
      display: "Haemoglobin",
    })
    expect(emisCoding("12345", "Local")).toEqual({
      system: "emis",
      code: "12345",
      display: "Local",
    })
    expect(emisCoding(0, "x")).toBeNull()
    expect(emisCoding(null, "x")).toBeNull()
    expect(emisCoding("123456000")).toEqual({
      system: SNOMED,
      code: "123456000",
    })
  })

  it("takes an id read losslessly as a bigint, its digits whole", () => {
    expect(emisCoding(14990931000006116n, "Example")).toEqual({
      system: SNOMED,
      descriptionId: "14990931000006116",
      display: "Example",
    })
    expect(emisCoding(1022431000000105n)).toEqual({
      system: SNOMED,
      code: "1022431000000105",
    })
    expect(emisCoding(0n, "x")).toBeNull()
  })
})

describe("an address", () => {
  it("keeps the lines, town and postcode that are not empty", () => {
    expect(
      address(["1 Example Road", " ", null, " Flat 2 "], "London", "AB1 2CD")
    ).toEqual({
      lines: ["1 Example Road", "Flat 2"],
      city: "London",
      postalCode: "AB1 2CD",
    })
    expect(address([], undefined, "AB1 2CD", "GB")).toEqual({
      postalCode: "AB1 2CD",
      country: "GB",
    })
    expect(address(["", null], " ", null)).toBeNull()
  })
})

describe("a name a health service wrote in capitals", () => {
  it("is made readable, its abbreviations kept", () => {
    expect(tidyName("THE EXAMPLE NHS SURGERY")).toBe("The Example NHS Surgery")
    expect(tidyName("ST. JOHN'S  PCN")).toBe("St. John's PCN")
    expect(tidyName("EXAMPLE (HEALTH) CENTRE 2")).toBe(
      "Example (health) Centre 2"
    )
  })

  it("is kept as written where it has a lower-case letter", () => {
    expect(tidyName("Example Practice")).toBe("Example Practice")
    expect(tidyName("123")).toBe("123")
  })
})

describe("a clinical record's small fields", () => {
  it("reads a date's precision from the GP system", () => {
    expect(precision("Year")).toBe("year")
    expect(precision("YearMonth")).toBe("month")
    expect(precision("YearMonthDay")).toBe("day")
    expect(precision("YearMonthDayTime")).toBe("time")
    expect(precision("Unknown")).toBeNull()
  })

  it("takes a stamp's date as midnight UTC", () => {
    expect(atDate("2026-01-02T09:30:00+01:00")).toBe("2026-01-02T00:00:00Z")
    expect(atDate("")).toBeNull()
  })

  it("cleans the marks a portal wraps a number in", () => {
    expect(cleanText("\u202a020 0000 0000\u202c")).toBe("020 0000 0000")
    expect(phone(" \u200b ")).toBeNull()
    expect(absoluteUrl("www.example.org")).toBe("https://www.example.org")
    expect(absoluteUrl("http://example.org")).toBe("http://example.org")
    expect(absoluteUrl("  ")).toBeNull()
  })

  it("takes a message's first line as its topic", () => {
    expect(firstLine("<p>Hello</p>\n\nworld")).toBe("Hello")
    expect(firstLine("\n \n")).toBeNull()
    const long = firstLine(`${"word ".repeat(30)}`)!
    expect(long.length).toBeLessThanOrEqual(80)
    expect(long.endsWith("word…")).toBe(true)
  })
})

describe("identifiers and organizations", () => {
  it("writes an NHS number without its spaces", () => {
    expect(nhsNumberIdentifier("123 456 7890")).toEqual({
      system: NHS_NUMBER_SYSTEM,
      value: "1234567890",
      label: "NHS number",
    })
  })

  it("merges an organization by its ODS code, writing only what is known", () => {
    expect(
      organizationRecord(
        {
          ods: "A12345",
          name: "Example Practice",
          type: "gppractice",
          address: { lines: ["1 Example Road"] },
          identifiers: [{ value: "X1", label: "Epic Care Everywhere id" }],
        },
        1767225600
      )
    ).toEqual({
      kind: KINDS.organization,
      namespace: "ods",
      key: "A12345",
      mode: "merge",
      revision: 1767225600,
      properties: {
        name: "Example Practice",
        type: "gppractice",
        address: { lines: ["1 Example Road"] },
        identifiers: [
          { system: ODS_SYSTEM, value: "A12345", label: "ODS code" },
          { value: "X1", label: "Epic Care Everywhere id" },
        ],
      },
    })
    const hospital = organizationRecord(
      { ods: "B1", name: "Example Hospital", type: "hospital" },
      1
    )
    expect(Object.keys(hospital.properties)).toEqual([
      "name",
      "identifiers",
      "type",
    ])
  })

  it("merges an organization with no ODS code under its importer's key", () => {
    expect(
      organizationRecordAt(
        { namespace: "mychart/example.org", key: "pharmacy/PH-1" },
        {
          name: "Example Pharmacy",
          type: "pharmacy",
          address: { lines: ["1 High Street"], postalCode: "AB1 2CD" },
          phone: null,
        },
        1767225600
      )
    ).toEqual({
      kind: KINDS.organization,
      namespace: "mychart/example.org",
      key: "pharmacy/PH-1",
      mode: "merge",
      revision: 1767225600,
      properties: {
        name: "Example Pharmacy",
        type: "pharmacy",
        address: { lines: ["1 High Street"], postalCode: "AB1 2CD" },
      },
    })
    // Identifiers only where it has some; nothing else it does not know.
    const bare = organizationRecordAt(
      { namespace: "n", key: "k" },
      { name: "Pharmacy", address: null },
      1
    )
    expect(bare.properties).toEqual({ name: "Pharmacy" })
    expect(
      organizationRecordAt(
        { namespace: "n", key: "k" },
        { name: "Pharmacy", identifiers: [{ value: "P1", label: "Epic id" }] },
        1
      ).properties
    ).toEqual({
      name: "Pharmacy",
      identifiers: [{ value: "P1", label: "Epic id" }],
    })
  })
})
