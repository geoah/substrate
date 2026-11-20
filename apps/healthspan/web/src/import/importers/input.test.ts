import { strToU8, zipSync } from "fflate"
import { describe, expect, it } from "vitest"
import {
  inputFromDirectoryEntries,
  inputFromFiles,
  inputFromZip,
  isZip,
} from "./input"
import type { Input } from "./types"

/** A picked file, its path inside a picked folder where it has one. */
function file(
  name: string,
  content: string | Uint8Array,
  relative?: string,
  type = ""
) {
  const bytes = new Uint8Array(
    typeof content === "string" ? strToU8(content) : content
  )
  const f = new File([bytes], name, { type })
  if (relative !== undefined)
    Object.defineProperty(f, "webkitRelativePath", { value: relative })
  return f
}

const paths = (input: Input) => input.files.map((f) => f.path)

describe("picked files", () => {
  it("are an input of one file by its name, read when asked", async () => {
    const input = await inputFromFiles([
      file(
        "export-numan-20260105.json",
        '\ufeff{"é":1}',
        undefined,
        "application/json"
      ),
    ])
    expect(input.name).toBe("export-numan-20260105.json")
    expect(input.files).toHaveLength(1)
    const [f] = input.files
    expect([f.path, f.type, f.size]).toEqual([
      "export-numan-20260105.json",
      "application/json",
      11,
    ])
    expect(await f.text()).toBe('{"é":1}')
    expect((await f.bytes()).length).toBe(11)
  })

  it("are named after the first where there are several", async () => {
    const input = await inputFromFiles([
      file("b.json", "{}"),
      file("a.json", "{}"),
    ])
    expect(input.name).toBe("b.json and 1 more")
    expect(paths(input)).toEqual(["a.json", "b.json"])
  })

  it("of a picked folder keep their paths inside it, its name the input's", async () => {
    const input = await inputFromFiles([
      file("manifest.json", "{}", "20260101T120000Z/manifest.json"),
      file("0001.json", "{}", "20260101T120000Z/responses/0001.json"),
      file(".DS_Store", "x", "20260101T120000Z/.DS_Store"),
    ])
    expect(input.name).toBe("20260101T120000Z")
    expect(paths(input)).toEqual(["manifest.json", "responses/0001.json"])
  })

  it("are unpacked where the one file picked is a zip", async () => {
    const zip = zipSync({ "run/manifest.json": strToU8("{}") })
    const input = await inputFromFiles([
      file("run.zip", zip, undefined, "application/zip"),
    ])
    expect(input.name).toBe("run.zip")
    expect(paths(input)).toEqual(["manifest.json"])
    expect(isZip({ name: "x.ZIP" })).toBe(true)
    expect(isZip({ name: "x.json", type: "application/json" })).toBe(false)
  })
})

describe("a zip", () => {
  const zip = zipSync({
    "export/items/visit/1.json": strToU8('{"id":1}'),
    "export/files/report/a.pdf": new Uint8Array([0x25, 0x50]),
    "export/sub/": new Uint8Array(),
    "export/.DS_Store": strToU8("x"),
    "__MACOSX/export/._a.pdf": strToU8("x"),
  })

  it("is its files under the one folder they share, macOS's own left out", async () => {
    const input = await inputFromZip(zip, "randox.zip")
    expect(input.name).toBe("randox.zip")
    expect(input.files.map((f) => [f.path, f.type, f.size])).toEqual([
      ["files/report/a.pdf", "application/pdf", 2],
      ["items/visit/1.json", "application/json", 8],
    ])
    expect(await input.files[1].text()).toBe('{"id":1}')
    expect(await input.files[0].bytes()).toEqual(new Uint8Array([0x25, 0x50]))
  })

  it("keeps its paths where its files share no folder", async () => {
    const flat = zipSync({
      "a.json": strToU8("{}"),
      "dir/b.json": strToU8("{}"),
    })
    const input = await inputFromZip(new File([flat], "flat.zip"))
    expect(input.name).toBe("flat.zip")
    expect(paths(input)).toEqual(["a.json", "dir/b.json"])
  })
})

/** Dropped items as a browser hands them over, the reader answering in
 * batches of one. */
function fileEntry(path: string, content: string): FileSystemFileEntry {
  const name = path.slice(path.lastIndexOf("/") + 1)
  return {
    isFile: true,
    isDirectory: false,
    name,
    fullPath: `/${path}`,
    file: (ok: (f: File) => void) => ok(file(name, content)),
  } as unknown as FileSystemFileEntry
}

function dirEntry(
  path: string,
  children: FileSystemEntry[]
): FileSystemDirectoryEntry {
  return {
    isFile: false,
    isDirectory: true,
    name: path.slice(path.lastIndexOf("/") + 1),
    fullPath: `/${path}`,
    createReader: () => {
      const left = [...children]
      return {
        readEntries: (ok: (e: FileSystemEntry[]) => void) =>
          ok(left.length ? [left.shift()!] : []),
      }
    },
  } as unknown as FileSystemDirectoryEntry
}

describe("a dropped folder", () => {
  it("is read whole, its files' paths inside it", async () => {
    const run = dirEntry("20260101T120000Z", [
      fileEntry("20260101T120000Z/manifest.json", "{}"),
      dirEntry("20260101T120000Z/responses", [
        fileEntry("20260101T120000Z/responses/0002.pdf", "%PDF"),
        fileEntry("20260101T120000Z/responses/0001.json", "{}"),
      ]),
      fileEntry("20260101T120000Z/.DS_Store", "x"),
      dirEntry("20260101T120000Z/__MACOSX", [
        fileEntry("20260101T120000Z/__MACOSX/x", "x"),
      ]),
    ])
    const input = await inputFromDirectoryEntries([run])
    expect(input.name).toBe("20260101T120000Z")
    expect(paths(input)).toEqual([
      "manifest.json",
      "responses/0001.json",
      "responses/0002.pdf",
    ])
    expect(await input.files[2].text()).toBe("%PDF")
  })

  it("beside other items keeps every path from the drop", async () => {
    const input = await inputFromDirectoryEntries([
      fileEntry("a.json", "{}"),
      dirEntry("dexa", [fileEntry("dexa/scan.pdf", "%PDF")]),
    ])
    expect(input.name).toBe("a.json and 1 more")
    expect(paths(input)).toEqual(["a.json", "dexa/scan.pdf"])
  })
})
