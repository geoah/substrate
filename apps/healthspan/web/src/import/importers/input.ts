// What the owner hands an importer, as one `Input`: picked files, a picked
// folder (`<input webkitdirectory>`), a dropped folder or a zip, each file
// by its path inside what was handed in. Bytes are read only when an
// importer asks for them; text is UTF-8.

import { unzipSync } from "fflate"
import { mediaTypeOf } from "./shared/documents"
import type { Input, InputFile } from "./types"

/** Files a system adds that no provider wrote: macOS's resource forks and
 * folder settings. */
function junk(path: string): boolean {
  const parts = path.split("/")
  return (
    parts.includes("__MACOSX") ||
    parts[parts.length - 1] === ".DS_Store" ||
    parts[parts.length - 1] === "Thumbs.db"
  )
}

/** A path inside what was handed in: `/`-separated, no leading `/` or
 * `./`. */
function cleanPath(path: string): string {
  return path
    .replace(/\\/g, "/")
    .replace(/^(\.?\/)+/, "")
    .replace(/\/{2,}/g, "/")
}

function decodeUtf8(bytes: Uint8Array): string {
  return new TextDecoder("utf-8").decode(bytes)
}

/** An input file whose bytes `load` reads once, when first asked. */
function lazyFile(
  path: string,
  size: number,
  type: string,
  load: () => Promise<Uint8Array>
): InputFile {
  let bytes: Promise<Uint8Array> | undefined
  const read = () => (bytes ??= load())
  return {
    path,
    size,
    type,
    bytes: read,
    text: () => read().then(decodeUtf8),
  }
}

function fromFile(file: Blob & { name: string }, path: string): InputFile {
  return lazyFile(
    path,
    file.size,
    file.type,
    async () => new Uint8Array(await file.arrayBuffer())
  )
}

function sorted(files: InputFile[]): InputFile[] {
  return files.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0))
}

/** Whether a picked or dropped file is a zip. */
export function isZip(file: { name: string; type?: string }): boolean {
  return (
    /\.zip$/i.test(file.name) ||
    file.type === "application/zip" ||
    file.type === "application/x-zip-compressed"
  )
}

/** The one folder every path starts with, where they all share one. */
function sharedTop(paths: readonly string[]): string | undefined {
  if (!paths.length) return undefined
  const tops = new Set(
    paths.map((p) => (p.includes("/") ? p.slice(0, p.indexOf("/")) : ""))
  )
  const [top] = [...tops]
  return tops.size === 1 && top ? top : undefined
}

/** Picked files as an input. A single zip is unpacked
 * (`inputFromZip`). Files of a picked folder (`<input webkitdirectory>`,
 * each with its `webkitRelativePath`) keep their paths inside the folder,
 * and the folder's name is the input's; loose files keep their names, and
 * the input is named after the first. */
export async function inputFromFiles(
  files: readonly File[] | FileList
): Promise<Input> {
  const list = Array.from(files)
  if (list.length === 1 && isZip(list[0]) && !list[0].webkitRelativePath)
    return inputFromZip(list[0])
  const relative = list.map((f) => cleanPath(f.webkitRelativePath || ""))
  const top =
    list.length && relative.every(Boolean) ? sharedTop(relative) : undefined
  const out: InputFile[] = []
  list.forEach((f, i) => {
    const path = top ? relative[i].slice(top.length + 1) : cleanPath(f.name)
    if (path && !junk(path)) out.push(fromFile(f, path))
  })
  const name =
    top ??
    (list.length <= 1
      ? (list[0]?.name ?? "")
      : `${list[0].name} and ${list.length - 1} more`)
  return { name, files: sorted(out) }
}

function fileOf(entry: FileSystemFileEntry): Promise<File> {
  return new Promise((resolve, reject) => entry.file(resolve, reject))
}

/** Every entry of a directory: a reader answers in batches until it
 * answers none. */
async function children(
  dir: FileSystemDirectoryEntry
): Promise<FileSystemEntry[]> {
  const reader = dir.createReader()
  const out: FileSystemEntry[] = []
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) =>
      reader.readEntries(resolve, reject)
    )
    if (!batch.length) return out
    out.push(...batch)
  }
}

async function walk(
  entry: FileSystemEntry,
  prefix: string,
  out: InputFile[]
): Promise<void> {
  const path = prefix ? `${prefix}/${entry.name}` : entry.name
  if (entry.isFile) {
    if (junk(path)) return
    const file = await fileOf(entry as FileSystemFileEntry)
    out.push(fromFile(file, path))
  } else if (entry.isDirectory) {
    if (junk(path)) return
    for (const child of await children(entry as FileSystemDirectoryEntry))
      await walk(child, path, out)
  }
}

/** Dropped items (`DataTransferItem.webkitGetAsEntry()`) as an input. A
 * single folder is read whole, its files' paths inside it and its name the
 * input's; a single zip is unpacked; several items keep their paths from
 * the drop, under the first item's name. */
export async function inputFromDirectoryEntries(
  entries: readonly FileSystemEntry[]
): Promise<Input> {
  if (entries.length === 1 && entries[0].isFile && isZip(entries[0]))
    return inputFromZip(await fileOf(entries[0] as FileSystemFileEntry))
  const out: InputFile[] = []
  if (entries.length === 1 && entries[0].isDirectory) {
    const dir = entries[0] as FileSystemDirectoryEntry
    for (const child of await children(dir)) await walk(child, "", out)
    return { name: dir.name, files: sorted(out) }
  }
  for (const entry of entries) await walk(entry, "", out)
  const name =
    entries.length <= 1
      ? (entries[0]?.name ?? "")
      : `${entries[0].name} and ${entries.length - 1} more`
  return { name, files: sorted(out) }
}

/** A zip as an input: its files by their paths inside it, one top-level
 * folder every file shares taken off, macOS's `__MACOSX` and `.DS_Store`
 * left out. Each file is unpacked only when an importer reads it. The
 * input is named `name`, else the zip file's name. A file's type is its
 * extension's, "" for one not known. */
export async function inputFromZip(
  zip: Blob | Uint8Array,
  name?: string
): Promise<Input> {
  const data =
    zip instanceof Uint8Array ? zip : new Uint8Array(await zip.arrayBuffer())
  const listed: { name: string; size: number }[] = []
  unzipSync(data, {
    filter: (f) => {
      if (!f.name.endsWith("/"))
        listed.push({ name: f.name, size: f.originalSize })
      return false
    },
  })
  const kept = listed
    .map((f) => ({ ...f, path: cleanPath(f.name) }))
    .filter((f) => f.path && !junk(f.path))
  const top = sharedTop(kept.map((f) => f.path))
  const files = kept.map((f) =>
    lazyFile(
      top ? f.path.slice(top.length + 1) : f.path,
      f.size,
      mediaTypeOf(f.path, ""),
      () =>
        Promise.resolve().then(() => {
          const out = unzipSync(data, { filter: (x) => x.name === f.name })
          const bytes = out[f.name]
          if (!bytes) throw new Error(`${f.path} is not in the zip`)
          return bytes
        })
    )
  )
  const fileName =
    "name" in zip && typeof zip.name === "string" ? zip.name : undefined
  return { name: name ?? fileName ?? top ?? "", files: sorted(files) }
}
